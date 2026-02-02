package relay

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const (
	RelayProtocol    = protocol.ID("/p2papp/relay/1.0.0")
	RelayGetProtocol = protocol.ID("/p2papp/relay-get/1.0.0")

	DefaultCacheSize = 100 * 1024 * 1024 // 100 MB
	DefaultCacheTTL  = 24 * time.Hour
)

// RelayMode defines the operating mode
type RelayMode string

const (
	ModeRelay  RelayMode = "relay"  // Act as relay server
	ModeClient RelayMode = "client" // Normal client
)

// RelayMessage represents a message being relayed
type RelayMessage struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"` // "forward", "cache", "fetch"
	From      peer.ID         `json:"from"`
	To        peer.ID         `json:"to,omitempty"`
	Key       string          `json:"key,omitempty"` // For cached content
	Data      json.RawMessage `json:"data,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// RelayConfig configures the relay node
type RelayConfig struct {
	Mode          RelayMode
	CacheSize     int64
	CacheTTL      time.Duration
	LimiterConfig LimiterConfig
	DataDir       string
}

// DefaultRelayConfig returns default configuration
func DefaultRelayConfig() RelayConfig {
	return RelayConfig{
		Mode:          ModeClient,
		CacheSize:     DefaultCacheSize,
		CacheTTL:      DefaultCacheTTL,
		LimiterConfig: DefaultLimiterConfig(),
	}
}

// RelayNode implements a relay server for the P2P network
type RelayNode struct {
	host    host.Host
	config  RelayConfig
	router  *ConsistentHashRouter
	cache   *LRUCache
	limiter *RateLimiter
	queue   *MessageQueue

	// Peer tracking
	onlinePeers map[peer.ID]time.Time

	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
}

// NewRelayNode creates a new relay node
func NewRelayNode(h host.Host, config RelayConfig) (*RelayNode, error) {
	ctx, cancel := context.WithCancel(context.Background())

	node := &RelayNode{
		host:        h,
		config:      config,
		router:      NewConsistentHashRouter(100), // 100 virtual nodes per physical
		cache:       NewLRUCache(config.CacheSize),
		limiter:     NewRateLimiter(config.LimiterConfig),
		onlinePeers: make(map[peer.ID]time.Time),
		ctx:         ctx,
		cancel:      cancel,
	}

	// Initialize message queue if data dir is set
	if config.DataDir != "" {
		queue, err := NewMessageQueue(config.DataDir)
		if err != nil {
			cancel()
			return nil, fmt.Errorf("failed to create message queue: %w", err)
		}
		node.queue = queue
	}

	return node, nil
}

// Start starts the relay node
func (r *RelayNode) Start() error {
	// Register protocol handlers
	r.host.SetStreamHandler(RelayProtocol, r.handleRelayStream)
	r.host.SetStreamHandler(RelayGetProtocol, r.handleGetStream)

	// Start background tasks
	go r.cleanupLoop()
	go r.deliveryLoop()

	// Add self to router if in relay mode
	if r.config.Mode == ModeRelay {
		r.router.AddNode(NodeInfo{
			ID:       r.host.ID().String(),
			Address:  r.host.Addrs()[0].String(),
			Weight:   1,
			Healthy:  true,
			Capacity: r.config.CacheSize,
		})
	}

	fmt.Printf("Relay node started in %s mode\n", r.config.Mode)
	return nil
}

// handleRelayStream handles incoming relay requests
func (r *RelayNode) handleRelayStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()

	// Rate limiting
	if !r.limiter.AllowRequest(peerID.String()) {
		return
	}

	decoder := json.NewDecoder(stream)
	var msg RelayMessage
	if err := decoder.Decode(&msg); err != nil {
		return
	}

	msg.From = peerID
	msg.Timestamp = time.Now()

	// Track peer as online
	r.mu.Lock()
	r.onlinePeers[peerID] = time.Now()
	r.mu.Unlock()

	switch msg.Type {
	case "forward":
		r.handleForward(msg)
	case "cache":
		r.handleCache(msg)
	case "announce":
		r.handleAnnounce(peerID)
	}
}

// handleGetStream handles content fetch requests
func (r *RelayNode) handleGetStream(stream network.Stream) {
	defer stream.Close()

	peerID := stream.Conn().RemotePeer()

	if !r.limiter.AllowRequest(peerID.String()) {
		return
	}

	decoder := json.NewDecoder(stream)
	var msg RelayMessage
	if err := decoder.Decode(&msg); err != nil {
		return
	}

	// Fetch from cache
	if data, ok := r.cache.Get(msg.Key); ok {
		encoder := json.NewEncoder(stream)
		encoder.Encode(RelayMessage{
			Type: "response",
			Key:  msg.Key,
			Data: data,
		})
	}
}

// handleForward forwards a message to target peer
func (r *RelayNode) handleForward(msg RelayMessage) {
	// Check if target is online
	r.mu.RLock()
	lastSeen, online := r.onlinePeers[msg.To]
	r.mu.RUnlock()

	if online && time.Since(lastSeen) < 5*time.Minute {
		// Try direct delivery
		if err := r.deliverToPeer(msg); err == nil {
			return
		}
	}

	// Queue for later delivery
	if r.queue != nil {
		r.queue.Enqueue(QueuedMessage{
			ID:      msg.ID,
			From:    msg.From,
			To:      msg.To,
			Type:    "forward",
			Content: msg.Data,
		})
	}
}

// handleCache caches content
func (r *RelayNode) handleCache(msg RelayMessage) {
	if msg.Key != "" && len(msg.Data) > 0 {
		r.cache.PutWithTTL(msg.Key, msg.Data, r.config.CacheTTL)
	}
}

// handleAnnounce handles peer announcements
func (r *RelayNode) handleAnnounce(peerID peer.ID) {
	r.mu.Lock()
	r.onlinePeers[peerID] = time.Now()
	r.mu.Unlock()

	// Deliver any pending messages
	if r.queue != nil {
		messages := r.queue.Peek(peerID)
		for _, msg := range messages {
			if err := r.deliverQueuedMessage(msg); err == nil {
				r.queue.RemoveMessage(peerID, msg.ID)
			}
		}
	}
}

// deliverToPeer delivers a message to a peer
func (r *RelayNode) deliverToPeer(msg RelayMessage) error {
	stream, err := r.host.NewStream(r.ctx, msg.To, RelayProtocol)
	if err != nil {
		return err
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	return encoder.Encode(msg)
}

// deliverQueuedMessage delivers a queued message
func (r *RelayNode) deliverQueuedMessage(qm QueuedMessage) error {
	msg := RelayMessage{
		ID:   qm.ID,
		Type: "forward",
		From: qm.From,
		To:   qm.To,
	}

	if content, ok := qm.Content.([]byte); ok {
		msg.Data = content
	} else {
		data, _ := json.Marshal(qm.Content)
		msg.Data = data
	}

	return r.deliverToPeer(msg)
}

// Forward sends a message through the relay network
func (r *RelayNode) Forward(to peer.ID, data []byte) error {
	msg := RelayMessage{
		ID:   fmt.Sprintf("%d", time.Now().UnixNano()),
		Type: "forward",
		From: r.host.ID(),
		To:   to,
		Data: data,
	}

	// Find relay node for this peer
	node, ok := r.router.GetNode(to.String())
	if !ok {
		return fmt.Errorf("no relay found for peer")
	}

	relayID, err := peer.Decode(node.ID)
	if err != nil {
		return err
	}

	stream, err := r.host.NewStream(r.ctx, relayID, RelayProtocol)
	if err != nil {
		return err
	}
	defer stream.Close()

	encoder := json.NewEncoder(stream)
	return encoder.Encode(msg)
}

// Cache stores content in the relay network
func (r *RelayNode) Cache(key string, data []byte) error {
	// Cache locally
	r.cache.PutWithTTL(key, data, r.config.CacheTTL)

	// Also cache on responsible relay nodes
	nodes := r.router.GetNodes(key, 3) // 3 replicas
	for _, node := range nodes {
		if node.ID == r.host.ID().String() {
			continue
		}

		relayID, err := peer.Decode(node.ID)
		if err != nil {
			continue
		}

		go func(peerID peer.ID) {
			stream, err := r.host.NewStream(r.ctx, peerID, RelayProtocol)
			if err != nil {
				return
			}
			defer stream.Close()

			encoder := json.NewEncoder(stream)
			encoder.Encode(RelayMessage{
				Type: "cache",
				Key:  key,
				Data: data,
			})
		}(relayID)
	}

	return nil
}

// Fetch retrieves content from the relay network
func (r *RelayNode) Fetch(key string) ([]byte, error) {
	// Check local cache first
	if data, ok := r.cache.Get(key); ok {
		return data, nil
	}

	// Try relay nodes
	nodes := r.router.GetNodes(key, 3)
	for _, node := range nodes {
		relayID, err := peer.Decode(node.ID)
		if err != nil {
			continue
		}

		stream, err := r.host.NewStream(r.ctx, relayID, RelayGetProtocol)
		if err != nil {
			continue
		}

		encoder := json.NewEncoder(stream)
		encoder.Encode(RelayMessage{Type: "fetch", Key: key})

		decoder := json.NewDecoder(stream)
		var resp RelayMessage
		if err := decoder.Decode(&resp); err == nil && len(resp.Data) > 0 {
			stream.Close()
			// Cache locally
			r.cache.PutWithTTL(key, resp.Data, r.config.CacheTTL)
			return resp.Data, nil
		}
		stream.Close()
	}

	return nil, fmt.Errorf("content not found: %s", key)
}

// cleanupLoop runs periodic cleanup
func (r *RelayNode) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			r.cache.CleanupExpired()
			r.limiter.Cleanup(24 * time.Hour)
			if r.queue != nil {
				r.queue.Cleanup()
			}

			// Clean stale peers
			r.mu.Lock()
			for peerID, lastSeen := range r.onlinePeers {
				if time.Since(lastSeen) > 1*time.Hour {
					delete(r.onlinePeers, peerID)
				}
			}
			r.mu.Unlock()

		case <-r.ctx.Done():
			return
		}
	}
}

// deliveryLoop attempts to deliver queued messages
func (r *RelayNode) deliveryLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if r.queue == nil {
				continue
			}

			for _, peerID := range r.queue.PeersWithPending() {
				r.mu.RLock()
				lastSeen, online := r.onlinePeers[peerID]
				r.mu.RUnlock()

				if !online || time.Since(lastSeen) > 5*time.Minute {
					continue
				}

				messages := r.queue.Peek(peerID)
				for _, msg := range messages {
					if err := r.deliverQueuedMessage(msg); err == nil {
						r.queue.RemoveMessage(peerID, msg.ID)
					} else {
						r.queue.MarkAttempt(peerID, msg.ID)
					}
				}
			}

		case <-r.ctx.Done():
			return
		}
	}
}

// AddRelayNode adds a relay node to the router
func (r *RelayNode) AddRelayNode(info NodeInfo) {
	r.router.AddNode(info)
}

// RemoveRelayNode removes a relay node from the router
func (r *RelayNode) RemoveRelayNode(nodeID string) {
	r.router.RemoveNode(nodeID)
}

// Stats returns relay node statistics
func (r *RelayNode) Stats() RelayStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := RelayStats{
		CacheStats:  r.cache.Stats(),
		OnlinePeers: len(r.onlinePeers),
		RelayNodes:  r.router.NodeCount(),
	}

	if r.queue != nil {
		stats.QueueStats = r.queue.Stats()
	}

	return stats
}

// RelayStats contains relay node statistics
type RelayStats struct {
	CacheStats  CacheStats
	QueueStats  QueueStats
	OnlinePeers int
	RelayNodes  int
}

// Close shuts down the relay node
func (r *RelayNode) Close() error {
	r.cancel()
	r.host.RemoveStreamHandler(RelayProtocol)
	r.host.RemoveStreamHandler(RelayGetProtocol)
	return nil
}
