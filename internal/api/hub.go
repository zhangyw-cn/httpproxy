package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/zhangyw-cn/httpproxy/internal/flow"
)

const clientQueueSize = 64

type Event struct {
	Type   string            `json:"type"`
	Flow   *flow.FlowSummary `json:"flow,omitempty"`
	FlowID string            `json:"flow_id,omitempty"`
}

type Hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
}

type client struct {
	conn      *websocket.Conn
	send      chan []byte
	done      chan struct{}
	closeOnce sync.Once
	enqueueMu sync.Mutex
}

func NewHub() *Hub {
	return &Hub{clients: make(map[*client]struct{})}
}

func (h *Hub) add(conn *websocket.Conn) *client {
	c := &client{
		conn: conn,
		send: make(chan []byte, clientQueueSize),
		done: make(chan struct{}),
	}
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
	go h.writeLoop(c)
	return c
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.closeOnce.Do(func() { close(c.done) })
}

// ClientCount returns the number of currently registered WebSocket clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) Broadcast(event Event) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	h.mu.RLock()
	clients := make([]*client, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		c.enqueue(data)
	}
}

// enqueue never waits for a client. If the queue is full, it drops the oldest
// pending event so that the newest proxy state can still be delivered.
func (c *client) enqueue(data []byte) {
	c.enqueueMu.Lock()
	defer c.enqueueMu.Unlock()

	select {
	case <-c.done:
		return
	default:
	}

	select {
	case c.send <- data:
		return
	default:
	}

	select {
	case <-c.send:
	default:
	}
	select {
	case c.send <- data:
	default:
	}
}

func (h *Hub) writeLoop(c *client) {
	for {
		select {
		case <-c.done:
			return
		case data := <-c.send:
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			err := c.conn.Write(ctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				h.remove(c)
				c.conn.CloseNow()
				return
			}
		}
	}
}
