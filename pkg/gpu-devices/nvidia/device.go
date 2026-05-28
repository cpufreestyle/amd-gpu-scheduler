// pkg/gpu-devices/nvidia/device.go
// NVIDIA GPU device implementation for Windows
// Calls nvidia-smi to get GPU information

package nvidia

import (
	"bytes"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/hybrid-gpu-scheduler/pkg/gpu-devices/common"
)

// NvidiaDevice represents an NVIDIA GPU device
type NvidiaDevice struct {
	Index      int     `json:"index"`
	UUID       string  `json:"uuid"`
	Name       string  `json:"name"`
	MemoryTotal uint64  `json:"memory_total"` // in MB
	MemoryUsed  uint64  `json:"memory_used"`  // in MB
	Utilization float64 `json:"utilization"`  // in percent
	Temperature int     `json:"temperature"`  // in Celsius
	PowerDraw   float64 `json:"power_draw"`   // in Watts
	FanSpeed    int     `json:"fan_speed"`    // in percent
	DriverVersion string `json:"driver_version"`
	CUDAVersion  string `json:"cuda_version"`
}

// DeviceManager manages NVIDIA GPU devices
type DeviceManager struct {
	devices []*NvidiaDevice
	mu      sync.RWMutex
}

var (
	globalManager *DeviceManager
	once         sync.Once
)

// GetDeviceManager returns the global NVIDIA device manager
func GetDeviceManager() *DeviceManager {
	once.Do(func() {
		globalManager = &DeviceManager{
			devices: make([]*NvidiaDevice, 0),
		}
	})
	return globalManager
}

// Refresh queries nvidia-smi to update device information
func (m *DeviceManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Query nvidia-smi for GPU information
	// Query: index, uuid, name, memory.total, memory.used, utilization.gpu, temperature.gpu, power.draw, fan.speed
	cmd := exec.Command("nvidia-smi",
		"--query-gpu=index,uuid,name,memory.total,memory.used,utilization.gpu,temperature.gpu,power.draw,fan.speed,driver_version,cuda_version",
		"--format=csv,noheader,nounits")

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute nvidia-smi: %v, stderr: %s", err, stderr.String())
	}

	// Parse output
	lines := strings.Split(out.String(), "\n")
	devices := make([]*NvidiaDevice, 0)

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse CSV line: index, uuid, name, memory.total [MiB], memory.used [MiB], utilization.gpu [%], temperature.gpu [C], power.draw [W], fan.speed [%], driver_version, cuda_version
		parts := strings.Split(line, ",")
		if len(parts) < 11 {
			continue
		}

		dev := &NvidiaDevice{}

		// Index
		idx, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil {
			continue
		}
		dev.Index = idx

		// UUID
		dev.UUID = strings.TrimSpace(parts[1])

		// Name
		dev.Name = strings.TrimSpace(parts[2])

		// Memory Total (MiB)
		memTotalStr := strings.TrimSpace(strings.Split(parts[3], "[")[0])
		memTotal, err := strconv.ParseUint(memTotalStr, 10, 64)
		if err != nil {
			dev.MemoryTotal = 0
		} else {
			dev.MemoryTotal = memTotal
		}

		// Memory Used (MiB)
		memUsedStr := strings.TrimSpace(strings.Split(parts[4], "[")[0])
		memUsed, err := strconv.ParseUint(memUsedStr, 10, 64)
		if err != nil {
			dev.MemoryUsed = 0
		} else {
			dev.MemoryUsed = memUsed
		}

		// Utilization (%)
		utilStr := strings.TrimSpace(strings.Split(parts[5], "[")[0])
		util, err := strconv.ParseFloat(utilStr, 64)
		if err != nil {
			dev.Utilization = 0
		} else {
			dev.Utilization = util
		}

		// Temperature (C)
		tempStr := strings.TrimSpace(strings.Split(parts[6], "[")[0])
		temp, err := strconv.Atoi(tempStr)
		if err != nil {
			dev.Temperature = 0
		} else {
			dev.Temperature = temp
		}

		// Power Draw (W)
		powerStr := strings.TrimSpace(strings.Split(parts[7], "[")[0])
		power, err := strconv.ParseFloat(powerStr, 64)
		if err != nil {
			dev.PowerDraw = 0
		} else {
			dev.PowerDraw = power
		}

		// Fan Speed (%)
		fanStr := strings.TrimSpace(strings.Split(parts[8], "[")[0])
		fan, err := strconv.Atoi(fanStr)
		if err != nil {
			dev.FanSpeed = 0
		} else {
			dev.FanSpeed = fan
		}

		// Driver Version
		dev.DriverVersion = strings.TrimSpace(parts[9])

		// CUDA Version
		dev.CUDAVersion = strings.TrimSpace(parts[10])

		devices = append(devices, dev)
	}

	m.devices = devices
	return nil
}

// GetDevices returns all NVIDIA GPU devices
func (m *DeviceManager) GetDevices() []*NvidiaDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.devices
}

// GetDeviceByIndex returns a device by index
func (m *DeviceManager) GetDeviceByIndex(index int) (*NvidiaDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dev := range m.devices {
		if dev.Index == index {
			return dev, nil
		}
	}
	return nil, fmt.Errorf("device with index %d not found", index)
}

// GetDeviceByUUID returns a device by UUID
func (m *DeviceManager) GetDeviceByUUID(uuid string) (*NvidiaDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dev := range m.devices {
		if dev.UUID == uuid {
			return dev, nil
		}
	}
	return nil, fmt.Errorf("device with UUID %s not found", uuid)
}

// GetMemoryUsage returns memory usage (used/total) for a device
func (m *DeviceManager) GetMemoryUsage(index int) (used uint64, total uint64, err error) {
	dev, err := m.GetDeviceByIndex(index)
	if err != nil {
		return 0, 0, err
	}
	return dev.MemoryUsed, dev.MemoryTotal, nil
}

// GetUtilization returns GPU utilization (%) for a device
func (m *DeviceManager) GetUtilization(index int) (float64, error) {
	dev, err := m.GetDeviceByIndex(index)
	if err != nil {
		return 0, err
	}
	return dev.Utilization, nil
}

// GetGPUInfo returns a common.GPUInfo struct for integration with hybrid-gpu-scheduler
func (m *DeviceManager) GetGPUInfo() ([]common.GPUInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	gpus := make([]common.GPUInfo, 0, len(m.devices))

	for _, dev := range m.devices {
		gpu := common.GPUInfo{
			Index:        dev.Index,
			UUID:         dev.UUID,
			Name:         dev.Name,
			MemoryTotal:  dev.MemoryTotal,
			MemoryUsed:   dev.MemoryUsed,
			Utilization:  dev.Utilization,
			Temperature:  dev.Temperature,
			PowerDraw:    dev.PowerDraw,
			FanSpeed:     dev.FanSpeed,
			DriverVersion: dev.DriverVersion,
			CUDAVersion:  dev.CUDAVersion,
			Vendor:       "NVIDIA",
		}
		gpus = append(gpus, gpu)
	}

	return gpus, nil
}
