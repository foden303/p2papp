package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/p2papp/internal/core"
)

// Message represents a chat message
type Message struct {
	ID        string    `json:"id"`
	From      peer.ID   `json:"from"`
	Content   string    `json:"content"`
	RoomID    string    `json:"room_id"`
	Timestamp time.Time `json:"timestamp"`
	Type      MsgType   `json:"type"`
}

// MsgType represents the type of message
type MsgType string

const (
	MsgTypeText   MsgType = "text"
	MsgTypeJoin   MsgType = "join"
	MsgTypeLeave  MsgType = "leave"
	MsgTypeSystem MsgType = "system"
)

// ChatService provides chat operations
type ChatService interface {
	JoinRoom(roomID string) error
	LeaveRoom(roomID string) error
	SendMessage(roomID, content string) error
	SendDM(peerID peer.ID, content string) error
	Subscribe() <-chan Message
	ListRooms() []string
	Close() error
}

type chatService struct {
	host    *core.P2PHost
	rooms   map[string]*Room
	msgChan chan Message
	mu      sync.RWMutex
	ctx     context.Context
	cancel  context.CancelFunc
}

// NewChatService creates a new chat service
func NewChatService(ctx context.Context, host *core.P2PHost) (ChatService, error) {
	ctx, cancel := context.WithCancel(ctx)

	return &chatService{
		host:    host,
		rooms:   make(map[string]*Room),
		msgChan: make(chan Message, 100),
		ctx:     ctx,
		cancel:  cancel,
	}, nil
}

// JoinRoom joins a chat room by creating or subscribing to a PubSub topic
func (s *chatService) JoinRoom(roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.rooms[roomID]; exists {
		return fmt.Errorf("already joined room: %s", roomID)
	}

	room, err := NewRoom(s.ctx, s.host, roomID, s.msgChan)
	if err != nil {
		return fmt.Errorf("failed to join room: %w", err)
	}

	s.rooms[roomID] = room

	// Send join message
	joinMsg := Message{
		ID:        generateMsgID(),
		From:      s.host.ID(),
		Content:   fmt.Sprintf("%s joined the room", s.host.ID().String()[:8]),
		RoomID:    roomID,
		Timestamp: time.Now(),
		Type:      MsgTypeJoin,
	}
	room.Publish(joinMsg)

	return nil
}

// LeaveRoom leaves a chat room
func (s *chatService) LeaveRoom(roomID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	room, exists := s.rooms[roomID]
	if !exists {
		return fmt.Errorf("not in room: %s", roomID)
	}

	// Send leave message
	leaveMsg := Message{
		ID:        generateMsgID(),
		From:      s.host.ID(),
		Content:   fmt.Sprintf("%s left the room", s.host.ID().String()[:8]),
		RoomID:    roomID,
		Timestamp: time.Now(),
		Type:      MsgTypeLeave,
	}
	room.Publish(leaveMsg)

	room.Close()
	delete(s.rooms, roomID)
	return nil
}

// SendMessage sends a text message to a room
func (s *chatService) SendMessage(roomID, content string) error {
	s.mu.RLock()
	room, exists := s.rooms[roomID]
	s.mu.RUnlock()

	if !exists {
		return fmt.Errorf("not in room: %s", roomID)
	}

	msg := Message{
		ID:        generateMsgID(),
		From:      s.host.ID(),
		Content:   content,
		RoomID:    roomID,
		Timestamp: time.Now(),
		Type:      MsgTypeText,
	}

	return room.Publish(msg)
}

// SendDM sends a direct message to a specific peer
func (s *chatService) SendDM(peerID peer.ID, content string) error {
	dm := NewDirectMessage(s.host)
	return dm.Send(s.ctx, peerID, content)
}

// Subscribe returns a channel for receiving messages
func (s *chatService) Subscribe() <-chan Message {
	return s.msgChan
}

// ListRooms returns a list of joined rooms
func (s *chatService) ListRooms() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rooms := make([]string, 0, len(s.rooms))
	for id := range s.rooms {
		rooms = append(rooms, id)
	}
	return rooms
}

// Close shuts down the chat service
func (s *chatService) Close() error {
	s.cancel()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, room := range s.rooms {
		room.Close()
	}
	close(s.msgChan)
	return nil
}

func generateMsgID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// EncodeMessage serializes a message to JSON
func EncodeMessage(msg Message) ([]byte, error) {
	return json.Marshal(msg)
}

// DecodeMessage deserializes a message from JSON
func DecodeMessage(data []byte) (*Message, error) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}
