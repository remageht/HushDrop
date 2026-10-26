package signaling

import (
	"encoding/json"
	"errors"
	"log"
	"sync"
	"time"
)

const (
	maxPeersPerRoom = 8
	pingInterval    = 25 * time.Second
)

// Message defines the structure of a signaling frame
type Message struct {
	Type     string          `json:"type"`               // "join", "joined", "peer-joined", "peer-left", "offer", "answer", "candidate", "error", "peers"
	RoomID   string          `json:"roomId,omitempty"`
	PeerID   string          `json:"peerId,omitempty"`
	TargetID string          `json:"targetId,omitempty"` // For peer-to-peer routed messages (offer, answer, candidate)
	Name     string          `json:"name,omitempty"`     // Device / User display name
	Payload  json.RawMessage `json:"payload,omitempty"`  // Arbitrary SDP or ICE object
	Peers    []string        `json:"peers,omitempty"`    // List of peer IDs in room
	Message  string          `json:"message,omitempty"`  // Error or notification text
}

// Client represents a connected WebRTC peer
type Client struct {
	ID     string
	RoomID string
	Name   string
	Conn   *Conn
	Hub    *Hub
	sendMu sync.Mutex
}

// Send sends a JSON-encoded message to the client
func (c *Client) Send(msg *Message) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.Conn.WriteTextMessage(string(data))
}

// Hub manages WebRTC rooms and signaling message routing
type Hub struct {
	mu    sync.RWMutex
	rooms map[string]map[string]*Client // roomID -> peerID -> *Client
}

// NewHub creates a new signaling Hub
func NewHub() *Hub {
	return &Hub{
		rooms: make(map[string]map[string]*Client),
	}
}

// RegisterClient adds a client to a room and broadcasts presence
func (h *Hub) RegisterClient(c *Client) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, exists := h.rooms[c.RoomID]
	if !exists {
		room = make(map[string]*Client)
		h.rooms[c.RoomID] = room
	}

	if len(room) >= maxPeersPerRoom {
		return errors.New("room is full (max 8 peers)")
	}

	// Collect existing peers
	var existingPeers []string
	for peerID, peer := range room {
		existingPeers = append(existingPeers, peerID)
		// Notify existing peer about the new arrival
		_ = peer.Send(&Message{
			Type:   "peer-joined",
			RoomID: c.RoomID,
			PeerID: c.ID,
			Name:   c.Name,
		})
	}

	room[c.ID] = c

	// Send confirmation to the joining client with existing peers list
	_ = c.Send(&Message{
		Type:   "joined",
		RoomID: c.RoomID,
		PeerID: c.ID,
		Peers:  existingPeers,
	})

	log.Printf("[SIGNALING] Peer %s joined room %s (total in room: %d)", c.ID, c.RoomID, len(room))
	return nil
}

// UnregisterClient removes a client and notifies room members
func (h *Hub) UnregisterClient(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()

	room, exists := h.rooms[c.RoomID]
	if !exists {
		return
	}

	delete(room, c.ID)
	log.Printf("[SIGNALING] Peer %s left room %s (remaining: %d)", c.ID, c.RoomID, len(room))

	for _, peer := range room {
		_ = peer.Send(&Message{
			Type:   "peer-left",
			RoomID: c.RoomID,
			PeerID: c.ID,
		})
	}

	if len(room) == 0 {
		delete(h.rooms, c.RoomID)
		log.Printf("[SIGNALING] Room %s closed (empty)", c.RoomID)
	}
}

// RouteMessage routes a direct signaling message (offer, answer, candidate) to TargetID
func (h *Hub) RouteMessage(from *Client, msg *Message) {
	if msg.TargetID == "" {
		_ = from.Send(&Message{
			Type:    "error",
			Message: "missing targetId for direct signaling message",
		})
		return
	}

	h.mu.RLock()
	room, exists := h.rooms[from.RoomID]
	var target *Client
	if exists {
		target = room[msg.TargetID]
	}
	h.mu.RUnlock()

	if target == nil {
		_ = from.Send(&Message{
			Type:     "error",
			TargetID: msg.TargetID,
			Message:  "target peer not found or left the room",
		})
		return
	}

	// Stamp sender identity
	msg.PeerID = from.ID
	msg.RoomID = from.RoomID
	if err := target.Send(msg); err != nil {
		log.Printf("[SIGNALING] Failed to forward %s from %s to %s: %v", msg.Type, from.ID, msg.TargetID, err)
	}
}

// HandleClient reads incoming signaling messages from a connected client
func (h *Hub) HandleClient(conn *Conn, defaultRoomID, defaultPeerID, defaultName string) {
	client := &Client{
		ID:     defaultPeerID,
		RoomID: defaultRoomID,
		Name:   defaultName,
		Conn:   conn,
		Hub:    h,
	}

	if err := h.RegisterClient(client); err != nil {
		_ = client.Send(&Message{
			Type:    "error",
			Message: err.Error(),
		})
		_ = conn.Close()
		return
	}
	defer func() {
		h.UnregisterClient(client)
		_ = conn.Close()
	}()

	// Ping loop ticker
	pingTicker := time.NewTicker(pingInterval)
	defer pingTicker.Stop()

	go func() {
		for range pingTicker.C {
			if err := conn.Ping(); err != nil {
				return
			}
		}
	}()

	for {
		raw, err := conn.ReadTextMessage()
		if err != nil {
			break
		}

		var msg Message
		if err := json.Unmarshal([]byte(raw), &msg); err != nil {
			_ = client.Send(&Message{
				Type:    "error",
				Message: "invalid JSON signaling message",
			})
			continue
		}

		switch msg.Type {
		case "offer", "answer", "candidate":
			h.RouteMessage(client, &msg)
		case "ping":
			_ = client.Send(&Message{Type: "pong"})
		case "leave":
			return
		default:
			// Unknown message type
			_ = client.Send(&Message{
				Type:    "error",
				Message: "unsupported message type: " + msg.Type,
			})
		}
	}
}
