package chat

import (
	"context"
	"fmt"
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"

	"p2papp/internal/core"
)

// Room represents a chat room using PubSub
type Room struct {
	ID           string
	topic        *pubsub.Topic
	subscription *pubsub.Subscription
	host         *core.P2PHost
	msgChan      chan<- Message
	ctx          context.Context
	cancel       context.CancelFunc
	mu           sync.RWMutex
	members      map[peer.ID]bool
}

// NewRoom creates and joins a new chat room
func NewRoom(ctx context.Context, host *core.P2PHost, roomID string, msgChan chan<- Message) (*Room, error) {
	topicName := fmt.Sprintf("/p2papp/chat/%s", roomID)

	topic, err := host.PubSub.Join(topicName)
	if err != nil {
		return nil, fmt.Errorf("failed to join topic: %w", err)
	}

	sub, err := topic.Subscribe()
	if err != nil {
		topic.Close()
		return nil, fmt.Errorf("failed to subscribe to topic: %w", err)
	}

	ctx, cancel := context.WithCancel(ctx)

	room := &Room{
		ID:           roomID,
		topic:        topic,
		subscription: sub,
		host:         host,
		msgChan:      msgChan,
		ctx:          ctx,
		cancel:       cancel,
		members:      make(map[peer.ID]bool),
	}

	// Start listening for messages
	go room.readLoop()
	// Start tracking peers
	go room.peerLoop()

	return room, nil
}

// readLoop continuously reads messages from the subscription
func (r *Room) readLoop() {
	for {
		msg, err := r.subscription.Next(r.ctx)
		if err != nil {
			if r.ctx.Err() != nil {
				return // Context cancelled
			}
			fmt.Printf("Error reading message: %v\n", err)
			continue
		}

		// Ignore messages from self
		if msg.ReceivedFrom == r.host.ID() {
			continue
		}

		chatMsg, err := DecodeMessage(msg.Data)
		if err != nil {
			fmt.Printf("Error decoding message: %v\n", err)
			continue
		}

		// Send to message channel
		select {
		case r.msgChan <- *chatMsg:
		case <-r.ctx.Done():
			return
		}
	}
}

// peerLoop tracks peers in the room
func (r *Room) peerLoop() {
	handler, err := r.topic.EventHandler()
	if err != nil {
		fmt.Printf("Failed to get event handler: %v\n", err)
		return
	}
	defer handler.Cancel()

	for {
		evt, err := handler.NextPeerEvent(r.ctx)
		if err != nil {
			if r.ctx.Err() != nil {
				return
			}
			continue
		}

		r.mu.Lock()
		switch evt.Type {
		case pubsub.PeerJoin:
			r.members[evt.Peer] = true
			fmt.Printf("Peer %s joined room %s\n", evt.Peer.String()[:8], r.ID)
		case pubsub.PeerLeave:
			delete(r.members, evt.Peer)
			fmt.Printf("Peer %s left room %s\n", evt.Peer.String()[:8], r.ID)
		}
		r.mu.Unlock()
	}
}

// Publish sends a message to the room
func (r *Room) Publish(msg Message) error {
	data, err := EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("failed to encode message: %w", err)
	}

	return r.topic.Publish(r.ctx, data)
}

// Members returns the list of peers in the room
func (r *Room) Members() []peer.ID {
	r.mu.RLock()
	defer r.mu.RUnlock()

	members := make([]peer.ID, 0, len(r.members))
	for p := range r.members {
		members = append(members, p)
	}
	return members
}

// Close leaves the room and cleans up resources
func (r *Room) Close() {
	r.cancel()
	r.subscription.Cancel()
	r.topic.Close()
}
