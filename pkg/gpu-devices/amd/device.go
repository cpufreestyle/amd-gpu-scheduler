// pkg/gpu-devices/amd/device.go
// AMD GPU device implementation for Windows
// Calls rocm-smi to get GPU information

package amd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"

	"github.com/hybrid-gpu-scheduler/pkg/gpu-devices/common"
)

// AMDDevice represents an AMD GPU device
type AMDDevice struct {
	Index         int     `json:"index"`
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	MemoryTotal   uint64  `json:"memory_total"`  // in MB
	MemoryUsed    uint64  `json:"memory_used"`   // in MB
	Utilization   float64 `json:"utilization"`   // in percent
	Temperature   int     `json:"temperature"`   // in Celsius
	PowerDraw     float64 `json:"power_draw"`    // in Watts
	FanSpeed      int     `json:"fan_speed"`     // in percent
	GPUClock      int     `json:"gpu_clock"`     // in MHz
	MemoryClock   int     `json:"memory_clock"`  // in MHz
	DriverVersion string  `json:"driver_version"`
	VBIOSVersion  string  `json:"vbios_version"`
}

// DeviceManager manages AMD GPU devices
type DeviceManager struct {
	devices []*AMDDevice
	mu      sync.RWMutex
}

var (
	globalManager *DeviceManager
	once         sync.Once
)

// GetDeviceManager returns the global AMD device manager
func GetDeviceManager() *DeviceManager {
	once.Do(func() {
		globalManager = &DeviceManager{
			devices: make([]*AMDDevice, 0),
		}
	})
	return globalManager
}

// Refresh queries rocm-smi to update device information
func (m *DeviceManager) Refresh() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Try to use rocm-smi to query GPU information
	// If rocm-smi is not available, try to use wmic to get basic info
	cmd := exec.Command("rocm-smi",
		"--showid",
		"--showproductname",
		"--showmeminfo", "vram",
		"--showusepercentage",
		"--showtemperature",
		"--showpower",
		"--showfanspeed",
		"--showclocks",
		"--showdriverversion",
		"--showvbiosversion",
		"--json")

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		// rocm-smi not available, fallback to wmic
		return m.refreshFallback()
	}

	// Parse JSON output
	var result map[string]interface{}
	err = json.Unmarshal(out.Bytes(), &result)
	if err != nil {
		return fmt.Errorf("failed to parse rocm-smi output: %v", err)
	}

	// Parse devices
	devices := make([]*AMDDevice, 0)
	for id, info := range result {
		dev := &AMDDevice{
			ID: id,
		}

		// Parse device info
		if infoMap, ok := info.(map[string]interface{}); ok {
			if name, ok := infoMap["Product Name"].(string); ok {
				dev.Name = name
			}
			if mem, ok := infoMap["VRAM Total"].(string); ok {
				dev.MemoryTotal = parseMemory(mem)
			}
			if mem, ok := infoMap["VRAM Used"].(string); ok {
				dev.MemoryUsed = parseMemory(mem)
			}
			if util, ok := infoMap["GPU Use (%)"].(float64); ok {
				dev.Utilization = util
			}
			if temp, ok := infoMap["Temperature"].(string); ok {
				dev.Temperature = parseTemperature(temp)
			}
			if power, ok := infoMap["Power Avg"].(string); ok {
				dev.PowerDraw = parsePower(power)
			}
			if fan, ok := infoMap["Fan Speed (%)"].(float64); ok {
				dev.FanSpeed = int(fan)
			}
			if clock, ok := infoMap["GPU Clock (MHz)"].(float64); ok {
				dev.GPUClock = int(clock)
			}
			if clock, ok := infoMap["Memory Clock (MHz)"].(float64); ok {
				dev.MemoryClock = int(clock)
			}
			if driver, ok := infoMap["Driver Version"].(string); ok {
				dev.DriverVersion = driver
			}
			if vbios, ok := infoMap["VBIOS Version"].(string); ok {
				dev.VBIOSVersion = vbios
			}
		}

		// Try to extract index from ID
		fmt.Sscanf(id, "%d", &dev.Index)

		devices = append(devices, dev)
	}

	m.devices = devices
	return nil
}

