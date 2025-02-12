package main

import (
	"encoding/binary"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	panelSize  = 128
	numPanels  = 840
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
	maxMsgSize = 512

	// Message type constants:
	MsgTypeUpdate      = 1 // Client → Server: 5 bytes: type, panel (2), x, y.
	MsgTypeRequest     = 2 // Client → Server: 3 bytes: type, panel (2)
	MsgTypeUpdateAck   = 3 // Server → Client: 2 bytes: type, result.
	MsgTypeBroadcast   = 4 // Server → Client: 16 bytes: type, panel (2), x, y, r, g, b, timestamp (8 bytes).
	MsgTypePanelSync   = 5 // Server → Client: 3-byte header (type, panel (2)) + 128×128×3 bytes of raw RGB.
	MsgTypeAssignColor = 6 // Server → Client: 4 bytes: type, r, g, b.
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

// Client represents a connected websocket client.
type Client struct {
	hub   *Hub
	conn  *websocket.Conn
	send  chan OutgoingMessage
	color struct {
		R, G, B byte
	}
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

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	// In production, check the origin as needed.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// serveWs upgrades the HTTP connection to a websocket, assigns a random color,
// sends an assign-color message to the client, and registers the client.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	log.Println("Client connected")
	client := &Client{
		hub:  hub,
		conn: conn,
		send: make(chan OutgoingMessage, 256),
	}
	// Assign a random color to this client.
	client.color.R = byte(rand.Intn(256))
	client.color.G = byte(rand.Intn(256))
	client.color.B = byte(rand.Intn(256))

	// Send an assign-color message (4 bytes: type, r, g, b).
	assignMsg := make([]byte, 4)
	assignMsg[0] = MsgTypeAssignColor
	assignMsg[1] = client.color.R
	assignMsg[2] = client.color.G
	assignMsg[3] = client.color.B
	client.send <- OutgoingMessage{messageType: websocket.BinaryMessage, data: assignMsg}

	hub.register <- client

	// (Optionally, you could send an initial full-panel sync here.)
	// syncPanels(client)

	go client.writePump()
	go client.readPump()
}

// syncPanels sends a full-panel sync for every panel.
// Message layout:
//   Byte 0: MsgTypePanelSync (5)
//   Bytes 1-2: Panel number (uint16)
//   Bytes 3...: 128×128×3 bytes of pixel data.
func syncPanels(c *Client) {
	for panel := 0; panel < numPanels; panel++ {
		buf := make([]byte, 3+panelSize*panelSize*3)
		buf[0] = MsgTypePanelSync
		binary.BigEndian.PutUint16(buf[1:3], uint16(panel))
		log.Printf("Sending full sync for panel %d to client\n", panel)
		idx := 3
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
		// Expect binary messages.
		if msgType != websocket.BinaryMessage {
			log.Println("Ignoring non-binary message")
			continue
		}
		if len(data) < 1 {
			continue
		}
		switch data[0] {
		case MsgTypeUpdate:
			// Expect 5 bytes: type, panel (2), x, y.
			if len(data) < 5 {
				log.Println("Invalid update message length")
				continue
			}
			panel := int(binary.BigEndian.Uint16(data[1:3]))
			x := int(data[3])
			y := int(data[4])
			// Use the client’s assigned color.
			rVal := c.color.R
			gVal := c.color.G
			bVal := c.color.B

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
			// Broadcast message (16 bytes): type, panel (2), x, y, r, g, b, timestamp (8 bytes).
			bcast := make([]byte, 16)
			bcast[0] = MsgTypeBroadcast
			binary.BigEndian.PutUint16(bcast[1:3], uint16(panel))
			bcast[3] = byte(x)
			bcast[4] = byte(y)
			bcast[5] = rVal
			bcast[6] = gVal
			bcast[7] = bVal
			binary.BigEndian.PutUint64(bcast[8:], uint64(now))
			c.hub.broadcast <- OutgoingMessage{messageType: websocket.BinaryMessage, data: bcast}

			// Send an acknowledgment (2 bytes).
			ack := []byte{MsgTypeUpdateAck, 1}
			c.send <- OutgoingMessage{messageType: websocket.BinaryMessage, data: ack}

		case MsgTypeRequest:
			// Expect 3 bytes: type, panel (2)
			if len(data) < 3 {
				log.Println("Invalid request message length")
				continue
			}
			panel := int(binary.BigEndian.Uint16(data[1:3]))
			if panel < 0 || panel >= numPanels {
				log.Println("Invalid panel number in request")
				continue
			}
			log.Printf("Panel sync requested for panel %d\n", panel)
			// Allocate buffer with header: 3 bytes header + pixel data.
			buf := make([]byte, 3+panelSize*panelSize*3)
			buf[0] = MsgTypePanelSync
			binary.BigEndian.PutUint16(buf[1:3], uint16(panel))
			idx := 3
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
				log.Println("Write error:", err)
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
	// Seed the random number generator.
	rand.Seed(time.Now().UnixNano())

	hub := newHub()
	go hub.run()

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWs(hub, w, r)
	})
	// Serve static files (including index.html) from "./dist".
	fs := http.FileServer(http.Dir("./dist"))
	http.Handle("/", fs)

	log.Println("Server started on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
