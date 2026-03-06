package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // CORS is handled by middleware
	},
}

// Hub manages WebSocket clients and broadcasts messages by channel.
type Hub struct {
	mu         sync.RWMutex
	clients    map[*Client]bool
	register   chan *Client
	unregister chan *Client
	logger     *zap.Logger
}

// NewHub creates a new WebSocket hub.
func NewHub(logger *zap.Logger) *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		register:   make(chan *Client),
		unregister: make(chan *Client),
		logger:     logger,
	}
}

// Run starts the hub's main loop.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			h.mu.Lock()
			for client := range h.clients {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			return
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
			h.logger.Debug("ws client connected", zap.Int("total", len(h.clients)))
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				close(client.send)
				delete(h.clients, client)
			}
			h.mu.Unlock()
			h.logger.Debug("ws client disconnected", zap.Int("total", len(h.clients)))
		}
	}
}

// BroadcastToChannel sends a message to all clients subscribed to the channel.
func (h *Hub) BroadcastToChannel(channel string, msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("ws marshal error", zap.Error(err))
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for client := range h.clients {
		if client.isSubscribed(channel) {
			select {
			case client.send <- data:
			default:
				// Client too slow, will be cleaned up
			}
		}
	}
}

// HandleWebSocket upgrades the HTTP connection to WebSocket.
func (h *Hub) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		h.logger.Error("ws upgrade failed", zap.Error(err))
		return
	}

	client := newClient(h, conn, h.logger)
	h.register <- client

	go client.writePump()
	go client.readPump()
}
