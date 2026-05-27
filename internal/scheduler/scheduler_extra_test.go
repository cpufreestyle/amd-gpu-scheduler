package scheduler

import (
	"sync"
	"testing"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// ── GPU Type Policy Tests ──────────────────────────────────────

func TestCalculateScore_GPUTypePolicy(t *testing.T) {
	scheduler := NewScheduler()

	nvidiaGPU := &types.GPUSnapshot{
		Info: types.GPUInfo{ID: "0", Type: types.GPUTypeNVIDIA, VRAMMB: 16384},
		Usage: types.GPUUsage{UsedVRAMMB: 0, Utilization: 0, RunningTasks: 0},
	}
	amdGPU := &types.GPUSnapshot{
		Info: types.GPUInfo{ID: "1", Type: types.GPUTypeAMD, VRAMMB: 24576},
		Usage: types.GPUUsage{UsedVRAMMB: 0, Utilization: 0, RunningTasks: 0},
	}

	t.Run("gpu_type policy: training goes to NVIDIA", func(t *testing.T) {
		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 1024, TaskType: "training", Priority: 5, Policy: types.PolicyGPUType,
		}
		ns := calculateScore(nvidiaGPU, criteria, scheduler.gpuCount)
		as := calculateScore(amdGPU, criteria, scheduler.gpuCount)
		if ns <= as {
			t.Errorf("gpu_type policy: NVIDIA should win for training, got nvidia=%.2f amd=%.2f", ns, as)
		}
	})

	t.Run("gpu_type policy: inference goes to AMD", func(t *testing.T) {
		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 1024, TaskType: "inference", Priority: 5, Policy: types.PolicyGPUType,
		}
		ns := calculateScore(nvidiaGPU, criteria, scheduler.gpuCount)
		as := calculateScore(amdGPU, criteria, scheduler.gpuCount)
		if as <= ns {
			t.Errorf("gpu_type policy: AMD should win for inference, got nvidia=%.2f amd=%.2f", ns, as)
		}
	})
}

// ── VRAM Insufficient Tests ────────────────────────────────────

func TestSubmitTask_VRAMExactlyFits(t *testing.T) {
	s := NewScheduler()
	// Default NVIDIA GPU has 16384 MB
	task := &types.Task{
		Name: "exact-fit", Type: "inference", VRAMMB: 16384,
	}
	id, err := s.SubmitTask(task)
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	t.Logf("Exact-fit task %s status=%s", id, task.Status)
	if task.Status != "running" && task.Status != "pending" && task.Status != "failed" {
		t.Errorf("unexpected status %s", task.Status)
	}
}

func TestSubmitTask_VRAMExceedsAll(t *testing.T) {
	s := NewScheduler()
	task := &types.Task{
		Name: "too-big", Type: "training", VRAMMB: 999999,
	}
	id, err := s.SubmitTask(task)
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if task.Status != "pending" {
		t.Errorf("oversized task should be pending, got %s", task.Status)
	}
	t.Logf("Oversized task %s = %s", id, task.Status)
}

// ── Policy Switch Tests ────────────────────────────────────────

func TestScheduler_SetPolicy(t *testing.T) {
	s := NewScheduler()
	if s.GetDefaultPolicy() != types.PolicyBinpack {
		t.Errorf("default policy should be binpack, got %v", s.GetDefaultPolicy())
	}
	s.SetDefaultPolicy(types.PolicySpread)
	if s.GetDefaultPolicy() != types.PolicySpread {
		t.Errorf("expected spread, got %v", s.GetDefaultPolicy())
	}
	s.SetDefaultPolicy(types.PolicyGPUType)
	if s.GetDefaultPolicy() != types.PolicyGPUType {
		t.Errorf("expected gpu_type, got %v", s.GetDefaultPolicy())
	}
}

// ── Concurrent Submit Tests ────────────────────────────────────

func TestScheduler_ConcurrentSubmit(t *testing.T) {
	s := NewScheduler()
	var wg sync.WaitGroup
	errors := make(chan error, 20)

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			task := &types.Task{
				Name: "concurrent-task", Type: "inference", VRAMMB: 256, Priority: idx % 10 + 1,
			}
			_, err := s.SubmitTask(task)
			if err != nil {
				errors <- err
			}
		}(i)
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent submit error: %v", err)
	}

	tasks := s.ListTasks()
	t.Logf("Submitted %d tasks concurrently, total tracked: %d", 20, len(tasks))
}

// ── List Operations Tests ──────────────────────────────────────

func TestScheduler_ListGPUs(t *testing.T) {
	s := NewScheduler()
	gpus := s.ListGPUs()
	if len(gpus) < 2 {
		t.Errorf("expected at least 2 GPUs, got %d", len(gpus))
	}
	for _, g := range gpus {
		if g.Info.ID == "" {
			t.Error("GPU ID should not be empty")
		}
		if g.Info.VRAMMB <= 0 {
			t.Errorf("GPU %s has invalid VRAM: %d", g.Info.ID, g.Info.VRAMMB)
		}
	}
}

func TestScheduler_GetGPU(t *testing.T) {
	s := NewScheduler()
	gpu, ok := s.GetGPU("0")
	if !ok {
		t.Fatal("GetGPU(0) not found")
	}
	if gpu.Info.Type != types.GPUTypeNVIDIA {
		t.Errorf("GPU 0 should be NVIDIA, got %s", gpu.Info.Type)
	}

	gpu1, ok := s.GetGPU("1")
	if !ok {
		t.Fatal("GetGPU(1) not found")
	}
	if gpu1.Info.Type != types.GPUTypeAMD {
		t.Errorf("GPU 1 should be AMD, got %s", gpu1.Info.Type)
	}
}

func TestScheduler_GetGPU_NotFound(t *testing.T) {
	s := NewScheduler()
	_, ok := s.GetGPU("99")
	if ok {
		t.Error("expected not found for non-existent GPU")
	}
}

// ── Task Status Transition Tests ───────────────────────────────

func TestSubmitTask_TaskIDIncrement(t *testing.T) {
	s := NewScheduler()
	ids := make(map[string]bool)
	for i := 0; i < 5; i++ {
		task := &types.Task{Name: "id-test", Type: "inference", VRAMMB: 256}
		id, err := s.SubmitTask(task)
		if err != nil {
			t.Fatalf("SubmitTask %d: %v", i, err)
		}
		if ids[id] {
			t.Errorf("duplicate task ID: %s", id)
		}
		ids[id] = true
	}
	if len(ids) != 5 {
		t.Errorf("expected 5 unique IDs, got %d", len(ids))
	}
}

// ── Preemption Integration Tests ───────────────────────────────

func TestPreemptConfig_Defaults(t *testing.T) {
	cfg := DefaultPreemptConfig()
	if !cfg.Enabled {
		t.Error("preemption should be enabled by default")
	}
	if cfg.MinPriorityToPreempt <= 0 {
		t.Error("MinPriorityToPreempt should be positive")
	}
}

// ── FreeVRAM Helper Test ──────────────────────────────────────

func TestGPUSnapshot_FreeVRAM(t *testing.T) {
	gpu := &types.GPUSnapshot{
		Info:  types.GPUInfo{ID: "0", VRAMMB: 16384},
		Usage: types.GPUUsage{UsedVRAMMB: 4096},
	}
	free := gpu.FreeVRAM()
	if free != 12288 {
		t.Errorf("expected 12288 MB free, got %d", free)
	}
}
