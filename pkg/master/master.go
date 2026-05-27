package master

import (
	"fmt"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hybrid-gpu-scheduler/pkg/types"
)

// Server is the cluster master that coordinates all nodes
type Server struct {
	clusterID   string
	uptimeStart time.Time

	mu         sync.RWMutex
	nodes      map[string]*nodeEntry // nodeID -> node
	tasks      map[string]*clusterTask // taskID -> task
	taskQueue  []*types.Task
	taskCounter atomic.Int64

	// Scheduler integration
	localScheduler *LocalScheduler
}

// nodeEntry holds node info + state
type nodeEntry struct {
	info   types.NodeInfo
	state  types.NodeState
	lastHB time.Time
}

// clusterTask is a task managed by the cluster
type clusterTask struct {
	task       *types.Task
	targetNode string // nodeID where task is running (empty = pending)
	submittedAt time.Time
}

var defaultHeartbeatTTL = 30 * time.Second

// New creates a new master server
func New(clusterID string) *Server {
	return &Server{
		clusterID:   clusterID,
		uptimeStart: time.Now(),
		nodes:       make(map[string]*nodeEntry),
		tasks:       make(map[string]*clusterTask),
		taskQueue:   make([]*types.Task, 0),
		localScheduler: NewLocalScheduler(),
	}
}

// RegisterNode registers a new node (or updates existing)
func (s *Server) RegisterNode(req types.HeartbeatRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	existing, ok := s.nodes[req.NodeID]

	if !ok {
		// New node
		s.nodes[req.NodeID] = &nodeEntry{
			info: types.NodeInfo{
				ID:      req.NodeID,
				Name:    req.Name,
				Role:    types.RoleAgent,
				Address: req.Address,
				Region:  req.Region,
				Labels:  req.Labels,
			},
			state: types.NodeState{
				NodeID:   req.NodeID,
				Status:   types.NodeStatusOnline,
				GPUs:     req.GPUs,
				LastHeartbeat: now,
			},
			lastHB: now,
		}
		log.Printf("[master] Node registered: %s (%s)", req.Name, req.Address)
	} else {
		// Update existing node
		existing.state.GPUs = req.GPUs
		existing.state.CPUUsage = req.Stats.CPUUsage
		existing.state.MemoryUsedMB = req.Stats.MemoryUsedMB
		existing.state.MemoryTotalMB = req.Stats.MemoryTotalMB
		existing.state.RunningTasks = req.Stats.RunningTasks
		existing.state.LastHeartbeat = now
		existing.lastHB = now
	}
}

// HandleHeartbeat processes a heartbeat from an agent
func (s *Server) HandleHeartbeat(req types.HeartbeatRequest) types.HeartbeatResponse {
	s.mu.Lock()
	s.RegisterNode(req)

	// Get pending tasks to dispatch
	tasks := s.getPendingDispatchesLocked(req.NodeID)

	// Check for stop commands for this node's tasks
	cmds := s.getCommandsLocked(req.NodeID)

	s.mu.Unlock()

	// Update local scheduler GPU state from node GPUs
	s.localScheduler.UpdateGPUs(req.NodeID, req.GPUs)

	// Try to schedule pending tasks
	go s.trySchedule()

	return types.HeartbeatResponse{
		ACK:       true,
		MasterID:  "master",
		ClusterID: s.clusterID,
		Tasks:     tasks,
		Commands:  cmds,
	}
}

func (s *Server) getPendingDispatchesLocked(nodeID string) []types.TaskDispatch {
	var dispatches []types.TaskDispatch

	for _, ct := range s.tasks {
		if ct.targetNode == "" {
			// Pending task — try to schedule on this node
			gpus := s.getNodeGPUsLocked(nodeID)
			assigned, gpuIDs := s.localScheduler.TrySchedule(ct.task, gpus)
			if assigned {
				ct.targetNode = nodeID
				dispatches = append(dispatches, types.TaskDispatch{
					TaskID:     ct.task.ID,
					Name:       ct.task.Name,
					Command:    ct.task.Command,
					Args:       ct.task.Args,
					Env:        ct.task.Env,
					WorkDir:    ct.task.WorkDir,
					TimeoutSec: ct.task.Timeout,
					GPUReq:     ct.task.GPUReq,
					GPUIDs:     gpuIDs,
				})
			}
		}
	}
	return dispatches
}

