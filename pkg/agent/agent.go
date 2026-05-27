package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/hybrid-gpu-scheduler/internal/executor"
	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// Agent is the node-side process that runs on each GPU machine
type Agent struct {
	config      types.AgentConfig
	nodeID      string
	heartbeatSec int

	// Local GPU monitoring
	gpuGetter *LocalGPUGetter

	// Local task executor
	execInst *executor.Executor

	// Task tracking
	mu         sync.RWMutex
	localTasks map[string]*types.Task // taskID -> task

	httpClient *http.Client
	stopCh     chan struct{}
}

// New creates a new agent
func New(cfg types.AgentConfig) *Agent {
	nodeID := cfg.NodeName
	if nodeID == "" {
		h, _ := os.Hostname()
		nodeID = h
	}
	if cfg.HeartbeatSec <= 0 {
		cfg.HeartbeatSec = 5
	}

	return &Agent{
		config:       cfg,
		nodeID:       nodeID,
		heartbeatSec: cfg.HeartbeatSec,
		gpuGetter:    NewLocalGPUGetter(),
		execInst:     executor.GetExecutor(),
		localTasks:   make(map[string]*types.Task),
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		stopCh:      make(chan struct{}),
	}
}

// Run starts the agent heartbeat loop
func (a *Agent) Run() error {
	if a.config.MasterURL == "" {
		return fmt.Errorf("MASTER_URL is required")
	}

	log.Printf("[agent] Starting agent %s -> master %s", a.nodeID, a.config.MasterURL)

	// Initial heartbeat
	if err := a.heartbeat(); err != nil {
		log.Printf("[agent] Initial heartbeat failed: %v", err)
	}

	// Heartbeat loop
	ticker := time.NewTicker(time.Duration(a.heartbeatSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			log.Printf("[agent] Agent stopping")
			return nil
		case <-ticker.C:
			if err := a.heartbeat(); err != nil {
				log.Printf("[agent] Heartbeat error: %v", err)
			}
		}
	}
}

// heartbeat sends GPU state to master and receives task dispatches
func (a *Agent) heartbeat() error {
	gpus := a.gpuGetter.GetGPUSnapshots()
	stats := a.collectStats()

	req := types.HeartbeatRequest{
		NodeID:  a.nodeID,
		Name:    a.nodeID,
		Address: fmt.Sprintf("%s:%d", getIP(), a.config.API_PORT),
		Region:  a.config.NodeRegion,
		Labels:  a.config.NodeLabels,
		GPUs:    gpus,
		Stats:   stats,
		Version: "1.1.0",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("%s/api/cluster/heartbeat", a.config.MasterURL)
	resp, err := a.httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST %s: %w", url, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("heartbeat returned %d", resp.StatusCode)
	}

	var hbResp types.HeartbeatResponse
	if err := json.NewDecoder(resp.Body).Decode(&hbResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	// Execute dispatched tasks asynchronously
	for _, task := range hbResp.Tasks {
		go a.executeTask(task)
	}

	return nil
}

// executeTask runs a task dispatched from the master
func (a *Agent) executeTask(dispatch types.TaskDispatch) {
	log.Printf("[agent] Executing task %s (%s) on GPUs %v", dispatch.Name, dispatch.TaskID, dispatch.GPUIDs)

	// Build env with GPU isolation
	env := dispatch.Env
	if env == nil {
		env = make(map[string]string)
	}

	// Set CUDA_VISIBLE_DEVICES or ROCR_VISIBLE_DEVICES based on GPU type
	gpuType := a.getGPUType(dispatch.GPUIDs)
	if gpuType == string(types.GPUTypeNVIDIA) {
		env["CUDA_VISIBLE_DEVICES"] = joinStrings(dispatch.GPUIDs, ",")
	} else {
		env["ROCR_VISIBLE_DEVICES"] = joinStrings(dispatch.GPUIDs, ",")
	}

	// Determine GPU ID (first assigned GPU)
	gpuID := ""
	if len(dispatch.GPUIDs) > 0 {
		gpuID = dispatch.GPUIDs[0]
	}

	task := &types.Task{
		ID:      dispatch.TaskID,
		Name:    dispatch.Name,
		Command: dispatch.Command,
		Args:    dispatch.Args,
		Env:     env,
		WorkDir: dispatch.WorkDir,
		Timeout: dispatch.TimeoutSec,
		Status:  "running",
	}

	now := time.Now()
	task.StartTime = &now

	a.mu.Lock()
	a.localTasks[dispatch.TaskID] = task
	a.mu.Unlock()

	// Execute locally via executor (async)
	err := a.execInst.SubmitTask(task, gpuID)
	if err != nil {
		log.Printf("[agent] Task %s submit error: %v", dispatch.TaskID, err)
		a.reportResult(dispatch.TaskID, -1, err.Error())
		return
	}

	// Wait for task completion (poll)
	a.waitTaskComplete(dispatch.TaskID)
}

// waitTaskComplete polls task status until done, then reports result
func (a *Agent) waitTaskComplete(taskID string) {
	// Poll every 2 seconds
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			a.reportResult(taskID, -1, "timeout waiting for task")
			return
		case <-ticker.C:
			a.mu.RLock()
			task, ok := a.localTasks[taskID]
			a.mu.RUnlock()
			if !ok {
				return
			}
			if task.Status != "running" {
				a.reportResult(taskID, task.ExitCode, task.Error)
				return
			}
		}
	}
}

// reportResult sends task result back to master
func (a *Agent) reportResult(taskID string, exitCode int, errMsg string) {
	body := map[string]interface{}{
		"node_id":   a.nodeID,
		"task_id":   taskID,
		"exit_code": exitCode,
		"error":     errMsg,
	}

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/api/cluster/tasks/result", a.config.MasterURL)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	a.httpClient.Do(req) // fire and forget
}

// collectStats gathers node-level resource stats
func (a *Agent) collectStats() types.NodeStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return types.NodeStats{
		RunningTasks:  len(a.localTasks),
		CPUUsage:      getCPUUsage(),
		MemoryUsedMB:  int(m.Alloc / 1024 / 1024),
		MemoryTotalMB: int(m.Sys / 1024 / 1024),
		UptimeSec:     0,
	}
}

// getGPUType determines GPU type from GPU IDs
func (a *Agent) getGPUType(gpuIDs []string) string {
	if len(gpuIDs) == 0 {
		return "unknown"
	}
	gpus := a.gpuGetter.GetGPUSnapshots()
	for _, g := range gpus {
		for _, id := range gpuIDs {
			if g.Info.ID == id {
				return string(g.Info.Type)
			}
		}
	}
	return "unknown"
}

// Stop gracefully stops the agent
func (a *Agent) Stop() {
	close(a.stopCh)
}

// Helpers
func getIP() string {
	addrs := getLocalIPs()
	if len(addrs) > 0 {
		return addrs[0]
	}
	return "127.0.0.1"
}

func joinStrings(ids []string, sep string) string {
	result := ""
	for i, id := range ids {
		if i > 0 {
			result += sep
		}
		result += id
	}
	return result
}
