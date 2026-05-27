package master

import (
	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// LocalScheduler handles scheduling decisions within the master
// using the same multi-factor scoring algorithm as the standalone scheduler
type LocalScheduler struct {
	gpus map[string]map[string]*types.GPUSnapshot // nodeID -> gpuID -> snapshot
}

func NewLocalScheduler() *LocalScheduler {
	return &LocalScheduler{gpus: make(map[string]map[string]*types.GPUSnapshot)}
}

// UpdateGPUs updates the GPU pool for a node
func (ls *LocalScheduler) UpdateGPUs(nodeID string, gpuList []*types.GPUSnapshot) {
	ls.gpus[nodeID] = make(map[string]*types.GPUSnapshot)
	for _, g := range gpuList {
		ls.gpus[nodeID][g.Info.ID] = g
	}
}

// TrySchedule attempts to schedule a task on a node's GPUs
// Returns (assigned, gpuIDs)
func (ls *LocalScheduler) TrySchedule(task *types.Task, nodeGPUs []*types.GPUSnapshot) (bool, []string) {
	if len(nodeGPUs) == 0 {
		return false, nil
	}

	// Build a combined GPU list with node context
	type scoredGPU struct {
		gpu    *types.GPUSnapshot
		nodeID string
	}

	var candidates []scoredGPU
	for _, gpu := range nodeGPUs {
		candidates = append(candidates, scoredGPU{gpu: gpu})
	}

	// Score each candidate
	for i := range candidates {
		candidates[i].gpu.Score = ls.calculateScore(&types.SchedulingCriteria{
			VRAMRequiredMB: task.VRAMMB,
			TaskType:      task.Type,
			Priority:      task.Priority,
			Policy:        types.PolicyBinpack,
		}, candidates[i].gpu)
	}

	// Sort by score descending
	for i := 0; i < len(candidates)-1; i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].gpu.Score > candidates[i].gpu.Score {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	// Try to allocate on best candidate
	for _, cand := range candidates {
		gpu := cand.gpu
		if gpu.FreeVRAM() >= task.VRAMMB && gpu.IsHealthy() {
			// Allocate
			gpu.Usage.UsedVRAMMB += task.VRAMMB
			gpu.Usage.RunningTasks++
			return true, []string{gpu.Info.ID}
		}
	}

	return false, nil
}

// calculateScore computes the scheduling score for a GPU
// Weights: VRAM=30%, Utilization=25%, GPUType=20%, Priority=15%, LoadBalance=10%
func (ls *LocalScheduler) calculateScore(criteria *types.SchedulingCriteria, gpu *types.GPUSnapshot) float64 {
	var score float64

	// 1. Free VRAM score (30%) — more free = higher score
	vramFreePct := gpu.FreeVRAMPercent()
	score += vramFreePct * 0.30

	// 2. Utilization score (25%) — lower utilization = higher score
	// A GPU at 0% gets full 100 points; at 100% gets 0
	utilScore := 100.0 - gpu.Usage.Utilization
	score += utilScore * 0.25

	// 3. GPU type match (20%) — depends on task type
	typeScore := 50.0
	switch criteria.TaskType {
	case "training":
		if gpu.Info.Type == types.GPUTypeNVIDIA {
			typeScore = 100.0
		}
	case "inference":
		if gpu.Info.Type == types.GPUTypeAMD {
			typeScore = 100.0
		}
	}
	score += typeScore * 0.20

	// 4. Priority boost (15%) — scale 1-10 to 0-100
	priorityScore := float64(criteria.Priority) * 10.0
	score += priorityScore * 0.15

	// 5. Load balance — binpack: fewer tasks = lower score (prefer busier)
	loadScore := float64(gpu.Usage.RunningTasks) * 10.0
	score += loadScore * 0.10

	return score
}
