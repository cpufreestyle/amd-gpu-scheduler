//go:build windows

package executor

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

func setupSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

func (e *Executor) buildTrainingCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("cmd.exe", "/c", fmt.Sprintf("echo training on GPU %s", gpuID))
}

func (e *Executor) buildInferenceCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("cmd.exe", "/c", fmt.Sprintf("echo inference on GPU %s", gpuID))
}

func (e *Executor) buildComputeCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("cmd.exe", "/c", fmt.Sprintf("echo compute on GPU %s", gpuID))
}

func (e *Executor) buildDefaultCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return e.buildCommandByOS(task.Command)
	}
	return exec.Command("cmd.exe", "/c", fmt.Sprintf("echo Task %s running on GPU %s", task.ID, gpuID))
}

func (e *Executor) killProcessTree(pid int) error {
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprintf("%d", pid))
	_ = killCmd.Run()
	return nil
}
