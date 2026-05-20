package main

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/amd-gpu-scheduler/internal/gpumonitor"
	"github.com/amd-gpu-scheduler/internal/metrics"
	"github.com/amd-gpu-scheduler/internal/scheduler"
	"github.com/amd-gpu-scheduler/pkg/types"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	log.Println("AMD GPU Scheduler starting...")
	log.Println("Hybrid NVIDIA + AMD GPU Scheduler v2.0")

	sched := scheduler.NewScheduler()

	// GPU 监控（通过 nvidia-smi / rocm-smi 读取真实 GPU 数据）
	gpuMon := gpumonitor.NewGPUMonitor(func(gpuID string, update gpumonitor.GPUMonitorUpdate) {
		usage := types.GPUUsage{
			UsedVRAMMB:   update.UsedVRAMMB,
			Utilization:  update.UtilizationPct,
			TemperatureC: update.TemperatureC,
			PowerDrawW:  update.PowerDrawW,
		}
		sched.UpdateGPUUsage(gpuID, usage)
	})
	go gpuMon.Start(5 * time.Second)
	defer gpuMon.Stop()

	gpus := sched.ListGPUs()
	log.Printf("Scheduler initialized with %d GPUs", len(gpus))
	for _, gpu := range gpus {
		log.Printf("  GPU %s: %s (%d MB)", gpu.Info.ID, gpu.Info.Name, gpu.Info.VRAMMB)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Prometheus metrics endpoint
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// HTTP metrics middleware
	r.Use(httpMetricsMiddleware())

	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// Dashboard
	r.GET("/dashboard", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("dashboard.html")
	})
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File("dashboard.html")
	})

	// Health
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "amd-gpu-scheduler",
			"version": "2.0",
			"gpus":    len(gpus),
		})
	})

	// Tasks API
	tasks := r.Group("/api/tasks")
	{
		tasks.POST("", func(c *gin.Context) {
			var req types.TaskRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			if req.Policy != "" {
				sched.SetDefaultPolicy(types.SchedulingPolicy(req.Policy))
			}

			task := &types.Task{
				Name:     req.Name,
				Type:     req.Type,
				Priority: req.Priority,
				GPUReq:   req.GPUReq,
				VRAMMB:   req.VRAMMB,
				Command:  req.Command,
				Args:     req.Args,
				Env:      req.Env,
				WorkDir:  req.WorkDir,
				Timeout:  req.Timeout,
			}

			metrics.RecordTaskSubmit(req.Type, req.Priority)
			assignedGPU, err := sched.SubmitTask(task)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			var createdTask *types.Task
			for _, t := range sched.ListTasks() {
				if t.ID == task.ID {
					createdTask = t
					break
				}
			}

			result := "success"
			if assignedGPU == "" {
				result = "no_gpu"
			}
			metrics.RecordScheduling(string(sched.GetDefaultPolicy()), result, 0)

			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"task_id": task.ID,
				"task":    createdTask,
				"policy":  sched.GetDefaultPolicy(),
			})
		})

		tasks.GET("", func(c *gin.Context) {
			t := sched.ListTasks()
			c.JSON(http.StatusOK, gin.H{"success": true, "tasks": t})
		})

		tasks.DELETE("/:id", func(c *gin.Context) {
			taskID := c.Param("id")
			if sched.CancelTask(taskID) {
				c.JSON(http.StatusOK, gin.H{"success": true, "message": "Task cancelled"})
			} else {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Task not found"})
			}
		})

		tasks.POST("/:id/complete", func(c *gin.Context) {
			taskID := c.Param("id")
			if sched.CompleteTask(taskID) {
				c.JSON(http.StatusOK, gin.H{"success": true, "message": "Task completed"})
			} else {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Task not found"})
			}
		})

		tasks.POST("/:id/stop", func(c *gin.Context) {
			taskID := c.Param("id")
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Task stop requested", "task_id": taskID})
		})

		tasks.GET("/:id/log", func(c *gin.Context) {
			taskID := c.Param("id")
			c.JSON(http.StatusOK, gin.H{"success": true, "task_id": taskID, "log": "log content..."})
		})
	}

	// GPU API
	gpusAPI := r.Group("/api/gpus")
	{
		gpusAPI.GET("", func(c *gin.Context) {
			g := sched.ListGPUs()
			for _, gpu := range g {
				metrics.RecordGPUUpdate(gpu.Info.ID, string(gpu.Info.Type), gpu.Info.Name,
					gpu.Usage.UsedVRAMMB, gpu.Info.VRAMMB, gpu.Usage.Utilization, gpu.Usage.RunningTasks)
				metrics.GPUScore.WithLabelValues(gpu.Info.ID, string(gpu.Info.Type), gpu.Info.Name).Set(gpu.Score)
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "gpus": g})
		})

		gpusAPI.GET("/:id", func(c *gin.Context) {
			gpuID := c.Param("id")
			gpu, ok := sched.GetGPU(gpuID)
			if !ok {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "GPU not found"})
				return
			}
			metrics.RecordGPUUpdate(gpu.Info.ID, string(gpu.Info.Type), gpu.Info.Name,
				gpu.Usage.UsedVRAMMB, gpu.Info.VRAMMB, gpu.Usage.Utilization, gpu.Usage.RunningTasks)
			metrics.GPUScore.WithLabelValues(gpu.Info.ID, string(gpu.Info.Type), gpu.Info.Name).Set(gpu.Score)
			c.JSON(http.StatusOK, gin.H{"success": true, "gpu": gpu})
		})

		gpusAPI.GET("/:id/score", func(c *gin.Context) {
			gpuID := c.Param("id")
			taskType := c.DefaultQuery("task_type", "training")
			vramMB := 4096
			fmt.Sscanf(c.DefaultQuery("vram_mb", "4096"), "%d", &vramMB)
			scoreBreakdown := sched.GetScoreBreakdown(gpuID, taskType, vramMB)
			c.JSON(http.StatusOK, gin.H{
				"success":         true,
				"gpu_id":          gpuID,
				"task_type":       taskType,
				"vram_required":   vramMB,
				"score_breakdown": scoreBreakdown,
			})
		})
	}

	// Scheduler API
	schedAPI := r.Group("/api/scheduler")
	{
		schedAPI.GET("/policy", func(c *gin.Context) {
			policy := sched.GetDefaultPolicy()
			c.JSON(http.StatusOK, gin.H{
				"success":     true,
				"policy":      policy,
				"description": getPolicyDescription(policy),
			})
		})

		schedAPI.POST("/policy", func(c *gin.Context) {
			var req struct{ Policy string `json:"policy"` }
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			sched.SetDefaultPolicy(types.SchedulingPolicy(req.Policy))
			c.JSON(http.StatusOK, gin.H{
				"success":     true,
				"policy":      req.Policy,
				"description": getPolicyDescription(types.SchedulingPolicy(req.Policy)),
			})
		})

		schedAPI.GET("/status", func(c *gin.Context) {
			tasks := sched.ListTasks()
			gpus := sched.ListGPUs()
			pendingCount, runningCount := 0, 0
			for _, t := range tasks {
				if t.Status == "pending" {
					pendingCount++
				} else if t.Status == "running" {
					runningCount++
				}
			}
			c.JSON(http.StatusOK, gin.H{
				"success":       true,
				"total_tasks":   len(tasks),
				"pending_tasks": pendingCount,
				"running_tasks": runningCount,
				"total_gpus":    len(gpus),
				"policy":        sched.GetDefaultPolicy(),
			})
		})
	}

	// Executor API
	execAPI := r.Group("/api/executor")
	{
		execAPI.GET("/running", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"success": true, "tasks": []string{}})
		})
		execAPI.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"success": true, "status": "running"})
		})
	}

	addr := ":8080"
	log.Printf("")
	log.Printf("API server listening on %s", addr)
	log.Printf("  Health:    http://localhost%s/health", addr)
	log.Printf("  Metrics:   http://localhost%s/metrics  (Prometheus)", addr)
	log.Printf("  Dashboard: http://localhost%s/dashboard", addr)
	log.Printf("  Tasks:     http://localhost%s/api/tasks", addr)
	log.Printf("  GPUs:      http://localhost%s/api/gpus", addr)
	log.Printf("")

	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func httpMetricsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.FullPath()
		if path == "" {
			path = c.Request.URL.Path
		}
		c.Next()
		status := strconv.Itoa(c.Writer.Status())
		duration := time.Since(start).Seconds()
		metrics.HTTPRequestsTotal.WithLabelValues(c.Request.Method, path, status).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(c.Request.Method, path).Observe(duration)
	}
}

func getPolicyDescription(policy types.SchedulingPolicy) string {
	switch policy {
	case types.PolicyBinpack:
		return "Binpack: compact scheduling"
	case types.PolicySpread:
		return "Spread: load balancing"
	case types.PolicyGPUType:
		return "GPU Type: type-first scheduling"
	default:
		return "Unknown policy"
	}
}
