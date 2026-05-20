//go:build linux

package executor

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

func setupSysProcAttr(cmd *exec.Cmd) {
	// No-op on Linux
}

func (e *Executor) buildTrainingCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("nvidia-smi", "nvlink", "-s")
}

func (e *Executor) buildInferenceCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	if strings.HasPrefix(gpuID, "0") {
		return exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,memory.used", "--format=csv")
	}
	return exec.Command("rocm-smi", "--showuse")
}

func (e *Executor) buildComputeCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("nvidia-smi", "nvlink", "-s")
}

func (e *Executor) buildDefaultCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("echo", fmt.Sprintf("Task %s running on GPU %s", task.ID, gpuID))
}

func (e *Executor) killProcessTree(pid int) error {
	// Kill the process group first
	pgKill := exec.Command("kill", "-9", "--", fmt.Sprintf("-%d", pid))
	_ = pgKill.Run()
	// Then try direct kill
	kill := exec.Command("kill", "-9", fmt.Sprintf("%d", pid))
	_ = kill.Run()
	return nil
}
