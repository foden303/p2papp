package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"sync"

	"github.com/serialx/hashring"
)

// ConsistentHashRouter implements consistent hashing for load distribution
type ConsistentHashRouter struct {
	ring     *hashring.HashRing
	nodes    map[string]NodeInfo
	replicas int // Virtual nodes per physical node
	mu       sync.RWMutex
}

// NodeInfo contains information about a relay node
type NodeInfo struct {
	ID       string
	Address  string
	Weight   int
	Healthy  bool
	Load     int64 // Current load (bytes or connections)
	Capacity int64 // Max capacity
}

// NewConsistentHashRouter creates a new consistent hash router
func NewConsistentHashRouter(replicas int) *ConsistentHashRouter {
	return &ConsistentHashRouter{
		ring:     hashring.New([]string{}),
		nodes:    make(map[string]NodeInfo),
		replicas: replicas,
	}
}

// AddNode adds a node to the hash ring
func (r *ConsistentHashRouter) AddNode(node NodeInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nodes[node.ID] = node

	// Rebuild ring with weighted nodes
	r.rebuildRing()
}

// RemoveNode removes a node from the hash ring
func (r *ConsistentHashRouter) RemoveNode(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.nodes, nodeID)
	r.rebuildRing()
}

// UpdateNode updates a node's info
func (r *ConsistentHashRouter) UpdateNode(node NodeInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nodes[node.ID] = node
	r.rebuildRing()
}

// SetHealthy marks a node as healthy/unhealthy
func (r *ConsistentHashRouter) SetHealthy(nodeID string, healthy bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if node, exists := r.nodes[nodeID]; exists {
		node.Healthy = healthy
		r.nodes[nodeID] = node
		r.rebuildRing()
	}
}

// rebuildRing rebuilds the hash ring with current nodes
func (r *ConsistentHashRouter) rebuildRing() {
	var nodeList []string
	for _, node := range r.nodes {
		if node.Healthy {
			// Add virtual nodes based on weight
			weight := node.Weight
			if weight <= 0 {
				weight = 1
			}
			for i := 0; i < weight*r.replicas; i++ {
				nodeList = append(nodeList, node.ID)
			}
		}
	}
	r.ring = hashring.New(nodeList)
}

// GetNode returns the node responsible for a key
func (r *ConsistentHashRouter) GetNode(key string) (NodeInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeID, ok := r.ring.GetNode(key)
	if !ok {
		return NodeInfo{}, false
	}

	node, exists := r.nodes[nodeID]
	return node, exists
}

// GetNodes returns N nodes responsible for a key (for replication)
func (r *ConsistentHashRouter) GetNodes(key string, n int) []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodeIDs, ok := r.ring.GetNodes(key, n)
	if !ok {
		return nil
	}

	// Deduplicate nodes
	seen := make(map[string]bool)
	var nodes []NodeInfo
	for _, id := range nodeIDs {
		if !seen[id] {
			seen[id] = true
			if node, exists := r.nodes[id]; exists {
				nodes = append(nodes, node)
			}
		}
	}

	return nodes
}

// GetAllNodes returns all registered nodes
func (r *ConsistentHashRouter) GetAllNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	nodes := make([]NodeInfo, 0, len(r.nodes))
	for _, node := range r.nodes {
		nodes = append(nodes, node)
	}

	// Sort by ID for consistency
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].ID < nodes[j].ID
	})

	return nodes
}

// GetHealthyNodes returns only healthy nodes
func (r *ConsistentHashRouter) GetHealthyNodes() []NodeInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var nodes []NodeInfo
	for _, node := range r.nodes {
		if node.Healthy {
			nodes = append(nodes, node)
		}
	}

	return nodes
}

// NodeCount returns the number of nodes
func (r *ConsistentHashRouter) NodeCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.nodes)
}

// HashKey hashes a key for consistent routing
func HashKey(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

// GetShardKey returns the shard key for different data types
func GetShardKey(dataType string, id string) string {
	return dataType + ":" + id
}
