package types

import "time"

// NodeRole represents the role of a node in the cluster
type NodeRole string

const (
	RoleMaster NodeRole = "master"
	RoleAgent  NodeRole = "agent"
)

// NodeStatus represents the health status of a node
type NodeStatus string

const (
	NodeStatusOnline  NodeStatus = "online"
	NodeStatusOffline NodeStatus = "offline"
	NodeStatusBusy    NodeStatus = "busy"
)

// NodeInfo represents a node in the cluster (static info)
type NodeInfo struct {
	ID        string   // Unique node ID (hostname or UUID)
	Name      string   // Human-readable name
	Role      NodeRole // master / agent
	Address   string   // IP:Port (e.g., "192.168.1.100:8081")
	Region    string   // e.g., "lab-1", "office"
	Labels    map[string]string // k/v labels for scheduling hints
}

// NodeState represents a node's dynamic state (reported via heartbeat)
type NodeState struct {
	NodeID        string            // matches NodeInfo.ID
	Status        NodeStatus        // online / offline / busy
	 GPUs         []*GPUSnapshot    // all GPUs on this node
	RunningTasks  int               // number of tasks running on this node
	CPUUsage      float64           // 0-100%
	MemoryUsedMB  int               // used memory in MB
	MemoryTotalMB int               // total memory in MB
	UptimeSec     int               // seconds since agent started
	Version       string            // agent version
	LastHeartbeat time.Time          // timestamp of last heartbeat
}

// HeartbeatRequest is sent by agent to master
type HeartbeatRequest struct {
	NodeID  string            `json:"node_id"`
	Name    string            `json:"name"`
	Address string            `json:"address"` // ip:port of agent API
	Region  string            `json:"region"`
	Labels  map[string]string `json:"labels"`
	GPUs    []*GPUSnapshot     `json:"gpus"`
	Stats   NodeStats         `json:"stats"`
	Version string            `json:"version"`
}

// NodeStats contains node-level resource stats
type NodeStats struct {
	RunningTasks  int     `json:"running_tasks"`
	CPUUsage      float64 `json:"cpu_usage"`
	MemoryUsedMB  int     `json:"memory_used_mb"`
	MemoryTotalMB int     `json:"memory_total_mb"`
	UptimeSec     int     `json:"uptime_sec"`
}

// HeartbeatResponse is returned by master to agent
type HeartbeatResponse struct {
	ACK       bool             `json:"ack"`
	MasterID  string           `json:"master_id"`
	ClusterID string          `json:"cluster_id"`
	Tasks     []TaskDispatch   `json:"tasks,omitempty"` // tasks assigned to this node
	Commands  []NodeCommand    `json:"commands,omitempty"` // e.g., stop task
}

// TaskDispatch instructs an agent to run a task
type TaskDispatch struct {
	TaskID     string            `json:"task_id"`
	Name       string            `json:"name"`
	Command    string            `json:"command"`
	Args       []string          `json:"args"`
	Env        map[string]string `json:"env"`
	WorkDir    string            `json:"work_dir"`
	TimeoutSec int              `json:"timeout_sec"`
	GPUReq     int               `json:"gpu_req"`   // number of GPUs
	GPUIDs     []string          `json:"gpu_ids"`   // specific GPU IDs assigned
}

// NodeCommand is a control message from master to agent
type NodeCommand struct {
	Type    string `json:"type"` // "stop_task", "drain", "resume"
	TaskID  string `json:"task_id,omitempty"`
	Message string `json:"message,omitempty"`
}

// ClusterStatus is the full cluster view
type ClusterStatus struct {
	MasterID   string           `json:"master_id"`
	ClusterID  string           `json:"cluster_id"`
	Version    string           `json:"version"`
	UptimeSec  int              `json:"uptime_sec"`
	NodeCount  int             `json:"node_count"`
	TotalGPUs  int             `json:"total_gpus"`
	OnlineNodes int            `json:"online_nodes"`
	TotalTasks int             `json:"total_tasks"`
	RunningTasks int           `json:"running_tasks"`
	PendingTasks int          `json:"pending_tasks"`
	Nodes      []*NodeState     `json:"nodes"`
}

// NodeRegistration is sent when agent first connects
type NodeRegistration struct {
	NodeID  string            `json:"node_id"`
	Name    string            `json:"name"`
	Address string            `json:"address"`
	Region  string            `json:"region"`
	Labels  map[string]string `json:"labels"`
	GPUs    []*GPUSnapshot    `json:"gpus"`
	Version string            `json:"version"`
}

// AgentConfig is configuration for the agent binary
type AgentConfig struct {
	MasterURL   string            `json:"master_url"`   // e.g., "http://192.168.1.1:8080"
	NodeName    string            `json:"node_name"`
	NodeRegion  string            `json:"node_region"`
	NodeLabels  map[string]string `json:"node_labels"`
	HeartbeatSec int              `json:"heartbeat_sec"` // default 5
	API_PORT    int              `json:"api_port"`      // agent local API port
}

// MasterConfig is configuration for the master binary
type MasterConfig struct {
	Port         int    `json:"port"`          // master API port
	ClusterID    string `json:"cluster_id"`    // cluster name
	HeartbeatTTL int   `json:"heartbeat_ttl"` // seconds before node considered offline
}
