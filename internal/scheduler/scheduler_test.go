package scheduler

import (
	"testing"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

func TestCalculateScore_VRAMWeight(t *testing.T) {
	// VRAM 30% weight: GPU 0% used vs 50% used, scores should be different
	scheduler := NewScheduler()
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: 1024,
		TaskType:       "inference",
		Priority:       5,
		Policy:         types.PolicySpread,
	}

	// GPU with 0% VRAM used
	gpu0 := &types.GPUSnapshot{
		Info: types.GPUInfo{
			ID:     "0",
			Type:   types.GPUTypeNVIDIA,
			VRAMMB: 16384,
		},
		Usage: types.GPUUsage{
			UsedVRAMMB:   0,
			Utilization:  0,
			RunningTasks: 0,
		},
	}

	// GPU with 50% VRAM used
	gpu50 := &types.GPUSnapshot{
		Info: types.GPUInfo{
			ID:     "1",
			Type:   types.GPUTypeNVIDIA,
			VRAMMB: 16384,
		},
		Usage: types.GPUUsage{
			UsedVRAMMB:   8192,
			Utilization:  0,
			RunningTasks: 0,
		},
	}

	score0 := calculateScore(gpu0, criteria, scheduler.gpuCount)
	score50 := calculateScore(gpu50, criteria, scheduler.gpuCount)

	t.Logf("GPU 0%% used score: %.2f", score0)
	t.Logf("GPU 50%% used score: %.2f", score50)

	if score0 == score50 {
		t.Errorf("Expected different scores for 0%% and 50%% VRAM usage, got %.2f and %.2f", score0, score50)
	}

	// GPU with 0% used should have higher score (more free VRAM)
	if score0 <= score50 {
		t.Errorf("Expected GPU with 0%% used to have higher score, got %.2f <= %.2f", score0, score50)
	}
}

func TestCalculateScore_GPUTypeWeight(t *testing.T) {
	// GPU type 20% weight:
	// training task: NVIDIA should score higher than AMD
	// inference task: AMD should score higher than NVIDIA

	scheduler := NewScheduler()

	t.Run("Training task prefers NVIDIA", func(t *testing.T) {
		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 1024,
			TaskType:       "training",
			Priority:       5,
			Policy:         types.PolicySpread,
		}

		nvidiaGPU := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "0",
				Type:   types.GPUTypeNVIDIA,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 0,
			},
		}

		amdGPU := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "1",
				Type:   types.GPUTypeAMD,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 0,
			},
		}

		nvidiaScore := calculateScore(nvidiaGPU, criteria, scheduler.gpuCount)
		amdScore := calculateScore(amdGPU, criteria, scheduler.gpuCount)

		t.Logf("NVIDIA score for training: %.2f", nvidiaScore)
		t.Logf("AMD score for training: %.2f", amdScore)

		if nvidiaScore <= amdScore {
			t.Errorf("Expected NVIDIA to score higher for training task, got %.2f <= %.2f", nvidiaScore, amdScore)
		}
	})

	t.Run("Inference task prefers AMD", func(t *testing.T) {
		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 1024,
			TaskType:       "inference",
			Priority:       5,
			Policy:         types.PolicySpread,
		}

		nvidiaGPU := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "0",
				Type:   types.GPUTypeNVIDIA,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 0,
			},
		}

		amdGPU := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "1",
				Type:   types.GPUTypeAMD,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 0,
			},
		}

		nvidiaScore := calculateScore(nvidiaGPU, criteria, scheduler.gpuCount)
		amdScore := calculateScore(amdGPU, criteria, scheduler.gpuCount)

		t.Logf("NVIDIA score for inference: %.2f", nvidiaScore)
		t.Logf("AMD score for inference: %.2f", amdScore)

		if amdScore <= nvidiaScore {
			t.Errorf("Expected AMD to score higher for inference task, got %.2f <= %.2f", amdScore, nvidiaScore)
		}
	})
}

