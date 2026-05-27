package types

import (
	"testing"
)

func TestGPUSnapshot_FreeVRAM(t *testing.T) {
	tests := []struct {
		name     string
		gpu      *GPUSnapshot
		expected int
	}{
		{
			name: "16GB GPU with 0MB used",
			gpu: &GPUSnapshot{
				Info: GPUInfo{
					VRAMMB: 16384,
				},
				Usage: GPUUsage{
					UsedVRAMMB: 0,
				},
			},
			expected: 16384,
		},
		{
			name: "16GB GPU with 8192MB used",
			gpu: &GPUSnapshot{
				Info: GPUInfo{
					VRAMMB: 16384,
				},
				Usage: GPUUsage{
					UsedVRAMMB: 8192,
				},
			},
			expected: 8192,
		},
		{
			name: "24GB GPU with 0MB used",
			gpu: &GPUSnapshot{
				Info: GPUInfo{
					VRAMMB: 24576,
				},
				Usage: GPUUsage{
					UsedVRAMMB: 0,
				},
			},
			expected: 24576,
		},
		{
			name: "24GB GPU with 12288MB used (50%)",
			gpu: &GPUSnapshot{
				Info: GPUInfo{
					VRAMMB: 24576,
				},
				Usage: GPUUsage{
					UsedVRAMMB: 12288,
				},
			},
			expected: 12288,
		},
		{
			name: "GPU with all VRAM used",
			gpu: &GPUSnapshot{
				Info: GPUInfo{
					VRAMMB: 16384,
				},
				Usage: GPUUsage{
					UsedVRAMMB: 16384,
				},
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.gpu.FreeVRAM()
			if result != tt.expected {
				t.Errorf("FreeVRAM() = %d, expected %d", result, tt.expected)
			}
		})
	}
}

func TestGPUSnapshot_IsHealthy(t *testing.T) {
	tests := []struct {
		name     string
		gpu      *GPUSnapshot
		expected bool
	}{
		{
			name: "Healthy GPU with 0% utilization",
			gpu: &GPUSnapshot{
				Usage: GPUUsage{
					Utilization: 0,
				},
			},
			expected: true,
		},
		{
			name: "Healthy GPU with 50% utilization",
			gpu: &GPUSnapshot{
				Usage: GPUUsage{
					Utilization: 50,
				},
			},
			expected: true,
		},
		{
			name: "Healthy GPU with 100% utilization",
			gpu: &GPUSnapshot{
				Usage: GPUUsage{
					Utilization: 100,
				},
			},
			expected: true,
		},
		{
			name: "Unhealthy GPU with negative utilization",
			gpu: &GPUSnapshot{
				Usage: GPUUsage{
					Utilization: -1,
				},
			},
			expected: false,
		},
		{
			name: "Unhealthy GPU with over 100% utilization",
			gpu: &GPUSnapshot{
				Usage: GPUUsage{
					Utilization: 101,
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.gpu.IsHealthy()
			if result != tt.expected {
				t.Errorf("IsHealthy() = %v, expected %v", result, tt.expected)
			}
		})
	}
}

func TestGPUSnapshot_CanAllocate(t *testing.T) {
	gpu := &GPUSnapshot{
		Info: GPUInfo{
			VRAMMB: 16384,
		},
		Usage: GPUUsage{
			UsedVRAMMB: 8192,
		},
	}

	tests := []struct {
		name     string
		vramMB   int
		expected bool
	}{
		{
			name:     "Can allocate 8192MB (exact free)",
			vramMB:   8192,
			expected: true,
		},
		{
			name:     "Can allocate 4096MB (less than free)",
			vramMB:   4096,
			expected: true,
		},
		{
			name:     "Cannot allocate 8193MB (more than free)",
			vramMB:   8193,
			expected: false,
		},
		{
			name:     "Cannot allocate 0MB",
			vramMB:   0,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := gpu.CanAllocate(tt.vramMB)
			if result != tt.expected {
				t.Errorf("CanAllocate(%d) = %v, expected %v", tt.vramMB, result, tt.expected)
			}
		})
	}
}
