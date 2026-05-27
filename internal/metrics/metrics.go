package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// GPU metrics
	GPUVRAMUsed = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Name:      "gpu_vram_used_mb",
			Help:      "Used VRAM per GPU in MB",
		},
		[]string{"gpu_id", "gpu_type", "gpu_name"},
	)

	GPUVRAMTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Name:      "gpu_vram_total_mb",
			Help:      "Total VRAM per GPU in MB",
		},
		[]string{"gpu_id", "gpu_type", "gpu_name"},
	)

	GPUUtilization = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Name:      "gpu_utilization_percent",
			Help:      "GPU utilization percentage",
		},
		[]string{"gpu_id", "gpu_type", "gpu_name"},
	)

	GPURunningTasks = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Name:      "gpu_running_tasks",
			Help:      "Number of running tasks per GPU",
		},
		[]string{"gpu_id", "gpu_type", "gpu_name"},
	)

	GPUScore = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Name:      "gpu_score",
			Help:      "Scheduling score per GPU",
		},
		[]string{"gpu_id", "gpu_type", "gpu_name"},
	)

	// Task metrics
	TasksSubmitted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scheduler",
			Name:      "tasks_submitted_total",
			Help:      "Total number of tasks submitted",
		},
		[]string{"type", "priority"},
	)

	TasksRunning = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Name:      "tasks_running",
			Help:      "Number of currently running tasks",
		},
	)

	TasksCompleted = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scheduler",
			Name:      "tasks_completed_total",
			Help:      "Total number of completed tasks",
		},
		[]string{"status"}, // completed, failed, timeout
	)

	TasksRunningDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "scheduler",
			Name:      "task_duration_seconds",
			Help:      "Task execution duration in seconds",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10), // 0.1s to ~102s
		},
		[]string{"type"},
	)

	// Scheduler metrics
	SchedulingDecisions = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scheduler",
			Name:      "scheduling_decisions_total",
			Help:      "Total scheduling decisions",
		},
		[]string{"policy", "result"}, // result: success, failed, no_gpu
	)

	SchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Namespace: "scheduler",
			Name:      "scheduling_latency_seconds",
			Help:      "Time to make a scheduling decision",
			Buckets:   prometheus.ExponentialBuckets(0.0001, 2, 12), // 0.1ms to ~409ms
		},
	)

	// Executor metrics
	ExecutorTasksActive = promauto.NewGauge(
		prometheus.GaugeOpts{
			Namespace: "scheduler",
			Subsystem: "executor",
			Name:      "active_tasks",
			Help:      "Number of tasks currently being executed",
		},
	)

	ExecutorTaskDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "scheduler",
			Subsystem: "executor",
			Name:      "task_duration_seconds",
			Help:      "Actual executor task duration",
			Buckets:   prometheus.ExponentialBuckets(0.1, 2, 10),
		},
		[]string{"status"},
	)

	// HTTP metrics
	HTTPRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "scheduler",
			Subsystem: "http",
			Name:      "requests_total",
			Help:      "Total HTTP requests",
		},
		[]string{"method", "endpoint", "status"},
	)

	HTTPRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "scheduler",
			Subsystem: "http",
			Name:      "request_duration_seconds",
			Help:      "HTTP request duration",
			Buckets:   prometheus.ExponentialBuckets(0.001, 2, 12),
		},
		[]string{"method", "endpoint"},
	)
)

// RecordGPUUpdate records GPU state metrics
func RecordGPUUpdate(id, gpuType, name string, usedVRAM, totalVRAM int, utilization float64, runningTasks int) {
	GPUVRAMUsed.WithLabelValues(id, gpuType, name).Set(float64(usedVRAM))
	GPUVRAMTotal.WithLabelValues(id, gpuType, name).Set(float64(totalVRAM))
	GPUUtilization.WithLabelValues(id, gpuType, name).Set(utilization)
	GPURunningTasks.WithLabelValues(id, gpuType, name).Set(float64(runningTasks))
}

// RecordTaskSubmit records a task submission
func RecordTaskSubmit(taskType string, priority int) {
	p := "normal"
	if priority > 5 {
		p = "high"
	} else if priority <= 0 {
		p = "low"
	}
	TasksSubmitted.WithLabelValues(taskType, p).Inc()
}

// RecordTaskComplete records task completion
func RecordTaskComplete(status string, durationSeconds float64, taskType string) {
	TasksCompleted.WithLabelValues(status).Inc()
	TasksRunningDuration.WithLabelValues(taskType).Observe(durationSeconds)
}

// RecordScheduling records scheduling decision
func RecordScheduling(policy, result string, latencySeconds float64) {
	SchedulingDecisions.WithLabelValues(policy, result).Inc()
	SchedulingLatency.Observe(latencySeconds)
}

// RecordExecutorTask records executor task events
func RecordExecutorTaskStart() {
	ExecutorTasksActive.Inc()
	TasksRunning.Inc()
}

func RecordExecutorTaskEnd(status string, durationSeconds float64) {
	ExecutorTasksActive.Dec()
	TasksRunning.Dec()
	ExecutorTaskDuration.WithLabelValues(status).Observe(durationSeconds)
}
