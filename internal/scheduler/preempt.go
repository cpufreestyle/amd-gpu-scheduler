package scheduler

import (
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/amd-gpu-scheduler/pkg/types"
	"github.com/amd-gpu-scheduler/internal/executor"
)

// PreemptPolicy defines the preemption strategy
type PreemptPolicy string

const (
	PreemptPolicyKill       PreemptPolicy = "kill"       // 直接杀死低优先级任务
	PreemptPolicyWait       PreemptPolicy = "wait"       // 等待低优先级任务结束
	PreemptPolicyCheckpoint PreemptPolicy = "checkpoint" // 保存状态后恢复（预留）
)

// PreemptConfig holds preemption configuration
type PreemptConfig struct {
	Enabled                 bool          // 是否启用抢占
	MinPriorityToPreempt    int           // 最小抢占优先级（≥此值的任务可以抢占）
	MaxPriorityToBePreempted int         // 最大被抢占优先级（≤此值的任务可以被抢占）
	Policy                  PreemptPolicy // 抢占策略
	WaitTimeout             time.Duration // wait 策略的超时时间
}

// PreemptRecord records a preemption event
type PreemptRecord struct {
	Timestamp     time.Time      // 抢占时间
	PreemptorTask *types.Task   // 抢占者任务
	PreemptedTask *types.Task   // 被抢占的任务
	GPUID         string        // 被抢占的 GPU ID
	Policy        PreemptPolicy // 使用的抢占策略
	Success       bool          // 是否成功
	Message       string        // 详细信息
}

// DefaultPreemptConfig returns the default preemption configuration
func DefaultPreemptConfig() *PreemptConfig {
	return &PreemptConfig{
		Enabled:                  true,
		MinPriorityToPreempt:    7, // Priority ≥ 7 可以抢占
		MaxPriorityToBePreempted: 5, // Priority ≤ 5 可以被抢占
		Policy:                   PreemptPolicyKill,
		WaitTimeout:              5 * time.Minute,
	}
}

// tryPreempt tries to preempt low-priority tasks to make room for a new task
func (s *Scheduler) tryPreempt(newTask *types.Task, criteria *types.SchedulingCriteria) bool {
	if !s.preemptConfig.Enabled {
		return false
	}
	
	// Only high-priority tasks can trigger preemption
	if newTask.Priority < s.preemptConfig.MinPriorityToPreempt {
		return false
	}
	
	log.Printf("⚡ Trying preemption for task %s (priority %d)\n", newTask.ID, newTask.Priority)
	
	// Find preemptable tasks (running tasks with low priority)
	var preemptable []*types.Task
	for _, task := range s.runningTasks {
		if task.Priority <= s.preemptConfig.MaxPriorityToBePreempted &&
			newTask.Priority - task.Priority >= 2 {
			preemptable = append(preemptable, task)
		}
	}
	
	if len(preemptable) == 0 {
		log.Printf("  No preemptable tasks found\n")
		return false
	}
	
	// Sort by priority (lowest first) to preempt the least important tasks first
	sort.Slice(preemptable, func(i, j int) bool {
		return preemptable[i].Priority < preemptable[j].Priority
	})
	
	// Try to preempt tasks until we have enough resources
	for _, taskToPreempt := range preemptable {
		log.Printf("  Preempting task %s (priority %d) for task %s (priority %d)\n",
			taskToPreempt.ID, taskToPreempt.Priority, newTask.ID, newTask.Priority)
		
		// Execute preemption based on policy
		switch s.preemptConfig.Policy {
		case PreemptPolicyKill:
			taskToPreempt.Status = "killed"
			taskToPreempt.Error = fmt.Sprintf("Preempted by task %s (priority %d > %d)",
				newTask.ID, newTask.Priority, taskToPreempt.Priority)
			
			// Stop the task execution
			exec := executor.GetExecutor()
			if err := exec.StopTask(taskToPreempt.ID); err != nil {
				log.Printf("  ⚠️  Failed to stop task %s: %v\n", taskToPreempt.ID, err)
			}
			
			// Update GPU usage
			if gpu, ok := s.gpus[fmt.Sprintf("%d", taskToPreempt.GPUReq)]; ok {
				gpu.Usage.RunningTasks--
				gpu.Usage.UsedVRAMMB -= taskToPreempt.VRAMMB
			}
			
			// Remove from running tasks
			delete(s.runningTasks, taskToPreempt.ID)
			
			// Record preemption
			record := &PreemptRecord{
				Timestamp:     time.Now(),
				PreemptorTask:  newTask,
				PreemptedTask: taskToPreempt,
				GPUID:          fmt.Sprintf("%d", taskToPreempt.GPUReq),
				Policy:         PreemptPolicyKill,
				Success:        true,
				Message:        fmt.Sprintf("Task %s preempted", taskToPreempt.ID),
			}
			s.preemptHistory = append(s.preemptHistory, record)
			
			log.Printf("  ✅ Task %s preempted successfully\n", taskToPreempt.ID)
			
		case PreemptPolicyWait:
			// TODO: Implement wait logic (set flag and wait for task to finish)
			log.Printf("  ⚠️  Wait policy not yet implemented\n")
			continue
		}
		
		// Check if we now have enough resources
		if s.hasEnoughResources(criteria) {
			log.Printf("  ✅ Enough resources after preemption\n")
			return true
		}
	}
	
	return false
}

// hasEnoughResources checks if there's any GPU with enough free VRAM
func (s *Scheduler) hasEnoughResources(criteria *types.SchedulingCriteria) bool {
	for _, gpu := range s.gpus {
		if gpu.FreeVRAM() >= criteria.VRAMRequiredMB {
			return true
		}
	}
	return false
}

// GetPreemptHistory returns the preemption history
func (s *Scheduler) GetPreemptHistory() []*PreemptRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	
	history := make([]*PreemptRecord, len(s.preemptHistory))
	copy(history, s.preemptHistory)
	return history
}

// UpdatePreemptConfig updates the preemption configuration
func (s *Scheduler) UpdatePreemptConfig(config *PreemptConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	s.preemptConfig = config
	log.Printf("⚡ Preemption config updated: Enabled=%v, MinPriority=%d, MaxBePreempted=%d, Policy=%s\n",
		config.Enabled, config.MinPriorityToPreempt, config.MaxPriorityToBePreempted, config.Policy)
}
