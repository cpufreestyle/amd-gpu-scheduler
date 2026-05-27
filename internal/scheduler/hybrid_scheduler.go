package scheduler

import (
	"fmt"
	"sort"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// HybridScheduler implements a hybrid GPU scheduling algorithm
// that supports both NVIDIA and AMD GPUs with multiple scheduling policies
type HybridScheduler struct {
	gpuCount int
	gpus     []*types.GPUSnapshot
}

// NewHybridScheduler creates a new hybrid scheduler
func NewHybridScheduler() *HybridScheduler {
	return &HybridScheduler{
		gpuCount: 2,
		gpus:     make([]*types.GPUSnapshot, 0),
	}
}

// SetGPUCount sets the number of available GPUs
func (s *HybridScheduler) SetGPUCount(count int) {
	s.gpuCount = count
}

// RegisterGPU registers a GPU with the scheduler
func (s *HybridScheduler) RegisterGPU(gpu *types.GPUSnapshot) {
	s.gpus = append(s.gpus, gpu)
}

// GetGPUs returns all registered GPUs
func (s *HybridScheduler) GetGPUs() []*types.GPUSnapshot {
	return s.gpus
}

// CalculateScore computes the scheduling score for a GPU given the criteria
func (s *HybridScheduler) CalculateScore(gpu *types.GPUSnapshot, criteria *types.SchedulingCriteria) float64 {
	score := 0.0
	reasons := []string{}

	// 1. VRAM 分数 (30% weight)
	// 可用 VRAM 越多，分数越高
	vramScore := float64(gpu.FreeVRAM()) / float64(gpu.Info.VRAMMB) * 30
	score += vramScore
	reasons = append(reasons, fmt.Sprintf("VRAM=%.1f", vramScore))

	// 2. 利用率分数 (25% weight)
	// GPU 利用率越低，分数越高（优先选择空闲 GPU）
	utilScore := (100.0 - gpu.Usage.Utilization) / 100.0 * 25
	score += utilScore
	reasons = append(reasons, fmt.Sprintf("Util=%.1f", utilScore))

	// 3. GPU 类型匹配分数 (20% weight)
	// 如果有偏好类型，检查匹配程度
	gpuTypeScore := 20.0
	if criteria.PreferredGPU != "" && criteria.PreferredGPU != types.GPUTypeUnknown {
		if gpu.Info.Type == criteria.PreferredGPU {
			gpuTypeScore = 20.0
			reasons = append(reasons, fmt.Sprintf("TypeMatch=%.1f(NVIDIA)", gpuTypeScore))
		} else {
			// 如果不匹配，降低分数
			gpuTypeScore = 10.0
			reasons = append(reasons, fmt.Sprintf("TypeMatch=%.1f(AMD)", gpuTypeScore))
		}
	} else {
		// 根据任务类型调整分数
		switch criteria.TaskType {
		case "training":
			// 训练任务优先 NVIDIA (CUDA 生态更好)
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
		reasons = append(reasons, fmt.Sprintf("TypeScore=%.1f", gpuTypeScore))
	}
	score += gpuTypeScore

	// 4. 优先级分数 (15% weight)
	// 高优先级任务应该优先获得资源
	priorityScore := float64(criteria.Priority) / 10.0 * 15
	score += priorityScore
	reasons = append(reasons, fmt.Sprintf("Priority=%.1f", priorityScore))

	// 5. 负载均衡分数 (10% weight)
	// 根据调度策略调整
	loadBalanceScore := 10.0
	switch criteria.Policy {
	case types.PolicyBinpack:
		// Binpack: 尽量用少的 GPU，优先选择已经有很多任务的 GPU
		loadBalanceScore = float64(gpu.Usage.RunningTasks) / float64(s.gpuCount) * 10
	case types.PolicySpread:
		// Spread: 尽量分散，优先选择任务少的 GPU
		loadBalanceScore = (float64(s.gpuCount) - float64(gpu.Usage.RunningTasks)) / float64(s.gpuCount) * 10
	case types.PolicyGPUType:
		// GPU Type: 类型匹配优先
		loadBalanceScore = 10.0
	default:
		loadBalanceScore = 5.0
	}
	score += loadBalanceScore
	reasons = append(reasons, fmt.Sprintf("LoadBal=%.1f", loadBalanceScore))

	// Store reasons (not used in score calculation, but useful for debugging)
	_ = reasons

	return score
}

// Schedule selects the best GPU for a task based on the criteria
func (s *HybridScheduler) Schedule(task *types.Task, criteria *types.SchedulingCriteria) *types.SchedulingResult {
	// Filter eligible GPUs
	eligibleGPUs := make([]*types.GPUSnapshot, 0)
	for _, gpu := range s.gpus {
		// Check if GPU can allocate the required VRAM
		if !gpu.CanAllocate(criteria.VRAMRequiredMB) {
			continue
		}
		// Check if GPU is healthy
		if !gpu.IsHealthy() {
			continue
		}
		eligibleGPUs = append(eligibleGPUs, gpu)
	}

	// If no eligible GPUs, return failure
	if len(eligibleGPUs) == 0 {
		return &types.SchedulingResult{
			Task:    task,
			Success: false,
			Message: "No eligible GPU available",
		}
	}

	// Calculate scores for all eligible GPUs
	scoredGPUs := make([]*types.ScoredGPU, 0, len(eligibleGPUs))
	for _, gpu := range eligibleGPUs {
		score := s.CalculateScore(gpu, criteria)
		scoredGPUs = append(scoredGPUs, &types.ScoredGPU{
			GPU:   gpu,
			Score: score,
		})
	}

	// Sort by score (descending)
	sort.Slice(scoredGPUs, func(i, j int) bool {
		return scoredGPUs[i].Score > scoredGPUs[j].Score
	})

	// Select the best GPU
	best := scoredGPUs[0]

	return &types.SchedulingResult{
		Task:         task,
		AssignedGPU:  best.GPU,
		Score:        best.Score,
		Success:      true,
		Message:      fmt.Sprintf("Scheduled to GPU %s (score=%.2f)", best.GPU.Info.ID, best.Score),
	}
}

// PrintScoreBreakdown prints the score breakdown for debugging
func (s *HybridScheduler) PrintScoreBreakdown(gpu *types.GPUSnapshot, criteria *types.SchedulingCriteria) string {
	score := 0.0
	
	// VRAM 分数
	vramScore := float64(gpu.FreeVRAM()) / float64(gpu.Info.VRAMMB) * 30
	score += vramScore
	
	// 利用率分数
	utilScore := (100.0 - gpu.Usage.Utilization) / 100.0 * 25
	score += utilScore
	
	// GPU 类型分数
	gpuTypeScore := 15.0
	switch criteria.TaskType {
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
	score += gpuTypeScore
	
	// 优先级分数
	priorityScore := float64(criteria.Priority) / 10.0 * 15
	score += priorityScore
	
	// 负载均衡分数
	loadBalanceScore := 5.0
	switch criteria.Policy {
	case types.PolicyBinpack:
		loadBalanceScore = float64(gpu.Usage.RunningTasks) / float64(s.gpuCount) * 10
	case types.PolicySpread:
		loadBalanceScore = (float64(s.gpuCount) - float64(gpu.Usage.RunningTasks)) / float64(s.gpuCount) * 10
	case types.PolicyGPUType:
		loadBalanceScore = 10.0
	}
	score += loadBalanceScore

	return fmt.Sprintf(
		"GPU %s | VRAM=%2.f/30 | Util=%2.f/25 | Type=%2.f/20 | Priority=%2.f/15 | Load=%2.f/10 | Total=%.2f",
		gpu.Info.ID,
		vramScore, utilScore, gpuTypeScore, priorityScore, loadBalanceScore,
		score,
	)
}

