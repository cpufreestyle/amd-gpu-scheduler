package types

import (
	"testing"
)

func TestTask_Defaults(t *testing.T) {
	task := &Task{}

	// In Go, zero values are used for struct fields
	// Priority int defaults to 0
	// Type string defaults to ""
	// The test should verify the zero value behavior

	t.Run("Priority defaults to 0 (zero value)", func(t *testing.T) {
		if task.Priority != 0 {
			t.Errorf("Priority = %d, expected 0 (zero value)", task.Priority)
		}
	})

	t.Run("Type defaults to empty string (zero value)", func(t *testing.T) {
		if task.Type != "" {
			t.Errorf("Type = %q, expected empty string (zero value)", task.Type)
		}
	})

	t.Run("Status defaults to empty string", func(t *testing.T) {
		if task.Status != "" {
			t.Errorf("Status = %q, expected empty string", task.Status)
		}
	})

	t.Run("GPUReq defaults to 0", func(t *testing.T) {
		if task.GPUReq != 0 {
			t.Errorf("GPUReq = %d, expected 0", task.GPUReq)
		}
	})

	t.Run("VRAMMB defaults to 0", func(t *testing.T) {
		if task.VRAMMB != 0 {
			t.Errorf("VRAMMB = %d, expected 0", task.VRAMMB)
		}
	})
}

func TestTask_WithValues(t *testing.T) {
	task := &Task{
		ID:       "task-1",
		Name:     "test-task",
		Type:     "training",
		Priority: 8,
		GPUReq:   1,
		VRAMMB:  4096,
		Status:   "pending",
	}

	t.Run("Type is training", func(t *testing.T) {
		if task.Type != "training" {
			t.Errorf("Type = %q, expected 'training'", task.Type)
		}
	})

	t.Run("Priority is 8", func(t *testing.T) {
		if task.Priority != 8 {
			t.Errorf("Priority = %d, expected 8", task.Priority)
		}
	})

	t.Run("VRAMMB is 4096", func(t *testing.T) {
		if task.VRAMMB != 4096 {
			t.Errorf("VRAMMB = %d, expected 4096", task.VRAMMB)
		}
	})

	t.Run("Status is pending", func(t *testing.T) {
		if task.Status != "pending" {
			t.Errorf("Status = %q, expected 'pending'", task.Status)
		}
	})
}

func TestTaskRequest_Parse(t *testing.T) {
	jsonData := []byte(`{
		"name": "test-task",
		"type": "inference",
		"priority": 5,
		"gpu_req": 1,
		"vram_mb": 2048,
		"command": "python infer.py"
	}`)

	req, err := ParseTaskRequest(jsonData)
	if err != nil {
		t.Fatalf("ParseTaskRequest failed: %v", err)
	}

	t.Run("Name is test-task", func(t *testing.T) {
		if req.Name != "test-task" {
			t.Errorf("Name = %q, expected 'test-task'", req.Name)
		}
	})

	t.Run("Type is inference", func(t *testing.T) {
		if req.Type != "inference" {
			t.Errorf("Type = %q, expected 'inference'", req.Type)
		}
	})

	t.Run("Priority is 5", func(t *testing.T) {
		if req.Priority != 5 {
			t.Errorf("Priority = %d, expected 5", req.Priority)
		}
	})
}
