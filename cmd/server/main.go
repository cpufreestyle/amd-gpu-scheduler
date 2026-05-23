package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/hybrid-gpu-scheduler/internal/executor"
	"github.com/hybrid-gpu-scheduler/internal/gpumonitor"
	"github.com/hybrid-gpu-scheduler/internal/metrics"
	"github.com/hybrid-gpu-scheduler/internal/scheduler"
	"github.com/hybrid-gpu-scheduler/pkg/sse"
	"github.com/hybrid-gpu-scheduler/pkg/types"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	log.Println("AMD GPU Scheduler starting...")
	log.Println("Hybrid NVIDIA + AMD GPU Scheduler v2.0")

	sched := scheduler.NewScheduler()
	execInst := executor.GetExecutor()

	// Integrate SSE for real-time log streaming
	sseMgr := sse.GetSSEManager()
	execInst.SetSSELogger(sseMgr)

	// When executor finishes a task, release GPU resources in scheduler
	execInst.SetOnTaskDone(func(taskID string) {
		sched.ReleaseTask(taskID)
	})

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
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	log.Printf("exeDir=%s", exeDir)
	dash := filepath.Join(exeDir, "dashboard.html")
	log.Printf("dash=%s", dash)
	if _, err := os.Stat(dash); os.IsNotExist(err) {
		log.Printf("WARNING: dashboard.html not found at %s", dash)
	}
	dash520 := filepath.Join(exeDir, "dashboard-520.html")

	r.GET("/dashboard", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File(dash)
	})
	r.GET("/dashboard-520", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File(dash520)
	})
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.File(dash)
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
			body, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read body"})
				return
			}
			req, err := types.ParseTaskRequest(body)
			if err != nil {
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

		// Get single task by ID
		tasks.GET("/:id", func(c *gin.Context) {
			taskID := c.Param("id")
			for _, t := range sched.ListTasks() {
				if t.ID == taskID {
					c.JSON(http.StatusOK, gin.H{"success": true, "task": t})
					return
				}
			}
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Task not found"})
		})

		tasks.DELETE("/:id", func(c *gin.Context) {
			taskID := c.Param("id")
			if sched.CancelTask(taskID) {
				c.JSON(http.StatusOK, gin.H{"success": true, "message": "Task cancelled"})
			} else {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Task not found"})
			}
		})

		// SSE stream: GET /api/tasks/stream - real-time task log streaming
		tasks.GET("/stream", func(c *gin.Context) {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("Connection", "keep-alive")
			c.Header("Access-Control-Allow-Origin", "*")
			c.Header("X-Accel-Buffering", "no")

			ch := sseMgr.Register()
			defer sseMgr.Unregister(ch)

			// Heartbeat every 20s to keep connection alive
			ticker := time.NewTicker(20 * time.Second)
			defer ticker.Stop()

			clientGone := c.Request.Context().Done()
			for {
				select {
				case ev := <-ch:
					data, _ := json.Marshal(ev)
					fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", ev.Type, data)
					c.Writer.Flush()
				case <-ticker.C:
					fmt.Fprintf(c.Writer, "event: heartbeat\ndata: {\"time\":\"%s\"}\n\n", time.Now().Format(time.RFC3339))
					c.Writer.Flush()
				case <-clientGone:
					return
				}
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
			// Check task exists
			found := false
			for _, t := range sched.ListTasks() {
				if t.ID == taskID {
					found = true
					break
				}
			}
			if !found {
				c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "Task not found"})
				return
			}
			// Try to stop via executor
			if err := execInst.StopTask(taskID); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
				return
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "message": "Task stop requested", "task_id": taskID})
		})

		tasks.GET("/:id/log", func(c *gin.Context) {
			taskID := c.Param("id")
			var logContent string
			for _, t := range sched.ListTasks() {
				if t.ID == taskID {
					if t.LogFile != "" {
						data, err := os.ReadFile(t.LogFile)
						if err == nil {
							logContent = string(data)
						}
					}
					break
				}
			}
			if logContent == "" {
				logContent = "No log available"
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "task_id": taskID, "log": logContent})
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
			running := execInst.ListRunningTasks()
			ids := make([]string, 0, len(running))
			for _, rt := range running {
				ids = append(ids, rt.Task.ID+" (PID:"+strconv.Itoa(rt.PID)+")")
			}
			c.JSON(http.StatusOK, gin.H{"success": true, "tasks": ids, "count": len(ids)})
		})
		execAPI.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{"success": true, "status": "running", "active_tasks": len(execInst.ListRunningTasks())})
		})
	}
	
	// Preemption API
	SetupPreemptAPI(r, sched)

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

