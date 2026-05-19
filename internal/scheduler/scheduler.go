package scheduler

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/amd-gpu-scheduler/pkg/types"
)

// Scheduler manages GPU task scheduling with hybrid NVIDIA + AMD support
type Scheduler struct {
	mu           sync.RWMutex
	pendingTasks []*types.Task
	runningTasks map[string]*types.Task // taskID -> Task
	gpuCount     int
	gpus         map[string]*types.GPUSnapshot // gpuID -> GPU
	defaultPolicy types.SchedulingPolicy
}

// NewScheduler creates a new hybrid scheduler
func NewScheduler() *Scheduler {
	s := &Scheduler{
		pendingTasks: make([]*types.Task, 0),
		runningTasks: make(map[string]*types.Task),
		gpuCount:     2,
		gpus:         make(map[string]*types.GPUSnapshot),
		defaultPolicy: types.PolicyBinpack,
	}
	
	// Initialize with two GPUs (will be updated by GPU manager)
	s.initDefaultGPUs()
	
	return s
}

// initDefaultGPUs initializes the default GPU configurations
func (s *Scheduler) initDefaultGPUs() {
	// RTX 5070 Ti (NVIDIA)
	s.gpus["0"] = &types.GPUSnapshot{
		Info: types.GPUInfo{
			ID:           "0",
			Type:         types.GPUTypeNVIDIA,
			Name:         "NVIDIA GeForce RTX 5070 Ti",
			VRAMMB:       16384, // 16GB
			CoreCount:    14080,
			ComputeUnits: 88,
			Architecture: "Blackwell",
		},
		Usage: types.GPUUsage{
			ID:           "0",
			UsedVRAMMB:   0,
			Utilization:  0,
			MemoryUtil:   0,
			ComputeUtil:  0,
			RunningTasks: 0,
			LastUpdated:  time.Now(),
		},
	}
	
	// RX 7900 XTX (AMD)
	s.gpus["1"] = &types.GPUSnapshot{
		Info: types.GPUInfo{
			ID:           "1",
			Type:         types.GPUTypeAMD,
			Name:         "AMD Radeon RX 7900 XTX",
			VRAMMB:       24576, // 24GB
			CoreCount:    12288,
			ComputeUnits: 96,
			Architecture: "RDNA 3",
		},
		Usage: types.GPUUsage{
			ID:           "1",
			UsedVRAMMB:   0,
			Utilization:  0,
			MemoryUtil:   0,
			ComputeUtil:  0,
			RunningTasks: 0,
			LastUpdated:  time.Now(),
		},
	}
}

// RegisterGPU adds or updates a GPU in the scheduler
func (s *Scheduler) RegisterGPU(gpu *types.GPUSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gpus[gpu.Info.ID] = gpu
}

// GetGPU returns a GPU by ID
func (s *Scheduler) GetGPU(id string) (*types.GPUSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	gpu, ok := s.gpus[id]
	return gpu, ok
}

// ListGPUs returns all registered GPUs
func (s *Scheduler) ListGPUs() []*types.GPUSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	gpus := make([]*types.GPUSnapshot, 0, len(s.gpus))
	for _, gpu := range s.gpus {
		gpus = append(gpus, gpu)
	}
	return gpus
}

// SubmitTask submits a new task for scheduling
func (s *Scheduler) SubmitTask(name, taskType string, priority, gpuReq int, vramMB int) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create task
	task := &types.Task{
		ID:       fmt.Sprintf("task-%d", len(s.pendingTasks)+len(s.runningTasks)+1),
		Name:     name,
		Type:     taskType,
		Priority: priority,
		GPUReq:   gpuReq,
		Status:   "pending",
	}

	// Build scheduling criteria
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: vramMB,
		PreferredGPU:   "", // No preference by default
		TaskType:      taskType,
		Priority:      priority,
		Policy:        s.defaultPolicy,
	}

	// Try to schedule immediately
	result := s.scheduleLocked(task, criteria)
	
	if result.Success {
		task.Status = "running"
		// Parse GPU ID from result
		if gpuID, err := strconv.Atoi(result.AssignedGPU.Info.ID); err == nil {
			task.GPUReq = gpuID
		}
		s.runningTasks[task.ID] = task
		
		// Update GPU usage statistics
		if assignedGPU, ok := s.gpus[result.AssignedGPU.Info.ID]; ok {
			assignedGPU.Usage.UsedVRAMMB += vramMB
			assignedGPU.Usage.RunningTasks++
			assignedGPU.Usage.LastUpdated = time.Now()
		}
	} else {
		s.pendingTasks = append(s.pendingTasks, task)
	}

	return task.ID, nil
}

