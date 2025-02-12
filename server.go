package main

import (
	"encoding/binary"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	panelSize  = 128
	numPanels  = 1920
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 512

	// Message type constants:
	MsgTypeUpdate    = 1 // Client → Server: 7 bytes: type, panel, x, y, r, g, b.
	MsgTypeRequest   = 2 // (Not used here; could be used to ask for a refresh.)
	MsgTypeUpdateAck = 3 // Server → Client: 2 bytes: type, result.
	MsgTypeBroadcast = 4 // Server → Client: 15 bytes: type, panel, x, y, r, g, b, timestamp (8 bytes).
	MsgTypePanelSync = 5 // Server → Client: 2-byte header (type, panel) + 128×128×3 bytes of raw RGB.
)

// Pixel holds a color (R, G, B) and a timestamp.
type Pixel struct {
	R, G, B   byte
	Timestamp int64
}

// A Panel is a 128×128 array of Pixels.
type Panel [panelSize][panelSize]Pixel

// Global panels and mutex for concurrent access.
var panels [numPanels]Panel
var panelMutex sync.RWMutex

// OutgoingMessage wraps a websocket message.
type OutgoingMessage struct {
	messageType int
	data        []byte
}

// Hub maintains the set of connected clients.
type Hub struct {
	clients    map[*Client]bool
	broadcast  chan OutgoingMessage
	register   chan *Client
	unregister chan *Client
	mu         sync.Mutex
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan OutgoingMessage),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			h.clients[client] = true
			h.mu.Unlock()
		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
			h.mu.Unlock()
		case message := <-h.broadcast:
			h.mu.Lock()
			for client := range h.clients {
				select {
				case client.send <- message:
				default:
					close(client.send)
					delete(h.clients, client)
				}
			}
			h.mu.Unlock()
		}
	}
}

// Client represents a connected websocket client.
type Client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan OutgoingMessage
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// In production, you should check the origin.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// serveWs upgrades the HTTP connection to a websocket, registers the client,
// and sends a full sync of all panels.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan OutgoingMessage, 256),
	}
	hub.register <- client

	// When a new client connects, send a full-panel sync for each panel.
	syncPanels(client)

	go client.writePump()
	go client.readPump()
}

// syncPanels sends, for each panel, a message that includes the panel number and
// the raw RGB data (128×128×3 bytes). The message layout is:
//   Byte 0: MsgTypePanelSync (5)
//   Byte 1: Panel number (0–9)
//   Bytes 2..(2+49152-1): 128×128×3 bytes of pixel data.
func syncPanels(c *Client) {
	for panel := 0; panel < numPanels; panel++ {
		buf := make([]byte, 2+panelSize*panelSize*3)
		buf[0] = MsgTypePanelSync
		buf[1] = byte(panel)
		idx := 2
		panelMutex.RLock()
		for y := 0; y < panelSize; y++ {
			for x := 0; x < panelSize; x++ {
				p := panels[panel][y][x]
				buf[idx] = p.R
				buf[idx+1] = p.G
				buf[idx+2] = p.B
				idx += 3
			}
		}
		panelMutex.RUnlock()
		c.send <- OutgoingMessage{messageType: websocket.BinaryMessage, data: buf}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMsgSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		msgType, data, err := c.conn.ReadMessage()
		if err != nil {
			break
		}
		// Our protocol expects binary messages.
		if msgType != websocket.BinaryMessage {
			log.Println("Ignoring non-binary message")
			continue
		}
		if len(data) < 1 {
			continue
		}
		switch data[0] {
		case MsgTypeUpdate:
			// Expect 7 bytes: type, panel, x, y, r, g, b.
			if len(data) < 7 {
				log.Println("Invalid update message length")
				continue
			}
			panel := int(data[1])
			x := int(data[2])
			y := int(data[3])
			rVal := data[4]
			gVal := data[5]
			bVal := data[6]

			if panel < 0 || panel >= numPanels || x < 0 || x >= panelSize || y < 0 || y >= panelSize {
				log.Println("Invalid update parameters")
				continue
			}

			now := time.Now().UnixMilli()
			panelMutex.Lock()
			p := &panels[panel][y][x]
			if now > p.Timestamp {
				p.R = rVal
				p.G = gVal
				p.B = bVal
				p.Timestamp = now
			}
			panelMutex.Unlock()

			// Broadcast update to all clients.
			// Broadcast message (15 bytes): type, panel, x, y, r, g, b, timestamp (8 bytes).
			bcast := make([]byte, 15)
			bcast[0] = MsgTypeBroadcast
			bcast[1] = byte(panel)
			bcast[2] = byte(x)
			bcast[3] = byte(y)
			bcast[4] = rVal
			bcast[5] = gVal
			bcast[6] = bVal
			binary.BigEndian.PutUint64(bcast[7:], uint64(now))
			c.hub.broadcast <- OutgoingMessage{messageType: websocket.BinaryMessage, data: bcast}

			// Send an acknowledgment back (2 bytes).
			ack := []byte{MsgTypeUpdateAck, 1}
			c.send <- OutgoingMessage{messageType: websocket.BinaryMessage, data: ack}

		case MsgTypeRequest:
			// (Not used in this example, but here for completeness.)
			if len(data) < 2 {
				log.Println("Invalid request message length")
				continue
			}
			panel := int(data[1])
			if panel < 0 || panel >= numPanels {
				log.Println("Invalid panel number in request")
				continue
			}
			buf := make([]byte, panelSize*panelSize*3)
			idx := 0
			panelMutex.RLock()
			for y := 0; y < panelSize; y++ {
				for x := 0; x < panelSize; x++ {
					p := panels[panel][y][x]
					buf[idx] = p.R
					buf[idx+1] = p.G
					buf[idx+2] = p.B
					idx += 3
				}
			}
			panelMutex.RUnlock()
			c.send <- OutgoingMessage{messageType: websocket.BinaryMessage, data: buf}

		default:
			log.Println("Unknown message type:", data[0])
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case m, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(m.messageType, m.data); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func main() {
	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})
	// Serve static files (including index.html) from the current directory.
	fs := http.FileServer(http.Dir("./dist"))
	http.Handle("/", fs)

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
