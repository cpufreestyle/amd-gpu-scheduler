package scheduler

import (
	"testing"
	"time"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// ── Scheduler Benchmark ──────────────────────────────────────

func BenchmarkScheduler_SubmitTask(b *testing.B) {
	sched := NewScheduler()
	for i := 2; i < 8; i++ {
		sched.RegisterGPU(&types.GPUSnapshot{
			Info:  types.GPUInfo{ID: string(rune('0' + i)), Type: types.GPUTypeNVIDIA, VRAMMB: 16384},
			Usage: types.GPUUsage{UsedVRAMMB: 0, RunningTasks: 0, Utilization: 0, LastUpdated: time.Now()},
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := &types.Task{
			Name: "bench-task", Type: "inference", Priority: 5, VRAMMB: 1024, Command: "echo ok",
		}
		_, _ = sched.SubmitTask(task)
	}
}

func BenchmarkScheduler_CalculateScore(b *testing.B) {
	sched := NewScheduler()
	gpu := &types.GPUSnapshot{
		Info:  types.GPUInfo{ID: "0", Type: types.GPUTypeNVIDIA, VRAMMB: 16384},
		Usage: types.GPUUsage{UsedVRAMMB: 4096, RunningTasks: 2, Utilization: 45, LastUpdated: time.Now()},
	}
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: 2048, TaskType: "training", Priority: 7, Policy: types.PolicyBinpack,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateScore(gpu, criteria, len(sched.ListGPUs()))
	}
}

func BenchmarkScheduler_SubmitConcurrent(b *testing.B) {
	sched := NewScheduler()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			task := &types.Task{
				Name: "bench-parallel", Type: "training", Priority: i%10 + 1, VRAMMB: 512, Command: "echo ok",
			}
			_, _ = sched.SubmitTask(task)
			i++
		}
	})
}

func BenchmarkScheduler_ListTasks(b *testing.B) {
	sched := NewScheduler()
	for i := 0; i < 100; i++ {
		sched.SubmitTask(&types.Task{
			Name: "prepop", Type: "inference", VRAMMB: 256, Command: "echo ok",
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sched.ListTasks()
	}
}

func BenchmarkScheduler_8GPUs_Submit(b *testing.B) {
	sched := NewScheduler()
	for i := 2; i <= 8; i++ {
		t := types.GPUTypeNVIDIA
		if i%2 == 0 {
			t = types.GPUTypeAMD
		}
		sched.RegisterGPU(&types.GPUSnapshot{
			Info:  types.GPUInfo{ID: string(rune('0' + i)), Type: t, VRAMMB: 16384},
			Usage: types.GPUUsage{UsedVRAMMB: 0, RunningTasks: 0, Utilization: 0, LastUpdated: time.Now()},
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		taskType := "training"
		if i%2 == 0 {
			taskType = "inference"
		}
		task := &types.Task{
			Name: "bench-8gpu", Type: taskType, Priority: 5, VRAMMB: 2048, Command: "echo ok",
		}
		_, _ = sched.SubmitTask(task)
	}
}

// ── CalculateScore Policy Variants ────────────────────────────

func BenchmarkCalculateScore_Binpack(b *testing.B) {
	gpu := &types.GPUSnapshot{
		Info:  types.GPUInfo{ID: "0", Type: types.GPUTypeNVIDIA, VRAMMB: 16384},
		Usage: types.GPUUsage{UsedVRAMMB: 2048, RunningTasks: 1, Utilization: 30, LastUpdated: time.Now()},
	}
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: 1024, TaskType: "training", Priority: 5, Policy: types.PolicyBinpack,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateScore(gpu, criteria, 2)
	}
}

func BenchmarkCalculateScore_Spread(b *testing.B) {
	gpu := &types.GPUSnapshot{
		Info:  types.GPUInfo{ID: "0", Type: types.GPUTypeNVIDIA, VRAMMB: 16384},
		Usage: types.GPUUsage{UsedVRAMMB: 2048, RunningTasks: 3, Utilization: 60, LastUpdated: time.Now()},
	}
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: 1024, TaskType: "inference", Priority: 5, Policy: types.PolicySpread,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = calculateScore(gpu, criteria, 2)
	}
}

func BenchmarkCalculateScore_GPUType(b *testing.B) {
	gpus := []*types.GPUSnapshot{
		{Info: types.GPUInfo{ID: "0", Type: types.GPUTypeNVIDIA, VRAMMB: 16384}, Usage: types.GPUUsage{UsedVRAMMB: 0, RunningTasks: 0, Utilization: 0, LastUpdated: time.Now()}},
		{Info: types.GPUInfo{ID: "1", Type: types.GPUTypeAMD, VRAMMB: 24576}, Usage: types.GPUUsage{UsedVRAMMB: 0, RunningTasks: 0, Utilization: 0, LastUpdated: time.Now()}},
	}
	criteria := &types.SchedulingCriteria{
		VRAMRequiredMB: 1024, TaskType: "training", Priority: 5, Policy: types.PolicyGPUType,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, gpu := range gpus {
			_ = calculateScore(gpu, criteria, 2)
		}
	}
}
