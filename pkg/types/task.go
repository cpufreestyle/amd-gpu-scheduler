package types

import "time"

// Task represents a GPU compute task
type Task struct {
	ID        string            // Task ID
	Name      string            // Task name
	Type      string            // "training", "inference", "compute"
	Priority  int               // 1-10, higher is more urgent
	GPUReq    int               // Number of GPUs required
	VRAMMB    int               // VRAM required in MB
	Status    string            // "pending", "running", "completed", "failed", "killed"
	
	// Execution fields
	Command   string            // Command to execute (e.g., "python train.py")
	Args      []string          // Command arguments
	Env       map[string]string // Environment variables
	WorkDir   string            // Working directory
	Timeout   int               // Timeout in seconds (0 = no timeout)
	
	// Runtime fields
	PID       int               // Process ID
	StartTime *time.Time        // Task start time
	EndTime   *time.Time        // Task end time
	LogFile   string            // Log file path
	ExitCode  int               // Process exit code
	Error     string            // Error message if failed
}

// TaskRequest for API
type TaskRequest struct {
	Name     string            `json:"name"`
	Type     string            `json:"type"`
	Priority int               `json:"priority"`
	GPUReq   int               `json:"gpu_req"`
	VRAMMB   int               `json:"vram_mb"`
	Policy   string            `json:"policy"`
	Command  string            `json:"command"`
	Args     []string          `json:"args"`
	Env      map[string]string `json:"env"`
	WorkDir  string            `json:"work_dir"`
	Timeout  int               `json:"timeout"`
}

// TaskResponse from API
type TaskResponse struct {
	Success bool   `json:"success"`
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
	Task    *Task  `json:"task,omitempty"`
}