// refreshFallback uses wmic to get basic GPU info (fallback when rocm-smi is not available)
func (m *DeviceManager) refreshFallback() error {
	// Use wmic to get basic GPU info
	cmd := exec.Command("wmic", "path", "Win32_VideoController", "get",
		"Name,AdapterRAM,Status,Availability", "/format:csv")

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to execute wmic: %v, stderr: %s", err, stderr.String())
	}

	// Parse output
	lines := strings.Split(out.String(), "\n")
	devices := make([]*AMDDevice, 0)

	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Node") {
			continue
		}

		// Parse CSV line: Node,Name,AdapterRAM,Status,Availability
		parts := strings.Split(line, ",")
		if len(parts) < 5 {
			continue
		}

		dev := &AMDDevice{
			Index: i - 1, // Start from 0
			Name:  strings.TrimSpace(parts[1]),
		}

		// AdapterRAM (in bytes) -> MB
		if ram, err := strconv.ParseUint(strings.TrimSpace(parts[2]), 10, 64); err == nil {
			dev.MemoryTotal = ram / 1024 / 1024 // bytes -> MB
		}

		// Estimate used memory (cannot get from wmic)
		dev.MemoryUsed = dev.MemoryTotal / 2 // Rough estimate: 50% used

		// Cannot get these from wmic, set defaults
		dev.Utilization = 0
		dev.Temperature = 0
		dev.PowerDraw = 0
		dev.FanSpeed = 0
		dev.GPUClock = 0
		dev.MemoryClock = 0
		dev.DriverVersion = "Unknown"
		dev.VBIOSVersion = "Unknown"

		// Only add AMD GPUs (heuristic: check if name contains "AMD" or "Radeon")
		nameLower := strings.ToLower(dev.Name)
		if strings.Contains(nameLower, "amd") || strings.Contains(nameLower, "radeon") {
			devices = append(devices, dev)
		}
	}

	m.devices = devices
	return nil
}

// parseMemory parses memory string (e.g., "8 GB" -> 8192 MB)
func parseMemory(s string) uint64 {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, " GB") {
		val, err := strconv.ParseFloat(strings.TrimSuffix(s, " GB"), 64)
		if err != nil {
			return 0
		}
		return uint64(val * 1024) // GB -> MB
	}
	if strings.HasSuffix(s, " MB") {
		val, err := strconv.ParseFloat(strings.TrimSuffix(s, " MB"), 64)
		if err != nil {
			return 0
		}
		return uint64(val)
	}
	return 0
}

// parseTemperature parses temperature string (e.g., "65 C" -> 65)
func parseTemperature(s string) int {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " C")
	s = strings.TrimSuffix(s, " °C")
	val, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return val
}

// parsePower parses power string (e.g., "120 W" -> 120.0)
func parsePower(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " W")
	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return val
}

// GetDevices returns all AMD GPU devices
func (m *DeviceManager) GetDevices() []*AMDDevice {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.devices
}

// GetDeviceByIndex returns a device by index
func (m *DeviceManager) GetDeviceByIndex(index int) (*AMDDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dev := range m.devices {
		if dev.Index == index {
			return dev, nil
		}
	}
	return nil, fmt.Errorf("device with index %d not found", index)
}

// GetDeviceByID returns a device by ID
func (m *DeviceManager) GetDeviceByID(id string) (*AMDDevice, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, dev := range m.devices {
		if dev.ID == id {
			return dev, nil
		}
	}
	return nil, fmt.Errorf("device with ID %s not found", id)
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
			Index:         dev.Index,
			ID:            dev.ID,
			Name:          dev.Name,
			MemoryTotal:   dev.MemoryTotal,
			MemoryUsed:    dev.MemoryUsed,
			Utilization:   dev.Utilization,
			Temperature:   dev.Temperature,
			PowerDraw:     dev.PowerDraw,
			FanSpeed:      dev.FanSpeed,
			DriverVersion:  dev.DriverVersion,
			Vendor:        "AMD",
		}
		gpus = append(gpus, gpu)
	}

	return gpus, nil
}
