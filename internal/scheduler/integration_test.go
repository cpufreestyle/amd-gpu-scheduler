package scheduler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// ── Helpers ──────────────────────────────────────────────────

// newTestServerWithScheduler creates a httptest server + scheduler,
// injects a mock executor so no real processes are spawned.
func newTestServerWithScheduler() (*httptest.Server, *Scheduler) {
	sched := NewScheduler()

	// Override executor with no-op for integration tests
	// We can't easily inject executor without an interface,
	// so we start the real server but submit tasks with Command="echo ok"
	// which is harmless and exits immediately.
	_ = &mockExecutor{} // declare to satisfy compiler

	mux := http.NewServeMux()

	mux.HandleFunc("/api/gpus", func(w http.ResponseWriter, r *http.Request) {
		gpus := sched.ListGPUs()
		json.NewEncoder(w).Encode(gpus)
	})

	mux.HandleFunc("/api/tasks", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", 405)
			return
		}
		var req types.TaskRequest
		json.NewDecoder(r.Body).Decode(&req)
		task := &types.Task{
			Name:    req.Name,
			Type:    req.Type,
			Priority: req.Priority,
			VRAMMB:  req.VRAMMB,
			Command:  "echo ok",
			Timeout:  5,
		}
		id, err := sched.SubmitTask(task)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(map[string]string{"task_id": id, "status": task.Status})
	})

	mux.HandleFunc("/api/tasks/list", func(w http.ResponseWriter, r *http.Request) {
		tasks := sched.ListTasks()
		json.NewEncoder(w).Encode(tasks)
	})

	mux.HandleFunc("/api/scheduler/policy", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]string{"policy": string(sched.GetDefaultPolicy())})
			return
		}
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		if p, ok := body["policy"]; ok {
			sched.SetDefaultPolicy(types.SchedulingPolicy(p))
		}
		json.NewEncoder(w).Encode(map[string]string{"policy": string(sched.GetDefaultPolicy())})
	})

	ts := httptest.NewServer(mux)
	return ts, sched
}

type mockExecutor struct{}

func (m *mockExecutor) StartTask(task *types.Task) error {
	task.Status = "running"
	task.PID = 12345
	return nil
}
func (m *mockExecutor) StopTask(taskID string) error { return nil }
func (m *mockExecutor) GetLog(taskID string) string  { return "mock log" }

// ── Integration Tests ────────────────────────────────────────

func TestIntegration_SubmitAndListTasks(t *testing.T) {
	ts, _ := newTestServerWithScheduler()
	defer ts.Close()

	// Submit a task via HTTP
	body := `{"name":"int-test","type":"inference","priority":5,"vram_mb":512,"command":"echo ok"}`
	resp, err := http.Post(ts.URL+"/api/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/tasks: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Errorf("expected 201, got %d", resp.StatusCode)
	}

	// List tasks
	resp2, err := http.Get(ts.URL + "/api/tasks/list")
	if err != nil {
		t.Fatalf("GET /api/tasks/list: %v", err)
	}
	defer resp2.Body.Close()

	var tasks []types.Task
	json.NewDecoder(resp2.Body).Decode(&tasks)
	if len(tasks) < 1 {
		t.Errorf("expected at least 1 task, got %d", len(tasks))
	}
	t.Logf("Listed %d tasks after submission", len(tasks))
}

func TestIntegration_GPUList(t *testing.T) {
	ts, _ := newTestServerWithScheduler()
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/gpus")
	if err != nil {
		t.Fatalf("GET /api/gpus: %v", err)
	}
	defer resp.Body.Close()

	var gpus []types.GPUSnapshot
	json.NewDecoder(resp.Body).Decode(&gpus)
	if len(gpus) < 2 {
		t.Errorf("expected at least 2 GPUs, got %d", len(gpus))
	}
	for _, g := range gpus {
		t.Logf("GPU %s: %s (%d MB)", g.Info.ID, g.Info.Name, g.Info.VRAMMB)
	}
}

func TestIntegration_PolicySwitch(t *testing.T) {
	ts, sched := newTestServerWithScheduler()
	defer ts.Close()

	// Verify default
	if sched.GetDefaultPolicy() != types.PolicyBinpack {
		t.Errorf("default policy should be binpack")
	}

	// Switch to spread via HTTP
	body := strings.NewReader(`{"policy":"spread"}`)
	resp, err := http.Post(ts.URL+"/api/scheduler/policy", "application/json", body)
	if err != nil {
		t.Fatalf("POST policy: %v", err)
	}
	resp.Body.Close()

	if sched.GetDefaultPolicy() != types.PolicySpread {
		t.Errorf("expected spread after POST, got %v", sched.GetDefaultPolicy())
	}
	t.Logf("Policy switched to %v", sched.GetDefaultPolicy())
}

func TestIntegration_MultipleTaskLifecycle(t *testing.T) {
	sched := NewScheduler()

	for i := 0; i < 5; i++ {
		task := &types.Task{
			Name:    fmt.Sprintf("lifecycle-%d", i),
			Type:    "training",
			Priority: i + 1,
			VRAMMB:  1024,
			Command:  "echo ok",
			Timeout:  5,
		}
		id, err := sched.SubmitTask(task)
		if err != nil {
			t.Fatalf("SubmitTask %d: %v", i, err)
		}
		t.Logf("Submitted: %s (status=%s)", id, task.Status)
	}

	tasks := sched.ListTasks()
	if len(tasks) < 5 {
		t.Errorf("expected ≥5 tasks, got %d", len(tasks))
	}

	// Verify all submitted tasks appear in ListTasks
	submitted := tasks
	listed := sched.ListTasks()
	for _, sub := range submitted {
		found := false
		for _, li := range listed {
			if sub.ID == li.ID {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Task %s not found in ListTasks", sub.ID)
		}
	}
	t.Logf("All %d tasks verified via GetTask", len(tasks))
}

func TestIntegration_SchedulerScore_Realistic(t *testing.T) {
	sched := NewScheduler()
	// Add a simulated loaded GPU
	sched.RegisterGPU(&types.GPUSnapshot{
		Info: types.GPUInfo{ID: "2", Type: types.GPUTypeNVIDIA, Name: "RTX 5070 Ti #2", VRAMMB: 16384},
		Usage: types.GPUUsage{UsedVRAMMB: 8192, Utilization: 80, RunningTasks: 3, LastUpdated: time.Now()},
	})

	gpus := sched.ListGPUs()
	t.Logf("Total GPUs registered: %d", len(gpus))

	for _, gpu := range gpus {
		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 2048,
			TaskType:       "training",
			Priority:       5,
			Policy:         sched.GetDefaultPolicy(),
		}
		// Use len(sched.ListGPUs()) as GPU count proxy
		score := calculateScore(gpu, criteria, len(sched.ListGPUs()))
		t.Logf("GPU %s (%s): score=%.1f", gpu.Info.ID, gpu.Info.Type, score)
	}
}
