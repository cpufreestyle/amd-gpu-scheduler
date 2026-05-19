package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/amd-gpu-scheduler/internal/scheduler"
	"github.com/amd-gpu-scheduler/pkg/types"
	"github.com/gin-gonic/gin"
)

func main() {
	log.Println("🚀 AMD GPU Scheduler starting...")
	log.Println("   Hybrid NVIDIA + AMD GPU Scheduler v2.0")
	log.Println("   Based on HAMi-inspired scheduling algorithm")

	// 初始化混合调度器
	sched := scheduler.NewScheduler()
	log.Printf("✅ Scheduler initialized with %d GPUs:", 2)
	
	// 打印 GPU 信息
	gpus := sched.ListGPUs()
	for _, gpu := range gpus {
		log.Printf("   - GPU %s: %s (%d MB VRAM, %s)",
			gpu.Info.ID, gpu.Info.Name, gpu.Info.VRAMMB, gpu.Info.Type)
	}

	// Gin HTTP API
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Middleware
	r.Use(gin.Logger())
	r.Use(gin.Recovery())

	// Dashboard - serve the web UI
	r.GET("/dashboard", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.File("dashboard.html")
	})
	r.GET("/", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Header("Cache-Control", "no-cache")
		c.File("dashboard.html")
	})

	// 健康检查
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"service": "amd-gpu-scheduler",
			"version": "2.0",
			"gpus":    len(gpus),
		})
	})

	// 任务 API
	tasks := r.Group("/api/tasks")
	{
		// 提交任务
		tasks.POST("", func(c *gin.Context) {
			var req struct {
				Name     string `json:"name"`
				Type     string `json:"type"`
				Priority int    `json:"priority"`
				GPUReq   int    `json:"gpu_req"`
				VRAMMB   int    `json:"vram_mb"`
				Policy   string `json:"policy"`
			}
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// 设置调度策略
				if req.Policy != "" {
				sched.SetDefaultPolicy(types.SchedulingPolicy(req.Policy))
			}

			taskID, err := sched.SubmitTask(req.Name, req.Type, req.Priority, req.GPUReq, req.VRAMMB)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
				return
			}

			// 获取任务详情
			var task *types.Task
			for _, t := range sched.ListTasks() {
				if t.ID == taskID {
					task = t
					break
				}
			}

			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"task_id": taskID,
				"task":    task,
				"policy":  sched.GetDefaultPolicy(),
			})
		})

		// 列出所有任务
		tasks.GET("", func(c *gin.Context) {
			t := sched.ListTasks()
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"tasks":   t,
			})
		})

		// 取消任务
		tasks.DELETE("/:id", func(c *gin.Context) {
			taskID := c.Param("id")
			if sched.CancelTask(taskID) {
				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"message": "Task cancelled",
				})
			} else {
				c.JSON(http.StatusNotFound, gin.H{
					"success": false,
					"error":   "Task not found",
				})
			}
		})

		// 完成任务
		tasks.POST("/:id/complete", func(c *gin.Context) {
			taskID := c.Param("id")
			if sched.CompleteTask(taskID) {
				c.JSON(http.StatusOK, gin.H{
					"success": true,
					"message": "Task completed",
				})
			} else {
				c.JSON(http.StatusNotFound, gin.H{
					"success": false,
					"error":   "Task not found",
				})
			}
		})
	}

	// GPU API
	gpusAPI := r.Group("/api/gpus")
	{
		// 列出所有 GPU
		gpusAPI.GET("", func(c *gin.Context) {
			g := sched.ListGPUs()
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"gpus":    g,
			})
		})

		// 获取单个 GPU 详情
		gpusAPI.GET("/:id", func(c *gin.Context) {
			gpuID := c.Param("id")
			gpu, ok := sched.GetGPU(gpuID)
			if !ok {
				c.JSON(http.StatusNotFound, gin.H{
					"success": false,
					"error":   "GPU not found",
				})
				return
			}
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"gpu":     gpu,
			})
		})

		// 获取 GPU 打分详情
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

	// 调度器 API
	schedAPI := r.Group("/api/scheduler")
	{
		// 获取当前调度策略
		schedAPI.GET("/policy", func(c *gin.Context) {
			policy := sched.GetDefaultPolicy()
			c.JSON(http.StatusOK, gin.H{
				"success":     true,
				"policy":      policy,
				"description": getPolicyDescription(policy),
			})
		})

		// 设置调度策略
		schedAPI.POST("/policy", func(c *gin.Context) {
			var req struct {
				Policy string `json:"policy"`
			}
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

		// 获取调度器状态
		schedAPI.GET("/status", func(c *gin.Context) {
			tasks := sched.ListTasks()
			gpus := sched.ListGPUs()
			pendingCount := 0
			runningCount := 0
			for _, t := range tasks {
				if t.Status == "pending" {
					pendingCount++
				} else if t.Status == "running" {
					runningCount++
				}
			}
			c.JSON(http.StatusOK, gin.H{
				"success":      true,
				"total_tasks":  len(tasks),
				"pending_tasks": pendingCount,
				"running_tasks": runningCount,
				"total_gpus":   len(gpus),
				"policy":       sched.GetDefaultPolicy(),
			})
		})
	}

	// 启动服务
	addr := ":8080"
	log.Printf("")
	log.Printf("🌐 API server listening on %s", addr)
	log.Printf("   Health:     http://localhost%s/health", addr)
	log.Printf("   Tasks:      http://localhost%s/api/tasks", addr)
	log.Printf("   GPUs:       http://localhost%s/api/gpus", addr)
	log.Printf("   Scheduler:  http://localhost%s/api/scheduler", addr)
	log.Printf("")
	
	if err := r.Run(addr); err != nil {
		log.Fatalf("❌ Failed to start server: %v", err)
	}
}

// getPolicyDescription returns a description of the scheduling policy
func getPolicyDescription(policy types.SchedulingPolicy) string {
	switch policy {
	case types.PolicyBinpack:
		return "Binpack: 集中调度，尽量用少的 GPU，减少资源碎片"
	case types.PolicySpread:
		return "Spread: 分散调度，负载均衡，避免单卡过热"
	case types.PolicyGPUType:
		return "GPU Type: GPU 类型优先调度"
	default:
		return "Unknown policy"
	}
}
