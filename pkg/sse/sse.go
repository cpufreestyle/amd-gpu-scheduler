package sse

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// SSEEvent represents a server-sent event
type SSEEvent struct {
	Type    string      `json:"type"`    // event type: task_log, task_status, gpu_update, heartbeat
	TaskID  string      `json:"task_id,omitempty"`
	Data    interface{} `json:"data"`
	Time    string      `json:"time"`
}

// SSEManager manages SSE client connections and broadcasting
type SSEManager struct {
	mu       sync.RWMutex
	clients  map[chan SSEEvent]struct{}
	register chan chan SSEEvent
	unregister chan chan SSEEvent
	broadcast chan SSEEvent
}

var globalSSE *SSEManager
var sseOnce sync.Once

// GetSSEManager returns the singleton SSE manager
func GetSSEManager() *SSEManager {
	sseOnce.Do(func() {
		globalSSE = &SSEManager{
			clients:    make(map[chan SSEEvent]struct{}),
			register:   make(chan chan SSEEvent, 64),
			unregister: make(chan chan SSEEvent, 64),
			broadcast:  make(chan SSEEvent, 256),
		}
		go globalSSE.run()
	})
	return globalSSE
}

func (m *SSEManager) run() {
	for {
		select {
		case ch := <-m.register:
			m.mu.Lock()
			m.clients[ch] = struct{}{}
			m.mu.Unlock()
			// Send initial connection event
			select {
			case ch <- SSEEvent{
				Type: "connected",
				Data: "SSE connected",
				Time: time.Now().Format(time.RFC3339),
			}:
			default:
				// client buffer full, skip
			}

		case ch := <-m.unregister:
			m.mu.Lock()
			delete(m.clients, ch)
			m.mu.Unlock()
			close(ch)

		case ev := <-m.broadcast:
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			msg := fmt.Sprintf("event: %s\ndata: %s\n\n", ev.Type, data)
			m.mu.RLock()
			for ch := range m.clients {
				select {
				case ch <- ev:
				default:
					// slow client, skip
				}
			}
			_ = msg // silence unused warning
			m.mu.RUnlock()
		}
	}
}

// Register registers a new SSE client channel
func (m *SSEManager) Register() chan SSEEvent {
	ch := make(chan SSEEvent, 64)
	m.register <- ch
	return ch
}

// Unregister removes a client channel
func (m *SSEManager) Unregister(ch chan SSEEvent) {
	m.unregister <- ch
}

// Broadcast sends an event to all connected clients
func (m *SSEManager) Broadcast(ev SSEEvent) {
	select {
	case m.broadcast <- ev:
	default:
		// broadcast channel full, event dropped
	}
}

// BroadcastTaskLog sends a task log event
func (m *SSEManager) BroadcastTaskLog(taskID, line string) {
	m.Broadcast(SSEEvent{
		Type:   "task_log",
		TaskID: taskID,
		Data:   map[string]string{"task_id": taskID, "line": line},
		Time:   time.Now().Format(time.RFC3339),
	})
}

// BroadcastTaskStatus sends a task status change event
func (m *SSEManager) BroadcastTaskStatus(taskID, status string) {
	m.Broadcast(SSEEvent{
		Type:   "task_status",
		TaskID: taskID,
		Data:   map[string]string{"task_id": taskID, "status": status},
		Time:   time.Now().Format(time.RFC3339),
	})
}

// BroadcastGPUUpdate sends a GPU update event
func (m *SSEManager) BroadcastGPUUpdate(gpuID string, data interface{}) {
	m.Broadcast(SSEEvent{
		Type: "gpu_update",
		Data: map[string]interface{}{"gpu_id": gpuID, "data": data, "time": time.Now().Format(time.RFC3339)},
		Time: time.Now().Format(time.RFC3339),
	})
}

// ClientCount returns the number of connected clients
func (m *SSEManager) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}