func (s *Server) getCommandsLocked(nodeID string) []types.NodeCommand {
	// TODO: implement command queue per node
	return nil
}

func (s *Server) getNodeGPUsLocked(nodeID string) []*types.GPUSnapshot {
	if n, ok := s.nodes[nodeID]; ok {
		return n.state.GPUs
	}
	return nil
}

// SubmitTask submits a task to the cluster
func (s *Server) SubmitTask(req types.TaskRequest) (*types.Task, error) {
	id := fmt.Sprintf("ctask-%d", s.taskCounter.Add(1))
	task := &types.Task{
		ID:       id,
		Name:     req.Name,
		Type:     req.Type,
		Priority: req.Priority,
		GPUReq:   req.GPUReq,
		VRAMMB:   req.VRAMMB,
		Status:   "pending",
		Command:  req.Command,
		Args:     req.Args,
		Env:      req.Env,
		WorkDir:  req.WorkDir,
		Timeout:  req.Timeout,
	}

	now := time.Now()
	task.StartTime = &now

	s.mu.Lock()
	s.tasks[id] = &clusterTask{task: task, submittedAt: now}
	s.taskQueue = append(s.taskQueue, task)
	s.mu.Unlock()

	// Try to schedule immediately
	go s.trySchedule()

	log.Printf("[master] Task submitted: %s (%s, VRAM=%dMB)", task.Name, task.Type, task.VRAMMB)
	return task, nil
}

// trySchedule attempts to schedule pending tasks to available nodes
func (s *Server) trySchedule() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, ct := range s.tasks {
		if ct.targetNode != "" {
			continue // already assigned
		}

		// Find a node with suitable GPUs
		for nodeID, entry := range s.nodes {
			if entry.state.Status == types.NodeStatusOffline {
				continue
			}

			assigned, gpuIDs := s.localScheduler.TrySchedule(ct.task, entry.state.GPUs)
			if assigned {
				ct.targetNode = nodeID
				log.Printf("[master] Task %s scheduled on node %s (GPUs: %v)", ct.task.Name, nodeID, gpuIDs)
				break
			}
		}
	}
}

// HandleTaskResult processes a task completion report from an agent
func (s *Server) HandleTaskResult(nodeID, taskID string, exitCode int, errorMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if ct, ok := s.tasks[taskID]; ok {
		ct.task.Status = "completed"
		if exitCode != 0 {
			ct.task.Status = "failed"
			ct.task.Error = errorMsg
		}
		ct.task.ExitCode = exitCode
		now := time.Now()
		ct.task.EndTime = &now
		ct.targetNode = "" // release

		log.Printf("[master] Task %s on node %s finished: %s (exit=%d)", taskID, nodeID, ct.task.Status, exitCode)
	}
}

// GetClusterStatus returns full cluster view
func (s *Server) GetClusterStatus() types.ClusterStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	var nodes []*types.NodeState
	totalGPUs := 0
	onlineNodes := 0
	running := 0
	pending := 0

	now := time.Now()
	for _, entry := range s.nodes {
		// Mark stale nodes as offline
		if now.Sub(entry.lastHB) > defaultHeartbeatTTL {
			entry.state.Status = types.NodeStatusOffline
		} else if entry.state.Status != types.NodeStatusOffline {
			entry.state.Status = types.NodeStatusOnline
			onlineNodes++
		}

		nodes = append(nodes, &entry.state)
		totalGPUs += len(entry.state.GPUs)
		running += entry.state.RunningTasks
	}

	for _, ct := range s.tasks {
		if ct.targetNode == "" {
			pending++
		}
	}

	return types.ClusterStatus{
		MasterID:     "master",
		ClusterID:    s.clusterID,
		Version:      "1.1.0",
		UptimeSec:    int(time.Since(s.uptimeStart).Seconds()),
		NodeCount:    len(s.nodes),
		TotalGPUs:    totalGPUs,
		OnlineNodes:  onlineNodes,
		TotalTasks:   len(s.tasks),
		RunningTasks: running,
		PendingTasks: pending,
		Nodes:        nodes,
	}
}

// CleanupStaleNodes removes nodes that haven't heartbeat within TTL
func (s *Server) CleanupStaleNodes() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for id, entry := range s.nodes {
		if now.Sub(entry.lastHB) > defaultHeartbeatTTL*3 {
			delete(s.nodes, id)
			log.Printf("[master] Node timed out and removed: %s", id)
		}
	}
}
