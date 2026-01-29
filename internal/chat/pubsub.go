package chat

import (
	"context"

	pubsub "github.com/libp2p/go-libp2p-pubsub"

	"github.com/p2papp/internal/core"
)

// PubSubManager manages GossipSub topics
type PubSubManager struct {
	ps     *pubsub.PubSub
	topics map[string]*pubsub.Topic
	host   *core.P2PHost
}

// NewPubSubManager creates a new PubSub manager
func NewPubSubManager(host *core.P2PHost) *PubSubManager {
	return &PubSubManager{
		ps:     host.PubSub,
		topics: make(map[string]*pubsub.Topic),
		host:   host,
	}
}

// JoinTopic joins or creates a PubSub topic
func (m *PubSubManager) JoinTopic(name string) (*pubsub.Topic, error) {
	if topic, exists := m.topics[name]; exists {
		return topic, nil
	}

	topic, err := m.ps.Join(name)
	if err != nil {
		return nil, err
	}

	m.topics[name] = topic
	return topic, nil
}

// LeaveTopic leaves a PubSub topic
func (m *PubSubManager) LeaveTopic(name string) error {
	topic, exists := m.topics[name]
	if !exists {
		return nil
	}

	if err := topic.Close(); err != nil {
		return err
	}

	delete(m.topics, name)
	return nil
}

// Publish publishes data to a topic
func (m *PubSubManager) Publish(ctx context.Context, topicName string, data []byte) error {
	topic, exists := m.topics[topicName]
	if !exists {
		var err error
		topic, err = m.JoinTopic(topicName)
		if err != nil {
			return err
		}
	}

	return topic.Publish(ctx, data)
}

// Subscribe subscribes to a topic and returns a subscription
func (m *PubSubManager) Subscribe(topicName string) (*pubsub.Subscription, error) {
	topic, exists := m.topics[topicName]
	if !exists {
		var err error
		topic, err = m.JoinTopic(topicName)
		if err != nil {
			return nil, err
		}
	}

	return topic.Subscribe()
}

// Close closes all topics
func (m *PubSubManager) Close() {
	for _, topic := range m.topics {
		topic.Close()
	}
	m.topics = make(map[string]*pubsub.Topic)
}
