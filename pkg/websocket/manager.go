package websocket

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// MessageType defines the WebSocket message type
type MessageType string

const (
	TypeGPUUpdate    MessageType = "gpu_update"
	TypeTaskUpdate   MessageType = "task_update"
	TypeTaskLog      MessageType = "task_log"
	TypeClusterNode  MessageType = "cluster_node"
	TypeHeartbeat    MessageType = "heartbeat"
	TypePing         MessageType = "ping"
	TypePong         MessageType = "pong"
)

// Message is a WebSocket message
type Message struct {
	Type    MessageType   `json:"type"`
	Payload interface{}   `json:"payload,omitempty"`
}

// Client represents a connected WebSocket client
type Client struct {
	ID     string
	Conn   *websocket.Conn
	Send   chan []byte
	IsDash bool // true if this is a dashboard client
}

// Manager handles all WebSocket connections
type Manager struct {
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	broadcast  chan []byte
	mu         sync.RWMutex
	upgrader   websocket.Upgrader
}

var globalManager *Manager
var managerOnce sync.Once

// GetManager returns the singleton WebSocket manager
func GetManager() *Manager {
	managerOnce.Do(func() {
		globalManager = &Manager{
			clients:    make(map[*Client]bool),
			register:   make(chan *Client, 100),
			unregister: make(chan *Client, 100),
			broadcast:  make(chan []byte, 500),
			upgrader: websocket.Upgrader{
				ReadBufferSize:  1024,
				WriteBufferSize: 1024,
				CheckOrigin: func(r *http.Request) bool {
					return true // allow all origins for now
				},
			},
		}
		go globalManager.run()
	})
	return globalManager
}

// run handles register/unregister/broadcast in a dedicated goroutine
func (m *Manager) run() {
	for {
		select {
		case c := <-m.register:
			m.mu.Lock()
			m.clients[c] = true
			m.mu.Unlock()
			log.Printf("[ws] client connected: %s (total: %d)", c.ID, len(m.clients))

		case c := <-m.unregister:
			m.mu.Lock()
			if _, ok := m.clients[c]; ok {
				delete(m.clients, c)
				close(c.Send)
			}
			m.mu.Unlock()
			log.Printf("[ws] client disconnected: %s (total: %d)", c.ID, len(m.clients))

		case msg := <-m.broadcast:
			m.mu.RLock()
			for c := range m.clients {
				select {
				case c.Send <- msg:
				default:
					// client buffer full, skip
				}
			}
			m.mu.RUnlock()
		}
	}
}

// HandleWebSocket upgrades an HTTP connection to WebSocket
func (m *Manager) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := m.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[ws] upgrade error: %v", err)
		return
	}

	// Check if it's a dashboard connection
	isDash := r.URL.Query().Get("type") == "dashboard"
	clientID := r.URL.Query().Get("id")
	if clientID == "" {
		clientID = randomID(8)
	}

	client := &Client{
		ID:     clientID,
		Conn:   conn,
		Send:   make(chan []byte, 256),
		IsDash: isDash,
	}

	m.register <- client

	// Start read and write goroutines
	go client.writePump()
	go client.readPump(m)
}

// readPump handles incoming messages from client
func (c *Client) readPump(m *Manager) {
	defer func() {
		m.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadLimit(4096)
	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, msg, err := c.Conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] read error %s: %v", c.ID, err)
			}
			break
		}

		// Parse incoming message
		var in Message
		if err := json.Unmarshal(msg, &in); err != nil {
			continue
		}

		// Handle ping
		if in.Type == TypePing {
			reply, _ := json.Marshal(Message{Type: TypePong})
			c.Send <- reply
		}
	}
}

// writePump sends messages to the client, including periodic ping
func (c *Client) writePump() {
	ticker := time.NewTicker(25 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// Broadcast sends a message to all connected clients
func (m *Manager) Broadcast(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	// Non-blocking send
	select {
	case m.broadcast <- data:
	default:
		// broadcast channel full
	}
}

// BroadcastToDashboards sends a message to all dashboard clients only
func (m *Manager) BroadcastToDashboards(msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for c := range m.clients {
		if c.IsDash {
			select {
			case c.Send <- data:
			default:
			}
		}
	}
}

// SendToClient sends a message to a specific client by ID
func (m *Manager) SendToClient(clientID string, msg Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for c := range m.clients {
		if c.ID == clientID {
			select {
			case c.Send <- data:
			default:
			}
			return
		}
	}
}

// ClientCount returns the number of connected clients
func (m *Manager) ClientCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.clients)
}

// ClientCountDashboards returns the number of connected dashboard clients
func (m *Manager) ClientCountDashboards() int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	count := 0
	for c := range m.clients {
		if c.IsDash {
			count++
		}
	}
	return count
}

// Helpers
func randomID(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}