// scheduleLocked schedules a task using the hybrid scheduling algorithm
func (s *Scheduler) scheduleLocked(task *types.Task, criteria *types.SchedulingCriteria) *types.SchedulingResult {
	// Get all eligible GPUs
	eligibleGPUs := make([]*types.GPUSnapshot, 0)
	for _, gpu := range s.gpus {
		// Check VRAM
		if gpu.FreeVRAM() < criteria.VRAMRequiredMB {
			continue
		}
		// Check health
		if !gpu.IsHealthy() {
			continue
		}
		eligibleGPUs = append(eligibleGPUs, gpu)
	}

	if len(eligibleGPUs) == 0 {
		return &types.SchedulingResult{
			Task:    task,
			Success: false,
			Message: fmt.Sprintf("No GPU has enough VRAM (need %dMB)", criteria.VRAMRequiredMB),
		}
	}

	// Calculate scores for all eligible GPUs
	scoredGPUs := make([]*types.ScoredGPU, 0)
	for _, gpu := range eligibleGPUs {
		score := calculateScore(gpu, criteria, s.gpuCount)
		scoredGPUs = append(scoredGPUs, &types.ScoredGPU{
			GPU:   gpu,
			Score: score,
		})
	}

	// Sort by score (descending)
	for i := 0; i < len(scoredGPUs)-1; i++ {
		for j := i + 1; j < len(scoredGPUs); j++ {
			if scoredGPUs[j].Score > scoredGPUs[i].Score {
				scoredGPUs[i], scoredGPUs[j] = scoredGPUs[j], scoredGPUs[i]
			}
		}
	}

	// Select the best GPU
	best := scoredGPUs[0]
	
	return &types.SchedulingResult{
		Task:         task,
		AssignedGPU:  best.GPU,
		Score:        best.Score,
		Success:      true,
		Message:      fmt.Sprintf("Scheduled to GPU %s (%s, score=%.2f)", best.GPU.Info.ID, best.GPU.Info.Name, best.Score),
	}
}

// calculateScore computes the scheduling score for a GPU
func calculateScore(gpu *types.GPUSnapshot, criteria *types.SchedulingCriteria, gpuCount int) float64 {
	score := 0.0

	// 1. VRAM 分数 (30% weight)
	// 可用 VRAM 越多，分数越高
	vramScore := float64(gpu.FreeVRAM()) / float64(gpu.Info.VRAMMB) * 30
	score += vramScore

	// 2. 利用率分数 (25% weight)
	// GPU 利用率越低，分数越高
	utilScore := (100.0 - gpu.Usage.Utilization) / 100.0 * 25
	score += utilScore

	// 3. GPU 类型分数 (20% weight)
	gpuTypeScore := 15.0
	switch criteria.TaskType {
	case "training":
		// 训练任务优先 NVIDIA
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			gpuTypeScore = 18.0
		} else {
			gpuTypeScore = 15.0
		}
	case "inference":
		// 推理任务两者皆可，AMD 性价比高
		if gpu.Info.Type == types.GPUTypeAMD {
			gpuTypeScore = 18.0
		} else {
			gpuTypeScore = 15.0
		}
	case "compute":
		// 计算任务优先 NVIDIA
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			gpuTypeScore = 20.0
		} else {
			gpuTypeScore = 12.0
		}
	default:
		gpuTypeScore = 15.0
	}
	score += gpuTypeScore

	// 4. 优先级分数 (15% weight)
	priorityScore := float64(criteria.Priority) / 10.0 * 15
	score += priorityScore

	// 5. 负载均衡分数 (10% weight)
	loadBalanceScore := 5.0
	switch criteria.Policy {
	case types.PolicyBinpack:
		loadBalanceScore = float64(gpu.Usage.RunningTasks) / float64(gpuCount) * 10
	case types.PolicySpread:
		loadBalanceScore = (float64(gpuCount) - float64(gpu.Usage.RunningTasks)) / float64(gpuCount) * 10
	case types.PolicyGPUType:
		loadBalanceScore = 10.0
	}
	score += loadBalanceScore

	return score
}

