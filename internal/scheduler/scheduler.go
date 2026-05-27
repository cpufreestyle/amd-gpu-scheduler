package scheduler

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hybrid-gpu-scheduler/internal/executor"
	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// Scheduler manages GPU task scheduling with hybrid NVIDIA + AMD support
type Scheduler struct {
	mu           sync.RWMutex
	pendingTasks []*types.Task
	runningTasks map[string]*types.Task // taskID -> Task
	completedTasks []*types.Task         // finished tasks (completed/failed/timeout/killed)
	gpuCount     int
	gpus         map[string]*types.GPUSnapshot // gpuID -> GPU
	defaultPolicy types.SchedulingPolicy
	taskCounter  atomic.Int64
	
	// Preemption support
	preemptConfig *PreemptConfig
	preemptHistory []*PreemptRecord
}

// NewScheduler creates a new hybrid scheduler
func NewScheduler() *Scheduler {
	s := &Scheduler{
		pendingTasks:  make([]*types.Task, 0),
		runningTasks:  make(map[string]*types.Task),
		completedTasks: make([]*types.Task, 0),
		gpuCount:      2,
		gpus:          make(map[string]*types.GPUSnapshot),
		defaultPolicy:  types.PolicyBinpack,
		preemptConfig:  DefaultPreemptConfig(),
		preemptHistory: make([]*PreemptRecord, 0),
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
func (s *Scheduler) SubmitTask(task *types.Task) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Generate task ID if not set
	if task.ID == "" {
		task.ID = fmt.Sprintf("task-%d", s.taskCounter.Add(1))
	}
	task.Status = "pending"

	// Build scheduling criteria
	vramMB := 4096 // default
	if task.VRAMMB > 0 {
		vramMB = task.VRAMMB
	}
	
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: vramMB,
		PreferredGPU:   "", // No preference by default
		TaskType:      task.Type,
		Priority:      task.Priority,
		Policy:        s.defaultPolicy,
	}

	// Try to schedule immediately
	result := s.scheduleLocked(task, criteria)
	
	// If scheduling failed, try preemption
	if !result.Success && s.preemptConfig.Enabled {
		log.Printf("⚡ Task %s (priority %d) failed to schedule, trying preemption...", task.ID, task.Priority)
		if s.tryPreempt(task, criteria) {
			// Preemption succeeded, retry scheduling
			result = s.scheduleLocked(task, criteria)
		}
	}
	
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
		
		// Actually execute the task on GPU
		exec := executor.GetExecutor()
		if err := exec.SubmitTask(task, result.AssignedGPU.Info.ID); err != nil {
			log.Printf("⚠️  Failed to execute task %s: %v", task.ID, err)
			// Mark task as failed but keep it in running tasks
			task.Status = "failed"
			task.Error = err.Error()
		} else {
			log.Printf("✅ Task %s submitted for execution on GPU %s", task.ID, result.AssignedGPU.Info.ID)
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

	// 1. VRAM score (30% weight)
	// More free VRAM = higher score
	vramScore := float64(gpu.FreeVRAM()) / float64(gpu.Info.VRAMMB) * 30
	score += vramScore

	// 2. Utilization score (25% weight)
	// Lower GPU utilization = higher score
	utilScore := (100.0 - gpu.Usage.Utilization) / 100.0 * 25
	score += utilScore

	// 3. GPU type score (20% weight)
	gpuTypeScore := 15.0
	switch criteria.TaskType {
	case "training":
		// Training tasks prefer NVIDIA
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			gpuTypeScore = 18.0
		} else {
			gpuTypeScore = 15.0
		}
	case "inference":
		// Inference tasks can use both, AMD has better price-performance
		if gpu.Info.Type == types.GPUTypeAMD {
			gpuTypeScore = 18.0
		} else {
			gpuTypeScore = 15.0
		}
	case "compute":
		// Compute tasks prefer NVIDIA
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			gpuTypeScore = 20.0
		} else {
			gpuTypeScore = 12.0
		}
	default:
		gpuTypeScore = 15.0
	}
	score += gpuTypeScore

	// 4. Priority score (15% weight)
	priorityScore := float64(criteria.Priority) / 10.0 * 15
	score += priorityScore

	// 5. Load balance score (10% weight)
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

	tasks := make([]*types.Task, 0, len(s.pendingTasks)+len(s.runningTasks)+len(s.completedTasks))
	tasks = append(tasks, s.pendingTasks...)
	for _, task := range s.runningTasks {
		tasks = append(tasks, task)
	}
	tasks = append(tasks, s.completedTasks...)
	return tasks
}

// ReleaseTask removes a task from running and releases GPU resources (called by executor on completion/timeout/kill)
func (s *Scheduler) ReleaseTask(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.runningTasks[taskID]
	if !ok {
		return
	}

	vramMB := task.VRAMMB
	if vramMB == 0 {
		vramMB = 4096
	}
	gpuID := fmt.Sprintf("%d", task.GPUReq)
	if gpu, exists := s.gpus[gpuID]; exists {
		gpu.Usage.UsedVRAMMB -= vramMB
		if gpu.Usage.UsedVRAMMB < 0 {
			gpu.Usage.UsedVRAMMB = 0
		}
		gpu.Usage.RunningTasks--
		if gpu.Usage.RunningTasks < 0 {
			gpu.Usage.RunningTasks = 0
		}
		gpu.Usage.LastUpdated = time.Now()
	}

	delete(s.runningTasks, taskID)
	s.completedTasks = append(s.completedTasks, task)
	// Keep at most 100 completed tasks
	if len(s.completedTasks) > 100 {
		s.completedTasks = s.completedTasks[len(s.completedTasks)-100:]
	}
	s.trySchedulePendingLocked()
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

// CompleteTask marks a running task as complete and releases GPU resources
func (s *Scheduler) CompleteTask(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	task, ok := s.runningTasks[taskID]
	if !ok {
		return false
	}

	// Release GPU resources
	vramMB := task.VRAMMB
	if vramMB == 0 {
		vramMB = 4096 // default allocation
	}
	gpuID := fmt.Sprintf("%d", task.GPUReq)
	if gpu, exists := s.gpus[gpuID]; exists {
		gpu.Usage.UsedVRAMMB -= vramMB
		if gpu.Usage.UsedVRAMMB < 0 {
			gpu.Usage.UsedVRAMMB = 0
		}
		gpu.Usage.RunningTasks--
		if gpu.Usage.RunningTasks < 0 {
			gpu.Usage.RunningTasks = 0
		}
		gpu.Usage.LastUpdated = time.Now()
	}

	delete(s.runningTasks, taskID)
	s.completedTasks = append(s.completedTasks, task)
	if len(s.completedTasks) > 100 {
		s.completedTasks = s.completedTasks[len(s.completedTasks)-100:]
	}
	// Try to schedule pending tasks
	s.trySchedulePendingLocked()
	return true
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

