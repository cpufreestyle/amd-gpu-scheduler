package executor

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// Executor manages GPU task execution
type Executor struct {
	mu        sync.RWMutex
	running   map[string]*RunningTask
	taskQueue chan *types.Task
	ctx       context.Context
	cancel    context.CancelFunc
	onTaskDone func(taskID string) // callback when task completes/fails/times out/killed
}

// RunningTask represents a task currently executing
type RunningTask struct {
	Task     *types.Task
	Cmd      *exec.Cmd
	PID      int
	StartTime time.Time
	GPUID    string
	LogFile  string
	Cancel   context.CancelFunc
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

	// Create log directory (absolute path next to executable)
	logDir := filepath.Join(filepath.Dir(os.Args[0]), "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		log.Printf("⚠️  Cannot create log dir %s: %v", logDir, err)
		// fallback to current directory
		logDir = "logs"
		os.MkdirAll(logDir, 0755)
	}

	// Create log file
	logFile := filepath.Join(logDir, fmt.Sprintf("task-%s-%d.log", task.ID, time.Now().Unix()))

	// Build command
	var cmd *exec.Cmd
	if task.Command != "" {
		// Use appropriate shell based on OS
		if runtime.GOOS == "windows" {
			cmd = exec.Command("cmd.exe", "/c", task.Command)
		} else {
			cmd = exec.Command("sh", "-c", task.Command)
		}
	} else {
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
	}

	if cmd == nil {
		return fmt.Errorf("failed to build command for task type: %s", task.Type)
	}

	// Set environment variables for GPU isolation
	cmd.Env = e.buildEnv(task, gpuID)

	// Redirect output to log file
	logFH, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}
	cmd.Stdout = logFH
	cmd.Stderr = logFH

	// Set up timeout context
	var taskCtx context.Context
	var taskCancel context.CancelFunc
	if task.Timeout > 0 {
		taskCtx, taskCancel = context.WithTimeout(context.Background(), time.Duration(task.Timeout)*time.Second)
	} else {
		taskCtx, taskCancel = context.WithCancel(context.Background())
	}

	// Platform-specific process group setup
	setupSysProcAttr(cmd)

	// Start the command
	if err := cmd.Start(); err != nil {
		taskCancel()
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
		Cancel:   taskCancel,
	}
	e.running[task.ID] = runningTask

	// Update task fields
	task.Status = "running"
	task.PID = cmd.Process.Pid
	task.StartTime = &now
	task.LogFile = logFile

	log.Printf("✅ Started task %s (PID: %d) on GPU %s (timeout: %ds)", task.ID, runningTask.PID, gpuID, task.Timeout)
	log.Printf("   Log: %s", logFile)

	// Monitor task completion in background
	go e.monitorTask(task.ID, cmd, taskCtx, taskCancel)

	return nil
}

// buildCommandByOS builds command with OS-appropriate shell
func (e *Executor) buildCommandByOS(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd.exe", "/c", command)
	}
	return exec.Command("sh", "-c", command)
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
func (e *Executor) monitorTask(taskID string, cmd *exec.Cmd, ctx context.Context, cancel context.CancelFunc) {
	defer cancel()

	// Wait for command to finish or context timeout
	doneCh := make(chan error, 1)
	go func() {
		doneCh <- cmd.Wait()
	}()

	var err error
	select {
	case err = <-doneCh:
		// Command finished normally
	case <-ctx.Done():
		// Timeout or cancellation - kill entire process tree
		if ctx.Err() == context.DeadlineExceeded {
			e.killProcessTree(cmd.Process.Pid)
			<-doneCh // Wait for Wait() to return after Kill
			err = fmt.Errorf("task timed out")
		} else {
			e.killProcessTree(cmd.Process.Pid)
			<-doneCh
			err = fmt.Errorf("task cancelled")
		}
	}

		e.mu.Lock()
		defer e.mu.Unlock()

		if running, exists := e.running[taskID]; exists {
			now := time.Now()
			running.Task.EndTime = &now

			if ctx.Err() == context.DeadlineExceeded {
				log.Printf("⏰ Task %s timed out", taskID)
				running.Task.Status = "timeout"
				running.Task.Error = "task exceeded timeout limit"
				if cmd.ProcessState != nil {
					running.Task.ExitCode = cmd.ProcessState.ExitCode()
				} else {
					running.Task.ExitCode = -1
				}
			} else if err != nil {
				log.Printf("❌ Task %s failed: %v", taskID, err)
				running.Task.Status = "failed"
				running.Task.Error = err.Error()
				if cmd.ProcessState != nil {
					running.Task.ExitCode = cmd.ProcessState.ExitCode()
				} else {
					running.Task.ExitCode = 1
				}
			} else {
				log.Printf("✅ Task %s completed", taskID)
				running.Task.Status = "completed"
				if cmd.ProcessState != nil {
					running.Task.ExitCode = cmd.ProcessState.ExitCode()
				} else {
					running.Task.ExitCode = 0
				}
			}
		}

		// Notify scheduler to release GPU resources
		if e.onTaskDone != nil {
			e.onTaskDone(taskID)
		}
}

// SetOnTaskDone sets a callback invoked when a task finishes (complete/fail/timeout/kill)
func (e *Executor) SetOnTaskDone(fn func(taskID string)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.onTaskDone = fn
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

	// Kill the process tree
	if err := e.killProcessTree(running.PID); err != nil {
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
		e.killProcessTree(running.PID)
		log.Printf("   Killed task %s", taskID)
	}
	e.cancel()
}

