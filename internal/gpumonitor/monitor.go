package gpumonitor

import (
	"encoding/json"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// GPUMonitor periodically reads real GPU stats and updates a callback.
type GPUMonitor struct {
	mu       sync.RWMutex
	stopCh   chan struct{}
	running  bool
	onUpdate func(gpuID string, usage GPUMonitorUpdate)
}

// GPUMonitorUpdate holds the data to push back to the scheduler.
type GPUMonitorUpdate struct {
	UtilizationPct float64
	TemperatureC    int
	PowerDrawW      float64
	UsedVRAMMB      int
	MemoryTotalMB   int
}

// NewGPUMonitor creates a monitor. onUpdate is called with each GPU's fresh stats.
func NewGPUMonitor(onUpdate func(gpuID string, usage GPUMonitorUpdate)) *GPUMonitor {
	return &GPUMonitor{
		stopCh:   make(chan struct{}),
		onUpdate: onUpdate,
	}
}

// Start begins periodic polling. Interval defaults to 5s.
func (m *GPUMonitor) Start(interval time.Duration) {
	if interval <= 0 {
		interval = 5 * time.Second
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.mu.Unlock()

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		// Run once immediately
		m.pollAll()
		for {
			select {
			case <-m.stopCh:
				return
			case <-ticker.C:
				m.pollAll()
			}
		}
	}()
	log.Println("[gpumonitor] started, interval =", interval)
}

// Stop stops the monitor.
func (m *GPUMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return
	}
	close(m.stopCh)
	m.running = false
	log.Println("[gpumonitor] stopped")
}

// pollAll queries all GPUs.
func (m *GPUMonitor) pollAll() {
	m.pollNVIDIA()
	m.pollAMD()
}

// pollNVIDIA queries nvidia-smi for all NVIDIA GPUs.
func (m *GPUMonitor) pollNVIDIA() {
	// Try common nvidia-smi paths on Windows
	paths := []string{
		"nvidia-smi",
		`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
		`C:\Windows\System32\nvidia-smi.exe`,
	}
	var cmd *exec.Cmd
	var out []byte
	var err error

	for _, p := range paths {
		cmd = exec.Command(p,
			"--query-gpu=index,name,utilization.gpu,temperature.gpu,power.draw,memory.used,memory.total",
			"--format=csv,noheader,nounits")
		out, err = cmd.Output()
		if err == nil {
			break
		}
	}
	if err != nil {
		// nvidia-smi not available; silently skip
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) < 7 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		gpuID := strconv.Itoa(idx)
		util, _ := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		temp, _ := strconv.Atoi(strings.TrimSpace(fields[3]))
		power, _ := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)
		memUsed, _ := strconv.Atoi(strings.TrimSpace(fields[5]))
		memTotal, _ := strconv.Atoi(strings.TrimSpace(fields[6]))

		if m.onUpdate != nil {
			m.onUpdate(gpuID, GPUMonitorUpdate{
				UtilizationPct: float64(util),
				TemperatureC:    temp,
				PowerDrawW:      power,
				UsedVRAMMB:      memUsed,
				MemoryTotalMB:   memTotal,
			})
		}
	}
}

// pollAMD queries rocm-smi for all AMD GPUs.
func (m *GPUMonitor) pollAMD() {
	paths := []string{"rocm-smi", `C:\Program Files\AMD\ROCm\bin\rocm-smi.exe`}
	var cmd *exec.Cmd
	var out []byte
	var err error

	for _, p := range paths {
		cmd = exec.Command(p, "--showuse", "--json")
		out, err = cmd.Output()
		if err == nil {
			m.parseAMDJSON(out)
			return
		}
		// Try without --json
		cmd = exec.Command(p, "--showuse")
		out, err = cmd.Output()
		if err == nil {
			m.parseAMDText(string(out))
			return
		}
	}
}

// parseAMDJSON parses `rocm-smi --showuse --json` output.
func (m *GPUMonitor) parseAMDJSON(data []byte) {
	var result map[string]map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return
	}
	for gpuKey, info := range result {
		// gpuKey looks like "card0" or "0"
		gpuID := gpuKey
		if strings.HasPrefix(gpuKey, "card") {
			gpuID = strings.TrimPrefix(gpuKey, "card")
		}
		usage := GPUMonitorUpdate{}
		if v, ok := info["GPU use (%)"].(float64); ok {
			usage.UtilizationPct = v
		}
		if v, ok := info["Temperature (C)"].(float64); ok {
			usage.TemperatureC = int(v)
		}
		if v, ok := info["Power (W)"].(float64); ok {
			usage.PowerDrawW = v
		}
		if v, ok := info["VRAM Total (MB)"].(float64); ok {
			usage.MemoryTotalMB = int(v)
		}
		if v, ok := info["VRAM Used (MB)"].(float64); ok {
			usage.UsedVRAMMB = int(v)
		}
		if m.onUpdate != nil {
			m.onUpdate(gpuID, usage)
		}
	}
}

// parseAMDText parses text output from rocm-smi (fallback).
func (m *GPUMonitor) parseAMDText(output string) {
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "GPU") && strings.Contains(line, "%") {
			// Rough parsing; rocm-smi text format varies
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				if util, err := strconv.ParseFloat(fields[1], 64); err == nil {
					// Assign to GPU 1 (AMD card) by default
					if m.onUpdate != nil {
						m.onUpdate("1", GPUMonitorUpdate{UtilizationPct: util})
					}
				}
			}
		}
	}
}

// SimulateChanges generates smooth simulated data for when real GPUs aren't available.
// Call this instead of pollAll() when no real GPU tool is found.
func (m *GPUMonitor) SimulateChanges(gpuIDs []string, getCurrentUsage func(gpuID string) GPUMonitorUpdate) {
	for _, id := range gpuIDs {
		current := GPUMonitorUpdate{}
		if getCurrentUsage != nil {
			current = getCurrentUsage(id)
		}
		// Small random walk to simulate changing utilization
		util := current.UtilizationPct + (float64(time.Now().UnixNano()%100)/100.0 - 0.5)
		if util < 0 {
			util = 0
		}
		if util > 100 {
			util = 100
		}
		if m.onUpdate != nil {
			m.onUpdate(id, GPUMonitorUpdate{
				UtilizationPct: util,
				TemperatureC:    current.TemperatureC,
				PowerDrawW:      current.PowerDrawW,
				UsedVRAMMB:      current.UsedVRAMMB,
				MemoryTotalMB:   current.MemoryTotalMB,
			})
		}
	}
}