// GetScoreBreakdown returns a formatted string showing the score breakdown for a GPU
func (s *Scheduler) GetScoreBreakdown(gpuID string, taskType string, vramMB int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	gpu, ok := s.gpus[gpuID]
	if !ok {
		return fmt.Sprintf("GPU %s not found", gpuID)
	}

	// Use default criteria for score calculation
	_ = &types.SchedulingCriteria{
		VRAMRequiredMB: vramMB,
		TaskType:      taskType,
		Priority:      5,
		Policy:        s.defaultPolicy,
	}

	vramScore := float64(gpu.FreeVRAM()) / float64(gpu.Info.VRAMMB) * 30
	utilScore := (100.0 - gpu.Usage.Utilization) / 100.0 * 25

	gpuTypeScore := 15.0
	switch taskType {
	case "training":
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			gpuTypeScore = 18.0
		} else {
			gpuTypeScore = 15.0
		}
	case "inference":
		if gpu.Info.Type == types.GPUTypeAMD {
			gpuTypeScore = 18.0
		} else {
			gpuTypeScore = 15.0
		}
	case "compute":
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			gpuTypeScore = 20.0
		} else {
			gpuTypeScore = 12.0
		}
	}

	priorityScore := float64(5) / 10.0 * 15
	loadBalanceScore := float64(gpu.Usage.RunningTasks) / float64(s.gpuCount) * 10
	totalScore := vramScore + utilScore + gpuTypeScore + priorityScore + loadBalanceScore

	return fmt.Sprintf(
		"GPU %s (%s)\n"+
			"  VRAM: %d/%d MB (%.1f/30)\n"+
			"  Util: %.1f%% (%.1f/25)\n"+
			"  Type: %s (%.1f/20)\n"+
			"  Priority: %.1f/15\n"+
			"  Load: %d tasks (%.1f/10)\n"+
			"  ─────────────\n"+
			"  Total: %.2f",
		gpu.Info.ID, gpu.Info.Name,
		gpu.FreeVRAM(), gpu.Info.VRAMMB, vramScore,
		gpu.Usage.Utilization, utilScore,
		gpu.Info.Type, gpuTypeScore,
		priorityScore,
		gpu.Usage.RunningTasks, loadBalanceScore,
		totalScore,
	)
}

// ListTasks returns all tasks (pending and running)
func (s *Scheduler) ListTasks() []*types.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]*types.Task, 0, len(s.pendingTasks)+len(s.runningTasks))
	tasks = append(tasks, s.pendingTasks...)
	for _, task := range s.runningTasks {
		tasks = append(tasks, task)
	}
	return tasks
}

// CancelTask cancels a pending task
func (s *Scheduler) CancelTask(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, task := range s.pendingTasks {
		if task.ID == taskID {
			s.pendingTasks = append(s.pendingTasks[:i], s.pendingTasks[i+1:]...)
			return true
		}
	}
	return false
}

// CompleteTask marks a running task as complete
func (s *Scheduler) CompleteTask(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.runningTasks[taskID]; ok {
		delete(s.runningTasks, taskID)
		// Try to schedule pending tasks
		s.trySchedulePendingLocked()
		return true
	}
	return false
}

// trySchedulePendingLocked tries to schedule pending tasks
func (s *Scheduler) trySchedulePendingLocked() {
	stillPending := make([]*types.Task, 0)
	
	for _, task := range s.pendingTasks {
		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 4096, // Default 4GB
			TaskType:       task.Type,
			Priority:       task.Priority,
			Policy:         s.defaultPolicy,
		}
		
		result := s.scheduleLocked(task, criteria)
		if result.Success {
			task.Status = "running"
			s.runningTasks[task.ID] = task
		} else {
			stillPending = append(stillPending, task)
		}
	}
	
	s.pendingTasks = stillPending
}

// SetDefaultPolicy sets the default scheduling policy
func (s *Scheduler) SetDefaultPolicy(policy types.SchedulingPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultPolicy = policy
}

// GetDefaultPolicy returns the current default scheduling policy
func (s *Scheduler) GetDefaultPolicy() types.SchedulingPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.defaultPolicy
}
