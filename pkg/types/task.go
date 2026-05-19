package types

// Task represents a GPU compute task
type Task struct {
	ID       string // Task ID
	Name     string // Task name
	Type     string // "training", "inference", "compute"
	Priority int    // 1-10, higher is more urgent
	GPUReq   int    // Number of GPUs required
	Status   string // "pending", "running", "done", "failed"
}

// TaskRequest for API
type TaskRequest struct {
	Task *Task
}

// TaskResponse from API
type TaskResponse struct {
	Success bool
	TaskID  string
	Message string
}
