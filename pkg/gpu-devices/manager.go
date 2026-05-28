// pkg/gpu-devices/manager.go
// Unified GPU device manager for hybrid-gpu-scheduler
// Manages both NVIDIA and AMD GPUs on Windows

package gpudevices

import (
	"fmt"
	"sync"

	"github.com/hybrid-gpu-scheduler/pkg/gpu-devices/amd"
	"github.com/hybrid-gpu-scheduler/pkg/gpu-devices/common"
	"github.com/hybrid-gpu-scheduler/pkg/gpu-devices/nvidia"
)

var (
	globalManager *GPUDeviceManager
	once         sync.Once
)

// GPUDeviceManager manages all GPU devices (NVIDIA + AMD)
type GPUDeviceManager struct {
	nvidiaMgr *nvidia.DeviceManager
	amdMgr    *amd.DeviceManager
	mu         sync.RWMutex
}

// GetGPUDeviceManager returns the global GPU device manager
func GetGPUDeviceManager() *GPUDeviceManager {
	once.Do(func() {
		globalManager = &GPUDeviceManager{
			nvidiaMgr: nvidia.GetDeviceManager(),
			amdMgr:    amd.GetDeviceManager(),
		}
	})
	return globalManager
}

// Refresh updates all GPU information (NVIDIA + AMD)
func (m *GPUDeviceManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Refresh NVIDIA GPUs
	err := m.nvidiaMgr.Refresh()
	if err != nil {
		fmt.Printf("Warning: Failed to refresh NVIDIA GPUs: %v\n", err)
		// Don't return error, try AMD
	}

	// Refresh AMD GPUs
	err = m.amdMgr.Refresh()
	if err != nil {
		fmt.Printf("Warning: Failed to refresh AMD GPUs: %v\n", err)
		// Don't return error, might have NVIDIA
	}

	return nil
}

// GetAllGPUs returns all GPUs (NVIDIA + AMD) as common.GPUInfo
func (m *GPUDeviceManager) GetAllGPUs() ([]common.GPUInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var gpus []common.GPUInfo

	// Get NVIDIA GPUs
	nvidiaGPUs, err := m.nvidiaMgr.GetGPUInfo()
	if err != nil {
		fmt.Printf("Warning: Failed to get NVIDIA GPU info: %v\n", err)
	} else {
		gpus = append(gpus, nvidiaGPUs...)
	}

	// Get AMD GPUs
	amdGPUs, err := m.amdMgr.GetGPUInfo()
	if err != nil {
		fmt.Printf("Warning: Failed to get AMD GPU info: %v\n", err)
	} else {
		gpus = append(gpus, amdGPUs...)
	}

	if len(gpus) == 0 {
		return nil, fmt.Errorf("no GPU devices found")
	}

	return gpus, nil
}

// GetNvidiaGPUs returns only NVIDIA GPUs
func (m *GPUDeviceManager) GetNvidiaGPUs() ([]common.GPUInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.nvidiaMgr.GetGPUInfo()
}

// GetAMDGPUs returns only AMD GPUs
func (m *GPUDeviceManager) GetAMDGPUs() ([]common.GPUInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.amdMgr.GetGPUInfo()
}

// GetGPUMemoryUsage returns memory usage (used/total) for a GPU by index
func (m *GPUDeviceManager) GetGPUMemoryUsage(index int) (used uint64, total uint64, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try NVIDIA first
	used, total, err = m.nvidiaMgr.GetMemoryUsage(index)
	if err == nil {
		return used, total, nil
	}

	// Try AMD
	used, total, err = m.amdMgr.GetMemoryUsage(index)
	if err == nil {
		return used, total, nil
	}

	return 0, 0, fmt.Errorf("GPU with index %d not found", index)
}

// GetGPUUtilization returns GPU utilization (%) for a GPU by index
func (m *GPUDeviceManager) GetGPUUtilization(index int) (float64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Try NVIDIA first
	util, err := m.nvidiaMgr.GetUtilization(index)
	if err == nil {
		return util, nil
	}

	// Try AMD
	util, err = m.amdMgr.GetUtilization(index)
	if err == nil {
		return util, nil
	}

	return 0, fmt.Errorf("GPU with index %d not found", index)
}

// GetSchedulerInfo returns SchedulerInfo for hybrid-gpu-scheduler
func (m *GPUDeviceManager) GetSchedulerInfo() (*common.SchedulerInfo, error) {
	gpus, err := m.GetAllGPUs()
	if err != nil {
		return nil, err
	}

	info := &common.SchedulerInfo{
		GPUs: gpus,
	}

	// Calculate aggregated metrics
	var totalMemory, usedMemory uint64
	var totalUtil float64
	var maxTemp int

	for _, gpu := range gpus {
		totalMemory += gpu.MemoryTotal
		usedMemory += gpu.MemoryUsed
		totalUtil += gpu.Utilization
		if gpu.Temperature > maxTemp {
			maxTemp = gpu.Temperature
		}
	}

	info.TotalMemory = totalMemory
	info.UsedMemory = usedMemory
	info.AvgUtilization = totalUtil / float64(len(gpus))
	info.MaxTemperature = maxTemp

	// Find best GPU (highest free memory + lowest utilization)
	bestScore := -1.0
	for i, gpu := range gpus {
		// Score = free_memory_weight * free_memory_ratio + (1 - utilization_weight) * (1 - utilization_ratio)
		freeMemory := gpu.MemoryTotal - gpu.MemoryUsed
		freeMemoryRatio := float64(freeMemory) / float64(gpu.MemoryTotal)
		utilRatio := gpu.Utilization / 100.0

		score := 0.5 * freeMemoryRatio + 0.5 * (1 - utilRatio)

		if score > bestScore {
			bestScore = score
			info.BestGPUIndex = i
			info.BestGPUScore = score
		}
	}

	return info, nil
}

// PrintGPUStatus prints GPU status to console (for debugging)
func (m *GPUDeviceManager) PrintGPUStatus() {
	gpus, err := m.GetAllGPUs()
	if err != nil {
		fmt.Printf("Error getting GPU info: %v\n", err)
		return
	}

	fmt.Println("=== GPU Status ===")
	for _, gpu := range gpus {
		fmt.Printf("[%d] %s (%s)\n", gpu.Index, gpu.Name, gpu.Vendor)
		fmt.Printf("    Memory: %d / %d MB (%.1f%% used)\n",
			gpu.MemoryUsed, gpu.MemoryTotal,
			float64(gpu.MemoryUsed)/float64(gpu.MemoryTotal)*100)
		fmt.Printf("    Utilization: %.1f%%\n", gpu.Utilization)
		fmt.Printf("    Temperature: %d C\n", gpu.Temperature)
		fmt.Println()
	}
}
