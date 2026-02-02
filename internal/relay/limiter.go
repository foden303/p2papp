package relay

import (
	"sync"
	"time"
)

// TokenBucket implements the token bucket rate limiting algorithm
type TokenBucket struct {
	tokens     float64
	maxTokens  float64
	refillRate float64 // tokens per second
	lastRefill time.Time
	mu         sync.Mutex
}

// NewTokenBucket creates a new token bucket
func NewTokenBucket(maxTokens, refillRate float64) *TokenBucket {
	return &TokenBucket{
		tokens:     maxTokens,
		maxTokens:  maxTokens,
		refillRate: refillRate,
		lastRefill: time.Now(),
	}
}

// Allow checks if a request is allowed and consumes a token
func (b *TokenBucket) Allow() bool {
	return b.AllowN(1)
}

// AllowN checks if n tokens are available and consumes them
func (b *TokenBucket) AllowN(n float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.refill()

	if b.tokens >= n {
		b.tokens -= n
		return true
	}
	return false
}

// refill adds tokens based on time elapsed
func (b *TokenBucket) refill() {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * b.refillRate
	if b.tokens > b.maxTokens {
		b.tokens = b.maxTokens
	}
	b.lastRefill = now
}

// Tokens returns the current number of tokens
func (b *TokenBucket) Tokens() float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()
	return b.tokens
}

// WaitTime returns how long to wait for n tokens
func (b *TokenBucket) WaitTime(n float64) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refill()

	if b.tokens >= n {
		return 0
	}

	needed := n - b.tokens
	return time.Duration(needed / b.refillRate * float64(time.Second))
}

// LimiterConfig configures the rate limiter
type LimiterConfig struct {
	MaxRequestsPerSecond  float64
	MaxBurstSize          float64
	MaxBytesPerSecond     int64
	MaxConnectionsPerPeer int
}

// DefaultLimiterConfig returns default limiter configuration
func DefaultLimiterConfig() LimiterConfig {
	return LimiterConfig{
		MaxRequestsPerSecond:  100,
		MaxBurstSize:          200,
		MaxBytesPerSecond:     10 * 1024 * 1024, // 10 MB/s
		MaxConnectionsPerPeer: 10,
	}
}

// RateLimiter manages rate limits per peer
type RateLimiter struct {
	config       LimiterConfig
	peerBuckets  map[string]*TokenBucket
	byteBuckets  map[string]*TokenBucket
	connections  map[string]int
	globalBucket *TokenBucket
	mu           sync.RWMutex
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(config LimiterConfig) *RateLimiter {
	return &RateLimiter{
		config:       config,
		peerBuckets:  make(map[string]*TokenBucket),
		byteBuckets:  make(map[string]*TokenBucket),
		connections:  make(map[string]int),
		globalBucket: NewTokenBucket(config.MaxBurstSize*10, config.MaxRequestsPerSecond*10),
	}
}

// AllowRequest checks if a request from a peer is allowed
func (r *RateLimiter) AllowRequest(peerID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Check global limit first
	if !r.globalBucket.Allow() {
		return false
	}

	// Get or create peer bucket
	bucket, exists := r.peerBuckets[peerID]
	if !exists {
		bucket = NewTokenBucket(r.config.MaxBurstSize, r.config.MaxRequestsPerSecond)
		r.peerBuckets[peerID] = bucket
	}

	return bucket.Allow()
}

// AllowBytes checks if bytes transfer is allowed
func (r *RateLimiter) AllowBytes(peerID string, bytes int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	bucket, exists := r.byteBuckets[peerID]
	if !exists {
		bucket = NewTokenBucket(float64(r.config.MaxBytesPerSecond), float64(r.config.MaxBytesPerSecond))
		r.byteBuckets[peerID] = bucket
	}

	return bucket.AllowN(float64(bytes))
}

// AllowConnection checks if a new connection is allowed
func (r *RateLimiter) AllowConnection(peerID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	count := r.connections[peerID]
	if count >= r.config.MaxConnectionsPerPeer {
		return false
	}

	r.connections[peerID] = count + 1
	return true
}

// ReleaseConnection releases a connection slot
func (r *RateLimiter) ReleaseConnection(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if count := r.connections[peerID]; count > 0 {
		r.connections[peerID] = count - 1
	}
}

// WaitForRequest returns how long to wait for a request
func (r *RateLimiter) WaitForRequest(peerID string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	bucket, exists := r.peerBuckets[peerID]
	if !exists {
		return 0
	}

	return bucket.WaitTime(1)
}

// GetPeerStats returns rate limit stats for a peer
func (r *RateLimiter) GetPeerStats(peerID string) PeerLimitStats {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := PeerLimitStats{
		PeerID:      peerID,
		Connections: r.connections[peerID],
	}

	if bucket, exists := r.peerBuckets[peerID]; exists {
		stats.RequestTokens = bucket.Tokens()
	}

	if bucket, exists := r.byteBuckets[peerID]; exists {
		stats.ByteTokens = int64(bucket.Tokens())
	}

	return stats
}

// PeerLimitStats contains rate limit statistics for a peer
type PeerLimitStats struct {
	PeerID        string
	RequestTokens float64
	ByteTokens    int64
	Connections   int
}

// Cleanup removes stale peer data
func (r *RateLimiter) Cleanup(maxAge time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()

	// Clean up buckets that haven't been used
	for peerID, bucket := range r.peerBuckets {
		bucket.mu.Lock()
		if now.Sub(bucket.lastRefill) > maxAge {
			delete(r.peerBuckets, peerID)
			delete(r.byteBuckets, peerID)
			delete(r.connections, peerID)
		}
		bucket.mu.Unlock()
	}
}

// Reset resets all limits for a peer
func (r *RateLimiter) Reset(peerID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.peerBuckets, peerID)
	delete(r.byteBuckets, peerID)
	delete(r.connections, peerID)
}

// ResetAll resets all limits
func (r *RateLimiter) ResetAll() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.peerBuckets = make(map[string]*TokenBucket)
	r.byteBuckets = make(map[string]*TokenBucket)
	r.connections = make(map[string]int)
}
