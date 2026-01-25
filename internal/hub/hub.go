package hub

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
	"opspilot-backend/internal/models"
)

type Hub struct {
	clients map[string]*websocket.Conn
	mu      sync.RWMutex
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*websocket.Conn),
	}
}

func (h *Hub) Add(agentID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[agentID] = conn
	log.Printf("Agent %s connected. Total clients: %d", agentID, len(h.clients))
}

func (h *Hub) Remove(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[agentID]; ok {
		delete(h.clients, agentID)
		log.Printf("Agent %s disconnected. Total clients: %d", agentID, len(h.clients))
	}
}

func (h *Hub) SendCommand(agentID string, cmd models.CommandPayload) error {
	h.mu.RLock()
	conn, ok := h.clients[agentID]
	h.mu.RUnlock()

	if !ok {
		return nil
	}

	data, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

func (h *Hub) Broadcast(cmd models.CommandPayload) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	data, err := json.Marshal(cmd)
	if err != nil {
		log.Printf("Error marshaling broadcast command: %v", err)
		return
	}

	for _, conn := range h.clients {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			log.Printf("Error sending broadcast: %v", err)
		}
	}
}
