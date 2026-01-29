package chat

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"

	"p2papp/internal/core"
)

const (
	DMProtocol = protocol.ID("/p2papp/dm/1.0.0")
)

// DirectMessage represents a direct P2P message
type DirectMessage struct {
	From      peer.ID   `json:"from"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// DirectMessageHandler handles incoming direct messages
type DirectMessageHandler struct {
	host    *core.P2PHost
	msgChan chan DirectMessage
}

// NewDirectMessage creates a new direct message handler
func NewDirectMessage(host *core.P2PHost) *DirectMessageHandler {
	dm := &DirectMessageHandler{
		host:    host,
		msgChan: make(chan DirectMessage, 50),
	}
	return dm
}

// RegisterHandler registers the DM protocol handler
func (dm *DirectMessageHandler) RegisterHandler() {
	dm.host.Host.SetStreamHandler(DMProtocol, dm.handleStream)
}

// handleStream handles incoming DM streams
func (dm *DirectMessageHandler) handleStream(stream network.Stream) {
	defer stream.Close()

	reader := bufio.NewReader(stream)
	data, err := reader.ReadBytes('\n')
	if err != nil && err != io.EOF {
		fmt.Printf("Error reading DM: %v\n", err)
		return
	}

	var msg DirectMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		fmt.Printf("Error decoding DM: %v\n", err)
		return
	}

	msg.From = stream.Conn().RemotePeer()

	select {
	case dm.msgChan <- msg:
	default:
		fmt.Println("DM channel full, dropping message")
	}
}

// Send sends a direct message to a peer
func (dm *DirectMessageHandler) Send(ctx context.Context, peerID peer.ID, content string) error {
	// Open stream to peer
	stream, err := dm.host.Host.NewStream(ctx, peerID, DMProtocol)
	if err != nil {
		return fmt.Errorf("failed to open stream to %s: %w", peerID.String()[:8], err)
	}
	defer stream.Close()

	msg := DirectMessage{
		From:      dm.host.ID(),
		Content:   content,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	// Add newline delimiter
	data = append(data, '\n')

	_, err = stream.Write(data)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}

	return nil
}

// Subscribe returns a channel for receiving direct messages
func (dm *DirectMessageHandler) Subscribe() <-chan DirectMessage {
	return dm.msgChan
}

// Close closes the DM handler
func (dm *DirectMessageHandler) Close() {
	dm.host.Host.RemoveStreamHandler(DMProtocol)
	close(dm.msgChan)
}
