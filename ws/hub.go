package ws

import (
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Hub maintains the set of active clients and broadcasts messages.
type Hub struct {
	Clients    map[*websocket.Conn]bool
	Broadcast  chan []byte
	Register   chan *websocket.Conn
	Unregister chan *websocket.Conn
	mu         sync.Mutex
}

func NewHub() *Hub {
	return &Hub{
		Clients:    make(map[*websocket.Conn]bool),
		Broadcast:  make(chan []byte, 256),
		Register:   make(chan *websocket.Conn),
		Unregister: make(chan *websocket.Conn),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.Register:
			h.mu.Lock()
			h.Clients[client] = true
			h.mu.Unlock()

		case client := <-h.Unregister:
			h.mu.Lock()
			if _, ok := h.Clients[client]; ok {
				delete(h.Clients, client)
				client.Close()
			}
			h.mu.Unlock()

		case msg := <-h.Broadcast:
			// 1. Copy clients list to avoid holding lock during network I/O
			h.mu.Lock()
			clients := make([]*websocket.Conn, 0, len(h.Clients))
			for client := range h.Clients {
				clients = append(clients, client)
			}
			h.mu.Unlock()

			// 2. Broadcast to clients without blocking the Hub
			for _, client := range clients {
				if err := client.WriteMessage(websocket.TextMessage, msg); err != nil {
					log.Printf("websocket write error: %v", err)
					h.Unregister <- client
				}
			}
		}
	}
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all for now
	},
}

func ServeWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}

	hub.Register <- conn

	// Cleanup logic
	defer func() {
		hub.Unregister <- conn
	}()

	// Read loop (to detect disconnects)
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
}
