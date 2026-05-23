package agent

import (
	"encoding/json"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// LocalGPUGetter queries GPU tools directly for agent's GPU snapshots
type LocalGPUGetter struct{}

// NewLocalGPUGetter creates a new local GPU getter
func NewLocalGPUGetter() *LocalGPUGetter {
	return &LocalGPUGetter{}
}

// GetGPUSnapshots returns current GPU snapshots for this node
func (g *LocalGPUGetter) GetGPUSnapshots() []*types.GPUSnapshot {
	var snaps []*types.GPUSnapshot
	g.snapsNVIDIA(&snaps)
	g.snapsAMD(&snaps)
	return snaps
}

func (g *LocalGPUGetter) snapsNVIDIA(snaps *[]*types.GPUSnapshot) {
	paths := []string{
		"nvidia-smi",
		`C:\Program Files\NVIDIA Corporation\NVSMI\nvidia-smi.exe`,
		`C:\Windows\System32\nvidia-smi.exe`,
	}
	var out []byte
	for _, p := range paths {
		cmd := exec.Command(p,
			"--query-gpu=index,name,utilization.gpu,temperature.gpu,power.draw,memory.used,memory.total",
			"--format=csv,noheader,nounits")
		var err error
		out, err = cmd.Output()
		if err == nil {
			break
		}
	}
	if len(out) == 0 {
		return
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		fields := strings.Split(line, ",")
		if len(fields) < 7 {
			continue
		}
		idx, _ := strconv.Atoi(strings.TrimSpace(fields[0]))
		util, _ := strconv.ParseFloat(strings.TrimSpace(fields[2]), 64)
		temp, _ := strconv.Atoi(strings.TrimSpace(fields[3]))
		power, _ := strconv.ParseFloat(strings.TrimSpace(fields[4]), 64)
		memUsed, _ := strconv.Atoi(strings.TrimSpace(fields[5]))
		memTotal, _ := strconv.Atoi(strings.TrimSpace(fields[6]))

		*snaps = append(*snaps, &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:           strconv.Itoa(idx),
				Type:         types.GPUTypeNVIDIA,
				Name:         strings.TrimSpace(fields[1]),
				VRAMMB:       memTotal,
				Architecture: "GPU",
			},
			Usage: types.GPUUsage{
				ID:             strconv.Itoa(idx),
				UsedVRAMMB:     memUsed,
				Utilization:    util,
				MemoryUtil:     float64(memUsed) / float64(memTotal) * 100,
				TemperatureC:   temp,
				PowerDrawW:      power,
				RunningTasks:    0,
				LastUpdated:     time.Now(),
			},
		})
	}
}

func (g *LocalGPUGetter) snapsAMD(snaps *[]*types.GPUSnapshot) {
	paths := []string{"rocm-smi", `C:\Program Files\AMD\ROCm\bin\rocm-smi.exe`}
	var out []byte
	for _, p := range paths {
		cmd := exec.Command(p, "--showuse", "--showid", "--showmeminfo", "vbot", "--json")
		var err error
		out, err = cmd.Output()
		if err == nil {
			g.parseAMDJSON(snaps, out)
			return
		}
	}
}

func (g *LocalGPUGetter) parseAMDJSON(snaps *[]*types.GPUSnapshot, data []byte) {
	var result map[string]map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return
	}
	for gpuKey, info := range result {
		gpuID := strings.TrimPrefix(gpuKey, "card")
		used := 0
		total := 24576 // default 24GB
		if v, ok := info["VRAM Used (MB)"].(float64); ok {
			used = int(v)
		}
		if v, ok := info["VRAM Total (MB)"].(float64); ok {
			total = int(v)
		}
		util := 0.0
		if v, ok := info["GPU use (%)"].(float64); ok {
			util = v
		}
		temp := 0
		if v, ok := info["Temperature (C)"].(float64); ok {
			temp = int(v)
		}
		power := 0.0
		if v, ok := info["Power (W)"].(float64); ok {
			power = v
		}

		*snaps = append(*snaps, &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:           gpuID,
				Type:         types.GPUTypeAMD,
				Name:         "AMD GPU",
				VRAMMB:       total,
				Architecture: "RDNA",
			},
			Usage: types.GPUUsage{
				ID:             gpuID,
				UsedVRAMMB:     used,
				Utilization:    util,
				MemoryUtil:     float64(used) / float64(total) * 100,
				TemperatureC:   temp,
				PowerDrawW:     power,
				RunningTasks:   0,
				LastUpdated:    time.Now(),
			},
		})
	}
}
