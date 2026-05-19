package types

import "time"

// GPUType represents the type of GPU
type GPUType string

const (
	GPUTypeNVIDIA GPUType = "nvidia"
	GPUTypeAMD    GPUType = "amd"
	GPUTypeUnknown GPUType = "unknown"
)

// SchedulingPolicy represents the scheduling strategy
type SchedulingPolicy string

const (
	PolicyBinpack  SchedulingPolicy = "binpack"  // 集中调度 - 尽量用少的车
	PolicySpread   SchedulingPolicy = "spread"   // 分散调度 - 负载均衡
	PolicyGPUType  SchedulingPolicy = "gpu_type" // GPU类型优先
)

// GPUInfo represents the static information of a GPU
type GPUInfo struct {
	ID          string   // GPU ID (e.g., "0", "1")
	Type        GPUType  // nvidia / amd
	Name        string   // GPU model name
	VRAMMB      int      // Total VRAM in MB
	CoreCount   int      // Number of compute cores
	ComputeUnits int     // Compute units (CUDA cores / CUs)
	DriverVersion string // Driver version
	Architecture string // Architecture (Ada, RDNA, etc.)
}

// GPUUsage represents the dynamic usage of a GPU
type GPUUsage struct {
	ID            string    // GPU ID
	UsedVRAMMB    int       // Used VRAM in MB
	Utilization   float64   // GPU utilization 0-100%
	MemoryUtil    float64   // Memory utilization 0-100%
	ComputeUtil   float64   // Compute utilization 0-100%
	RunningTasks  int       // Number of running tasks
	LastUpdated   time.Time // Last update time
}

// GPUSnapshot combines static and dynamic GPU information
type GPUSnapshot struct {
	Info   GPUInfo
	Usage  GPUUsage
	Score  float64 // Computed scheduling score
}

// NewGPUSnapshot creates a new GPU snapshot
func NewGPUSnapshot(info GPUInfo, usage GPUUsage) *GPUSnapshot {
	return &GPUSnapshot{
		Info:   info,
		Usage:  usage,
		Score:  0,
	}
}

// FreeVRAM returns the free VRAM in MB
func (g *GPUSnapshot) FreeVRAM() int {
	return g.Info.VRAMMB - g.Usage.UsedVRAMMB
}

// FreeVRAMPercent returns the free VRAM percentage
func (g *GPUSnapshot) FreeVRAMPercent() float64 {
	if g.Info.VRAMMB == 0 {
		return 0
	}
	return float64(g.FreeVRAM()) / float64(g.Info.VRAMMB) * 100
}

// IsHealthy returns true if the GPU is healthy
func (g *GPUSnapshot) IsHealthy() bool {
	return g.Usage.Utilization >= 0 && g.Usage.Utilization <= 100
}

// CanAllocate returns true if the GPU can allocate the requested VRAM
func (g *GPUSnapshot) CanAllocate(vramMB int) bool {
	return g.FreeVRAM() >= vramMB
}

// SchedulingCriteria represents the criteria for scheduling a task
type SchedulingCriteria struct {
	VRAMRequiredMB int            // Required VRAM in MB
	PreferredGPU   GPUType       // Preferred GPU type (optional)
	TaskType       string        // "training", "inference", "compute"
	Priority       int           // Task priority (1-10)
	Policy         SchedulingPolicy // Scheduling policy
}

// ScoredGPU wraps a GPU with its scheduling score
type ScoredGPU struct {
	GPU      *GPUSnapshot
	Score    float64
	Reasons  []string // Why this GPU was selected
}

// SchedulingResult contains the result of scheduling
type SchedulingResult struct {
	Task     *Task
	AssignedGPU *GPUSnapshot
	Score    float64
	Success  bool
	Message  string
}
