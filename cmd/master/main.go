package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/hybrid-gpu-scheduler/pkg/master"
	"github.com/hybrid-gpu-scheduler/pkg/types"
)

func main() {
	port := 8080
	if p := os.Getenv("MASTER_PORT"); p != "" {
		fmt.Sscanf(p, "%d", &port)
	}
	clusterID := os.Getenv("CLUSTER_ID")
	if clusterID == "" {
		clusterID = "default"
	}

	srv := master.New(clusterID)
	r := gin.Default()

	// ── Health ──────────────────────────────────────────────
	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok", "role": "master"})
	})

	// ── Cluster API ─────────────────────────────────────────
	api := r.Group("/api/cluster")
	{
		// Agent -> Master heartbeat
		api.POST("/heartbeat", func(c *gin.Context) {
			var req types.HeartbeatRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			resp := srv.HandleHeartbeat(req)
			c.JSON(200, resp)
		})

		// Agent reports task result
		api.POST("/tasks/result", func(c *gin.Context) {
			var body struct {
				NodeID   string `json:"node_id"`
				TaskID   string `json:"task_id"`
				ExitCode int    `json:"exit_code"`
				Error    string `json:"error"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			srv.HandleTaskResult(body.NodeID, body.TaskID, body.ExitCode, body.Error)
			c.JSON(200, gin.H{"ok": true})
		})

		// Cluster status
		api.GET("/status", func(c *gin.Context) {
			c.JSON(200, srv.GetClusterStatus())
		})
	}

	// ── Original task API (reuses same server) ───────────────
	// Task submission via cluster API
	api.POST("/tasks", func(c *gin.Context) {
		var req types.TaskRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		task, err := srv.SubmitTask(req)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		c.JSON(200, types.TaskResponse{Success: true, TaskID: task.ID, Task: task})
	})

	// Cleanup stale nodes periodically
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		for range ticker.C {
			srv.CleanupStaleNodes()
		}
	}()

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh
		log.Println("[master] Shutting down...")
		os.Exit(0)
	}()

	addr := fmt.Sprintf(":%d", port)
	log.Printf("[master] Starting cluster master on %s (cluster=%s)", addr, clusterID)
	log.Printf("[master] Agents connect to: http://<master-ip>:%d/api/cluster/heartbeat", port)
	log.Fatal(http.ListenAndServe(addr, r))
}