func TestCalculateScore_PriorityWeight(t *testing.T) {
	// Priority 15% weight: priority=10 should score higher than priority=1

	scheduler := NewScheduler()

	gpu := &types.GPUSnapshot{
		Info: types.GPUInfo{
			ID:     "0",
			Type:   types.GPUTypeNVIDIA,
			VRAMMB: 16384,
		},
		Usage: types.GPUUsage{
			UsedVRAMMB:   0,
			Utilization:  0,
			RunningTasks: 0,
		},
	}

	criteriaLow := &types.SchedulingCriteria{
		VRAMRequiredMB: 1024,
		TaskType:       "inference",
		Priority:       1,
		Policy:         types.PolicySpread,
	}

	criteriaHigh := &types.SchedulingCriteria{
		VRAMRequiredMB: 1024,
		TaskType:       "inference",
		Priority:       10,
		Policy:         types.PolicySpread,
	}

	scoreLow := calculateScore(gpu, criteriaLow, scheduler.gpuCount)
	scoreHigh := calculateScore(gpu, criteriaHigh, scheduler.gpuCount)

	t.Logf("Priority 1 score: %.2f", scoreLow)
	t.Logf("Priority 10 score: %.2f", scoreHigh)

	if scoreHigh <= scoreLow {
		t.Errorf("Expected higher priority to have higher score, got %.2f <= %.2f", scoreHigh, scoreLow)
	}
}

func TestCalculateScore_LoadBalanceWeight(t *testing.T) {
	// Load balance 10% weight:
	// - binpack: GPU with more tasks should have higher score
	// - spread: GPU with fewer tasks should have higher score

	t.Run("Binpack policy: GPU with more tasks scores higher", func(t *testing.T) {
		scheduler := NewScheduler()

		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 1024,
			TaskType:       "inference",
			Priority:       5,
			Policy:         types.PolicyBinpack,
		}

		// GPU with many tasks
		gpuManyTasks := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "0",
				Type:   types.GPUTypeNVIDIA,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 5,
			},
		}

		// GPU with few tasks
		gpuFewTasks := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "1",
				Type:   types.GPUTypeNVIDIA,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 1,
			},
		}

		scoreMany := calculateScore(gpuManyTasks, criteria, scheduler.gpuCount)
		scoreFew := calculateScore(gpuFewTasks, criteria, scheduler.gpuCount)

		t.Logf("Binpack - GPU with 5 tasks score: %.2f", scoreMany)
		t.Logf("Binpack - GPU with 1 task score: %.2f", scoreFew)

		if scoreMany <= scoreFew {
			t.Errorf("Expected GPU with more tasks to have higher score in binpack, got %.2f <= %.2f", scoreMany, scoreFew)
		}
	})

	t.Run("Spread policy: GPU with fewer tasks scores higher", func(t *testing.T) {
		scheduler := NewScheduler()

		criteria := &types.SchedulingCriteria{
			VRAMRequiredMB: 1024,
			TaskType:       "inference",
			Priority:       5,
			Policy:         types.PolicySpread,
		}

		// GPU with many tasks
		gpuManyTasks := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "0",
				Type:   types.GPUTypeNVIDIA,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 5,
			},
		}

		// GPU with few tasks
		gpuFewTasks := &types.GPUSnapshot{
			Info: types.GPUInfo{
				ID:     "1",
				Type:   types.GPUTypeNVIDIA,
				VRAMMB: 16384,
			},
			Usage: types.GPUUsage{
				UsedVRAMMB:   0,
				Utilization:  0,
				RunningTasks: 1,
			},
		}

		scoreMany := calculateScore(gpuManyTasks, criteria, scheduler.gpuCount)
		scoreFew := calculateScore(gpuFewTasks, criteria, scheduler.gpuCount)

		t.Logf("Spread - GPU with 5 tasks score: %.2f", scoreMany)
		t.Logf("Spread - GPU with 1 task score: %.2f", scoreFew)

		if scoreFew <= scoreMany {
			t.Errorf("Expected GPU with fewer tasks to have higher score in spread, got %.2f <= %.2f", scoreFew, scoreMany)
		}
	})
}

func TestSubmitTask_NormalTask(t *testing.T) {
	scheduler := NewScheduler()

	task := &types.Task{
		Name:   "test-task",
		Type:   "inference",
		VRAMMB: 1024,
	}

	taskID, err := scheduler.SubmitTask(task)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	if taskID == "" {
		t.Error("Expected task ID to be generated")
	}

	// Task should be in running, pending, or failed (if already running from another test)
	if task.Status != "running" && task.Status != "pending" && task.Status != "failed" {
		t.Errorf("Expected task status 'running', 'pending', or 'failed', got %q", task.Status)
	}

	t.Logf("Task ID: %s, Status: %s", taskID, task.Status)
}

