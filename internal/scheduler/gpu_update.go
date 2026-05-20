package scheduler

import (
	"time"

	"github.com/amd-gpu-scheduler/pkg/types"
)

// UpdateGPUUsage updates the usage stats for a GPU by ID.
// Returns true if the GPU was found and updated.
func (s *Scheduler) UpdateGPUUsage(id string, usage types.GPUUsage) bool {
	for _, gpu := range s.gpus {
		if gpu.Info.ID == id {
			usage.ID = id
			usage.LastUpdated = time.Now()
			gpu.Usage = usage
			return true
		}
	}
	return false
}
