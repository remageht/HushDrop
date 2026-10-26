package signaling

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

func netPipe(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	c1, c2 := net.Pipe()
	return c1, c2
}

func bufioReadWriter(c net.Conn) *bufio.ReadWriter {
	r := bufio.NewReader(c)
	w := bufio.NewWriter(c)
	return bufio.NewReadWriter(r, w)
}

func TestSignaling_RoomHub(t *testing.T) {
	hub := NewHub()

	// Client 1 dummy pipe
	c1PipeServer, c1PipeClient := netPipe(t)
	defer c1PipeServer.Close()
	defer c1PipeClient.Close()

	// Drain client 1 messages
	go io.Copy(io.Discard, c1PipeClient)

	conn1 := &Conn{
		netConn: c1PipeServer,
		bufrw:   bufioReadWriter(c1PipeServer),
	}

	client1 := &Client{
		ID:     "peer-1",
		RoomID: "room-abc",
		Name:   "Device A",
		Conn:   conn1,
		Hub:    hub,
	}

	if err := hub.RegisterClient(client1); err != nil {
		t.Fatalf("RegisterClient 1 failed: %v", err)
	}

	// Client 2 dummy pipe
	c2PipeServer, c2PipeClient := netPipe(t)
	defer c2PipeServer.Close()
	defer c2PipeClient.Close()

	// Drain client 2 messages
	go io.Copy(io.Discard, c2PipeClient)

	conn2 := &Conn{
		netConn: c2PipeServer,
		bufrw:   bufioReadWriter(c2PipeServer),
	}

	client2 := &Client{
		ID:     "peer-2",
		RoomID: "room-abc",
		Name:   "Device B",
		Conn:   conn2,
		Hub:    hub,
	}

	if err := hub.RegisterClient(client2); err != nil {
		t.Fatalf("RegisterClient 2 failed: %v", err)
	}

	// Test RouteMessage from client1 to client2
	offerPayload := json.RawMessage(`{"sdp":"v=0..."}`)
	msg := &Message{
		Type:     "offer",
		TargetID: "peer-2",
		Payload:  offerPayload,
	}

	hub.RouteMessage(client1, msg)

	// Unregister client1
	hub.UnregisterClient(client1)

	hub.mu.RLock()
	room := hub.rooms["room-abc"]
	if len(room) != 1 {
		t.Errorf("Expected 1 peer remaining in room, got %d", len(room))
	}
	hub.mu.RUnlock()

	// Unregister client2 -> room should be deleted
	hub.UnregisterClient(client2)

	hub.mu.RLock()
	_, exists := hub.rooms["room-abc"]
	if exists {
		t.Errorf("Expected room to be pruned after all peers left")
	}
	hub.mu.RUnlock()
}

func TestSignaling_HandshakeValidation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := Upgrade(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}))
	defer ts.Close()

	// Send normal HTTP request without WebSocket headers
	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("http.Get failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for non-websocket request, got %d", resp.StatusCode)
	}
}