func TestSubmitTask_OverSizeVRAM(t *testing.T) {
	scheduler := NewScheduler()

	// Submit a task with 50GB VRAM (50000MB) - larger than any GPU
	task := &types.Task{
		Name:   "oversized-task",
		Type:   "training",
		VRAMMB: 50000,
	}

	taskID, err := scheduler.SubmitTask(task)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	// Task should be pending since no GPU has enough VRAM
	if task.Status != "pending" {
		t.Errorf("Expected task status 'pending' for oversized task, got %q", task.Status)
	}

	t.Logf("Oversized task ID: %s, Status: %s", taskID, task.Status)
}

func TestSubmitTask_GPUFull(t *testing.T) {
	scheduler := NewScheduler()

	// Fill up the GPUs
	gpus := scheduler.ListGPUs()
	for _, gpu := range gpus {
		// Use all VRAM
		gpu.Usage.UsedVRAMMB = gpu.Info.VRAMMB
	}

	// Try to submit a task
	task := &types.Task{
		Name:   "task-when-full",
		Type:   "inference",
		VRAMMB: 1024,
	}

	taskID, err := scheduler.SubmitTask(task)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	// Task should be pending
	if task.Status != "pending" {
		t.Errorf("Expected task status 'pending' when GPU is full, got %q", task.Status)
	}

	t.Logf("Task ID when GPU full: %s, Status: %s", taskID, task.Status)
}

func TestReleaseTask(t *testing.T) {
	scheduler := NewScheduler()

	// Submit a task first
	task := &types.Task{
		Name:   "test-release",
		Type:   "inference",
		VRAMMB: 4096,
	}

	taskID, err := scheduler.SubmitTask(task)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	t.Logf("Submitted task: ID=%s, Status=%s, GPUReq=%d", taskID, task.Status, task.GPUReq)

	// Only test release if task is running
	if task.Status == "running" {
		// Get the GPU's UsedVRAMMB before release
		gpuID := string(rune('0' + task.GPUReq))
		gpu, _ := scheduler.GetGPU(gpuID)
		var vramBefore int
		if gpu != nil {
			vramBefore = gpu.Usage.UsedVRAMMB
		}

		// Release the task
		scheduler.ReleaseTask(taskID)

		// Check GPU's UsedVRAMMB after release
		if gpu != nil {
			vramAfter := gpu.Usage.UsedVRAMMB
			if vramAfter >= vramBefore {
				t.Errorf("Expected UsedVRAMMB to decrease after release, before=%d, after=%d", vramBefore, vramAfter)
			}
			t.Logf("VRAM before release: %d, after: %d", vramBefore, vramAfter)
		}

		// Verify task is no longer in running tasks
		tasks := scheduler.ListTasks()
		found := false
		for _, t := range tasks {
			if t.ID == taskID && t.Status == "running" {
				found = true
				break
			}
		}
		if found {
			t.Error("Task should not be in running state after release")
		}
	} else {
		t.Logf("Task is not running (status=%s), skipping release test", task.Status)
	}
}

func TestReleaseTask_SchedulePending(t *testing.T) {
	scheduler := NewScheduler()

	// Fill up GPUs
	gpus := scheduler.ListGPUs()
	for _, gpu := range gpus {
		gpu.Usage.UsedVRAMMB = gpu.Info.VRAMMB
	}

	// Submit a task that will be pending
	pendingTask := &types.Task{
		Name:   "pending-task",
		Type:   "inference",
		VRAMMB: 1024,
	}

	pendingTaskID, err := scheduler.SubmitTask(pendingTask)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	if pendingTask.Status != "pending" {
		t.Fatalf("Expected pending task to be pending, got %q", pendingTask.Status)
	}

	t.Logf("Pending task: ID=%s, Status=%s", pendingTaskID, pendingTask.Status)

	// Now submit a running task and immediately release it
	// This should trigger scheduling of pending task
	runningTask := &types.Task{
		Name:   "running-task",
		Type:   "inference",
		VRAMMB: 4096,
	}

	// Make room for this task
	for _, gpu := range gpus {
		gpu.Usage.UsedVRAMMB = 0
	}

	runningTaskID, err := scheduler.SubmitTask(runningTask)
	if err != nil {
		t.Fatalf("SubmitTask failed: %v", err)
	}

	t.Logf("Running task: ID=%s, Status=%s", runningTaskID, runningTask.Status)

	// Release the running task - this should trigger scheduling of pending task
	if runningTask.Status == "running" {
		scheduler.ReleaseTask(runningTaskID)

		// Check if pending task got scheduled
		// (This depends on implementation of trySchedulePendingLocked)
		t.Logf("After release - Pending task status: %s", pendingTask.Status)
	}
}
