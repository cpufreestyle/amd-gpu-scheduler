package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/amd-gpu-scheduler/pkg/types"
)

// Executor manages GPU task execution
type Executor struct {
	mu        sync.RWMutex
	running   map[string]*RunningTask
	taskQueue chan *types.Task
	ctx       context.Context
	cancel    context.CancelFunc
}

// RunningTask represents a task currently executing
type RunningTask struct {
	Task     *types.Task
	Cmd      *exec.Cmd
	PID      int
	StartTime time.Time
	GPUID    string
	LogFile  string
}

var globalExecutor *Executor
var once sync.Once

// GetExecutor returns the singleton executor instance
func GetExecutor() *Executor {
	once.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		globalExecutor = &Executor{
			running:   make(map[string]*RunningTask),
			taskQueue: make(chan *types.Task, 100),
			ctx:       ctx,
			cancel:    cancel,
		}
		go globalExecutor.processQueue()
	})
	return globalExecutor
}

// SubmitTask submits a task for execution
func (e *Executor) SubmitTask(task *types.Task, gpuID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Check if task already running
	if _, exists := e.running[task.ID]; exists {
		return fmt.Errorf("task %s is already running", task.ID)
	}

	// Create log directory
	logDir := "logs"
	os.MkdirAll(logDir, 0755)

	// Create log file
	logFile := filepath.Join(logDir, fmt.Sprintf("task-%s-%d.log", task.ID, time.Now().Unix()))
	f, err := os.Create(logFile)
	if err != nil {
		return fmt.Errorf("failed to create log file: %v", err)
	}
	f.Close()

	// Build command based on task type
	var cmd *exec.Cmd
	switch task.Type {
	case "training":
		cmd = e.buildTrainingCommand(task, gpuID)
	case "inference":
		cmd = e.buildInferenceCommand(task, gpuID)
	case "compute":
		cmd = e.buildComputeCommand(task, gpuID)
	default:
		cmd = e.buildDefaultCommand(task, gpuID)
	}

	if cmd == nil {
		return fmt.Errorf("failed to build command for task type: %s", task.Type)
	}

	// Set environment variables for GPU isolation
	cmd.Env = e.buildEnv(task, gpuID)

	// Redirect output to log file
	cmd.Stdout, _ = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	cmd.Stderr, _ = os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start task: %v", err)
	}

	// Track the running task
	now := time.Now()
	runningTask := &RunningTask{
		Task:     task,
		Cmd:      cmd,
		PID:      cmd.Process.Pid,
		StartTime: now,
		GPUID:    gpuID,
		LogFile:  logFile,
	}
	e.running[task.ID] = runningTask

	// Update task fields
	task.Status = "running"
	task.PID = cmd.Process.Pid
	task.StartTime = &now
	task.LogFile = logFile

	log.Printf("✅ Started task %s (PID: %d) on GPU %s", task.ID, runningTask.PID, gpuID)
	log.Printf("   Log: %s", logFile)

	// Monitor task completion in background
	go e.monitorTask(task.ID, cmd)

	return nil
}

// buildTrainingCommand builds command for training tasks
func (e *Executor) buildTrainingCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return exec.Command("sh", "-c", task.Command)
	}
	// Default: simulate training with GPU stress
	return exec.Command("nvidia-smi", "nvlink", "-s")
}

// buildInferenceCommand builds command for inference tasks
func (e *Executor) buildInferenceCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return exec.Command("sh", "-c", task.Command)
	}
	// Default: query GPU status
	if strings.HasPrefix(gpuID, "0") {
		return exec.Command("nvidia-smi", "--query-gpu=utilization.gpu,memory.used", "--format=csv")
	}
	return exec.Command("rocm-smi", "--showuse")
}

// buildComputeCommand builds command for general compute tasks
func (e *Executor) buildComputeCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return exec.Command("sh", "-c", task.Command)
	}
	// Default: GPU compute test
	return exec.Command("nvidia-smi", "nvlink", "-s")
}

// buildDefaultCommand builds default command
func (e *Executor) buildDefaultCommand(task *types.Task, gpuID string) *exec.Cmd {
	if task.Command != "" {
		return exec.Command("sh", "-c", task.Command)
	}
	return exec.Command("echo", fmt.Sprintf("Task %s running on GPU %s", task.ID, gpuID))
}

// buildEnv builds environment variables for GPU isolation
func (e *Executor) buildEnv(task *types.Task, gpuID string) []string {
	env := os.Environ()

	// Remove existing GPU visibility settings
	newEnv := make([]string, 0, len(env))
	for _, e := range env {
		if !strings.HasPrefix(e, "CUDA_VISIBLE_DEVICES=") &&
			!strings.HasPrefix(e, "ROCR_VISIBLE_DEVICES=") {
			newEnv = append(newEnv, e)
		}
	}

	// Add GPU isolation variables
	newEnv = append(newEnv, fmt.Sprintf("CUDA_VISIBLE_DEVICES=%s", gpuID))
	newEnv = append(newEnv, fmt.Sprintf("ROCR_VISIBLE_DEVICES=%s", gpuID))

	// Add task-specific environment variables
	if task.Env != nil {
		for k, v := range task.Env {
			newEnv = append(newEnv, fmt.Sprintf("%s=%s", k, v))
		}
	}

	return newEnv
}

// monitorTask monitors a running task and updates status on completion
func (e *Executor) monitorTask(taskID string, cmd *exec.Cmd) {
	err := cmd.Wait()
	
	e.mu.Lock()
	defer e.mu.Unlock()

	if running, exists := e.running[taskID]; exists {
		now := time.Now()
		running.Task.EndTime = &now
		
		if err != nil {
			log.Printf("❌ Task %s failed: %v", taskID, err)
			running.Task.Status = "failed"
			running.Task.Error = err.Error()
			running.Task.ExitCode = 1
		} else {
			log.Printf("✅ Task %s completed", taskID)
			running.Task.Status = "completed"
			running.Task.ExitCode = 0
		}
	}
}

// GetRunningTask returns a running task by ID
func (e *Executor) GetRunningTask(taskID string) (*RunningTask, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	task, ok := e.running[taskID]
	return task, ok
}

// StopTask stops a running task
func (e *Executor) StopTask(taskID string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	running, exists := e.running[taskID]
	if !exists {
		return fmt.Errorf("task %s is not running", taskID)
	}

	// Kill the process
	if err := running.Cmd.Process.Kill(); err != nil {
		return fmt.Errorf("failed to kill task %s: %v", taskID, err)
	}

	now := time.Now()
	running.Task.Status = "killed"
	running.Task.EndTime = &now
	
	log.Printf("🛑 Killed task %s (PID: %d)", taskID, running.PID)

	return nil
}

// ListRunningTasks returns all running tasks
func (e *Executor) ListRunningTasks() []*RunningTask {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tasks := make([]*RunningTask, 0, len(e.running))
	for _, task := range e.running {
		tasks = append(tasks, task)
	}
	return tasks
}

// processQueue processes tasks from the queue
func (e *Executor) processQueue() {
	for {
		select {
		case task := <-e.taskQueue:
			// Find best GPU for this task
			// This should be called from scheduler
			log.Printf("Task %s queued for execution", task.ID)
		case <-e.ctx.Done():
			return
		}
	}
}

// Shutdown stops all running tasks
func (e *Executor) Shutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()

	log.Println("🛑 Shutting down executor...")
	for taskID, running := range e.running {
		if running.Cmd.Process != nil {
			running.Cmd.Process.Kill()
			log.Printf("   Killed task %s", taskID)
		}
	}
	e.cancel()
}
