package websocket

import (
	"log"
	"strings"
)

// LogBridge implements the SSELogger interface to broadcast task logs via WebSocket.
type LogBridge struct{}

// NewLogBridge creates a new LogBridge.
func NewLogBridge() *LogBridge {
	return &LogBridge{}
}

// BroadcastTaskLog sends a log line to all dashboard clients.
// This method satisfies the SSELogger interface defined in internal/executor.
func (l *LogBridge) BroadcastTaskLog(taskID, line string) {
	line = strings.TrimRight(line, "\r\n")
	if line == "" {
		return
	}

	msg := Message{
		Type: TypeTaskLog,
		Payload: map[string]interface{}{
			"task_id": taskID,
			"line":    line,
		},
	}

	GetManager().BroadcastToDashboards(msg)
	log.Printf("[log_bridge] task=%s len=%d", taskID, len(line))
}
