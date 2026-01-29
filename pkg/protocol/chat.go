package protocol

import (
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

// ChatMessage is the wire format for chat messages
type ChatMessage struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	From      peer.ID   `json:"from"`
	RoomID    string    `json:"room_id,omitempty"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// ChatProtocol constants
const (
	ChatProtocolVersion = 1
	ChatTopic           = "/p2papp/chat/"
	DMProtocol          = "/p2papp/dm/1.0.0"
)

// Message types
const (
	TypeText   = "text"
	TypeJoin   = "join"
	TypeLeave  = "leave"
	TypeSystem = "system"
)
