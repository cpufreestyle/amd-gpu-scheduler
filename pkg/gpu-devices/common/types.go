// pkg/gpu-devices/common/types.go
// Common types for GPU device abstraction layer

package common

// GPUInfo represents basic GPU information
type GPUInfo struct {
	Index         int     `json:"index"`
	ID            string  `json:"id"`
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	MemoryTotal   uint64  `json:"memory_total"`   // in MB
	MemoryUsed    uint64  `json:"memory_used"`    // in MB
	Utilization   float64 `json:"utilization"`    // in percent
	Temperature   int     `json:"temperature"`    // in Celsius
	PowerDraw     float64 `json:"power_draw"`     // in Watts
	FanSpeed      int     `json:"fan_speed"`      // in percent
	GPUClock      int     `json:"gpu_clock"`      // in MHz (optional)
	MemoryClock   int     `json:"memory_clock"`   // in MHz (optional)
	DriverVersion string  `json:"driver_version"`
	CUDAVersion   string  `json:"cuda_version"`    // NVIDIA only
	VBIOSVersion  string  `json:"vbios_version"`  // AMD only
	Vendor        string  `json:"vendor"`          // "NVIDIA" or "AMD"
}

// GPUManager interface defines common methods for GPU management
type GPUManager interface {
	// Refresh updates GPU information
	Refresh() error

	// GetDevices returns all GPU devices
	GetDevices() interface{}

	// GetGPUInfo returns GPUInfo structs for integration
	GetGPUInfo() ([]GPUInfo, error)

	// GetMemoryUsage returns memory usage (used, total) for a device
	GetMemoryUsage(index int) (used uint64, total uint64, err error)

	// GetUtilization returns GPU utilization (%) for a device
	GetUtilization(index int) (float64, error)
}

// SchedulerInfo is used by hybrid-gpu-scheduler for scheduling decisions
type SchedulerInfo struct {
	GPUs []GPUInfo `json:"gpus"`

	// Aggregated metrics
	TotalMemory   uint64  `json:"total_memory"`
	UsedMemory    uint64  `json:"used_memory"`
	AvgUtilization float64 `json:"avg_utilization"`
	MaxTemperature int     `json:"max_temperature"`

	// Scheduling hints
	BestGPUIndex   int     `json:"best_gpu_index"`   // Index of best GPU for next task
	BestGPUScore   float64 `json:"best_gpu_score"`   // Score (0-100)
}
