package relay

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	QueueDir        = "queue"
	DefaultTTL      = 7 * 24 * time.Hour // 7 days
	CleanupInterval = 1 * time.Hour
)

// QueuedMessage represents a message waiting for delivery
type QueuedMessage struct {
	ID          string      `json:"id"`
	From        peer.ID     `json:"from"`
	To          peer.ID     `json:"to"`
	Type        string      `json:"type"` // "chat", "file", "dm"
	Content     interface{} `json:"content"`
	CreatedAt   time.Time   `json:"created_at"`
	ExpiresAt   time.Time   `json:"expires_at"`
	Attempts    int         `json:"attempts"`
	LastAttempt time.Time   `json:"last_attempt,omitempty"`
}

// MessageQueue implements store-and-forward for offline peers
type MessageQueue struct {
	baseDir  string
	pending  map[peer.ID][]QueuedMessage
	ttl      time.Duration
	maxRetry int
	mu       sync.RWMutex
}

// NewMessageQueue creates a new message queue
func NewMessageQueue(baseDir string) (*MessageQueue, error) {
	queueDir := filepath.Join(baseDir, QueueDir)
	if err := os.MkdirAll(queueDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create queue directory: %w", err)
	}

	q := &MessageQueue{
		baseDir:  queueDir,
		pending:  make(map[peer.ID][]QueuedMessage),
		ttl:      DefaultTTL,
		maxRetry: 10,
	}

	// Load existing queued messages
	if err := q.load(); err != nil {
		fmt.Printf("Warning: failed to load queue: %v\n", err)
	}

	return q, nil
}

// Enqueue adds a message to the queue for a peer
func (q *MessageQueue) Enqueue(msg QueuedMessage) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if msg.ID == "" {
		msg.ID = fmt.Sprintf("%d", time.Now().UnixNano())
	}

	msg.CreatedAt = time.Now()
	msg.ExpiresAt = msg.CreatedAt.Add(q.ttl)
	msg.Attempts = 0

	q.pending[msg.To] = append(q.pending[msg.To], msg)

	return q.savePeer(msg.To)
}

// Dequeue retrieves all pending messages for a peer
func (q *MessageQueue) Dequeue(peerID peer.ID) []QueuedMessage {
	q.mu.Lock()
	defer q.mu.Unlock()

	messages := q.pending[peerID]
	delete(q.pending, peerID)

	// Remove persisted queue
	os.Remove(filepath.Join(q.baseDir, peerID.String()+".json"))

	return messages
}

// Peek returns messages without removing them
func (q *MessageQueue) Peek(peerID peer.ID) []QueuedMessage {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.pending[peerID]
}

// MarkAttempt marks a delivery attempt for a message
func (q *MessageQueue) MarkAttempt(peerID peer.ID, msgID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	messages := q.pending[peerID]
	for i := range messages {
		if messages[i].ID == msgID {
			messages[i].Attempts++
			messages[i].LastAttempt = time.Now()
			break
		}
	}

	q.savePeer(peerID)
}

// RemoveMessage removes a specific message from the queue
func (q *MessageQueue) RemoveMessage(peerID peer.ID, msgID string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	messages := q.pending[peerID]
	for i, msg := range messages {
		if msg.ID == msgID {
			q.pending[peerID] = append(messages[:i], messages[i+1:]...)
			q.savePeer(peerID)
			return true
		}
	}

	return false
}

// HasPending checks if a peer has pending messages
func (q *MessageQueue) HasPending(peerID peer.ID) bool {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.pending[peerID]) > 0
}

// PendingCount returns the number of pending messages for a peer
func (q *MessageQueue) PendingCount(peerID peer.ID) int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return len(q.pending[peerID])
}

// TotalPending returns total pending messages across all peers
func (q *MessageQueue) TotalPending() int {
	q.mu.RLock()
	defer q.mu.RUnlock()

	total := 0
	for _, msgs := range q.pending {
		total += len(msgs)
	}
	return total
}

// PeersWithPending returns all peers that have pending messages
func (q *MessageQueue) PeersWithPending() []peer.ID {
	q.mu.RLock()
	defer q.mu.RUnlock()

	peers := make([]peer.ID, 0, len(q.pending))
	for peerID := range q.pending {
		if len(q.pending[peerID]) > 0 {
			peers = append(peers, peerID)
		}
	}
	return peers
}

// Cleanup removes expired messages
func (q *MessageQueue) Cleanup() int {
	q.mu.Lock()
	defer q.mu.Unlock()

	removed := 0
	now := time.Now()

	for peerID, messages := range q.pending {
		var valid []QueuedMessage
		for _, msg := range messages {
			if now.Before(msg.ExpiresAt) && msg.Attempts < q.maxRetry {
				valid = append(valid, msg)
			} else {
				removed++
			}
		}

		if len(valid) == 0 {
			delete(q.pending, peerID)
			os.Remove(filepath.Join(q.baseDir, peerID.String()+".json"))
		} else {
			q.pending[peerID] = valid
			q.savePeer(peerID)
		}
	}

	return removed
}

// StartCleanupLoop starts periodic cleanup
func (q *MessageQueue) StartCleanupLoop(interval time.Duration) chan struct{} {
	done := make(chan struct{})

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				removed := q.Cleanup()
				if removed > 0 {
					fmt.Printf("Queue cleanup: removed %d expired messages\n", removed)
				}
			case <-done:
				return
			}
		}
	}()

	return done
}

// load reads all queued messages from disk
func (q *MessageQueue) load() error {
	files, err := os.ReadDir(q.baseDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() || filepath.Ext(file.Name()) != ".json" {
			continue
		}

		peerIDStr := file.Name()[:len(file.Name())-5]
		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			continue
		}

		data, err := os.ReadFile(filepath.Join(q.baseDir, file.Name()))
		if err != nil {
			continue
		}

		var messages []QueuedMessage
		if err := json.Unmarshal(data, &messages); err != nil {
			continue
		}

		q.pending[peerID] = messages
	}

	return nil
}

// savePeer saves messages for a peer to disk
func (q *MessageQueue) savePeer(peerID peer.ID) error {
	messages := q.pending[peerID]
	if len(messages) == 0 {
		return os.Remove(filepath.Join(q.baseDir, peerID.String()+".json"))
	}

	data, err := json.MarshalIndent(messages, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(q.baseDir, peerID.String()+".json"), data, 0644)
}

// Stats returns queue statistics
type QueueStats struct {
	TotalMessages   int
	TotalPeers      int
	OldestMessage   time.Time
	AverageAttempts float64
}

func (q *MessageQueue) Stats() QueueStats {
	q.mu.RLock()
	defer q.mu.RUnlock()

	stats := QueueStats{
		TotalPeers: len(q.pending),
	}

	var totalAttempts int
	for _, msgs := range q.pending {
		stats.TotalMessages += len(msgs)
		for _, msg := range msgs {
			totalAttempts += msg.Attempts
			if stats.OldestMessage.IsZero() || msg.CreatedAt.Before(stats.OldestMessage) {
				stats.OldestMessage = msg.CreatedAt
			}
		}
	}

	if stats.TotalMessages > 0 {
		stats.AverageAttempts = float64(totalAttempts) / float64(stats.TotalMessages)
	}

	return stats
}
