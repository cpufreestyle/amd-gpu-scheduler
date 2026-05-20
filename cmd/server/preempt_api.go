package main

import (
	"net/http"

	"github.com/amd-gpu-scheduler/internal/scheduler"
	"github.com/gin-gonic/gin"
)

// SetupPreemptAPI sets up the preemption-related API endpoints
func SetupPreemptAPI(r *gin.Engine, sched *scheduler.Scheduler) {
	schedAPI := r.Group("/api/scheduler")
	{
		// Get preemption history
		schedAPI.GET("/preempt/history", func(c *gin.Context) {
			history := sched.GetPreemptHistory()
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"history": history,
				"count":   len(history),
			})
		})

		// Get preemption config
		schedAPI.GET("/preempt/config", func(c *gin.Context) {
			// Note: This accesses internal field, might need a getter method
			// For now, return a default config
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"config": gin.H{
					"enabled":                   true,
					"min_priority_to_preempt":    7,
					"max_priority_to_be_preempted": 5,
					"policy":                     "kill",
					"wait_timeout_seconds":       300,
				},
			})
		})

		// Update preemption config
		schedAPI.POST("/preempt/config", func(c *gin.Context) {
			var req struct {
				Enabled                  *bool   `json:"enabled"`
				MinPriorityToPreempt     *int    `json:"min_priority_to_preempt"`
				MaxPriorityToBePreempted *int    `json:"max_priority_to_be_preempted"`
				Policy                  *string `json:"policy"`
				WaitTimeoutSeconds      *int    `json:"wait_timeout_seconds"`
			}
			
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			
			// TODO: Actually update the config via sched.UpdatePreemptConfig()
			// For now, just return success
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "Preemption config updated (not implemented yet)",
			})
		})

		// Manual preemption trigger
		schedAPI.POST("/preempt", func(c *gin.Context) {
			var req struct {
				PreemptorTaskID string `json:"preemptor_task_id"`
				PreemptedTaskID string `json:"preempted_task_id"`
			}
			
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			
			// TODO: Implement actual preemption via sched.PreemptTask()
			// For now, return a placeholder response
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "Preemption triggered (not implemented yet)",
				"preemptor_task_id": req.PreemptorTaskID,
				"preempted_task_id": req.PreemptedTaskID,
			})
		})
	}
}
