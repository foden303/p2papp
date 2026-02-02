package relay

import (
	"container/list"
	"sync"
	"time"
)

// CacheItem represents an item in the LRU cache
type CacheItem struct {
	Key        string
	Data       []byte
	Size       int64
	CreatedAt  time.Time
	AccessedAt time.Time
	TTL        time.Duration
}

// LRUCache implements a Least Recently Used cache with size limits
type LRUCache struct {
	capacity    int64 // Max bytes
	currentSize int64
	items       *list.List
	lookup      map[string]*list.Element
	mu          sync.RWMutex

	// Stats
	hits   int64
	misses int64
}

// NewLRUCache creates a new LRU cache with the given capacity in bytes
func NewLRUCache(capacityBytes int64) *LRUCache {
	return &LRUCache{
		capacity: capacityBytes,
		items:    list.New(),
		lookup:   make(map[string]*list.Element),
	}
}

// Get retrieves an item from the cache
func (c *LRUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.lookup[key]
	if !exists {
		c.misses++
		return nil, false
	}

	item := elem.Value.(*CacheItem)

	// Check TTL
	if item.TTL > 0 && time.Since(item.CreatedAt) > item.TTL {
		c.removeElement(elem)
		c.misses++
		return nil, false
	}

	// Move to front (most recently used)
	c.items.MoveToFront(elem)
	item.AccessedAt = time.Now()
	c.hits++

	return item.Data, true
}

// Put adds an item to the cache
func (c *LRUCache) Put(key string, data []byte) {
	c.PutWithTTL(key, data, 0)
}

// PutWithTTL adds an item with a TTL
func (c *LRUCache) PutWithTTL(key string, data []byte, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	size := int64(len(data))

	// Check if item already exists
	if elem, exists := c.lookup[key]; exists {
		item := elem.Value.(*CacheItem)
		c.currentSize -= item.Size
		c.items.Remove(elem)
		delete(c.lookup, key)
	}

	// Evict items until we have enough space
	for c.currentSize+size > c.capacity && c.items.Len() > 0 {
		c.evictOldest()
	}

	// Don't add if single item is larger than capacity
	if size > c.capacity {
		return
	}

	item := &CacheItem{
		Key:        key,
		Data:       data,
		Size:       size,
		CreatedAt:  time.Now(),
		AccessedAt: time.Now(),
		TTL:        ttl,
	}

	elem := c.items.PushFront(item)
	c.lookup[key] = elem
	c.currentSize += size
}

// Delete removes an item from the cache
func (c *LRUCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.lookup[key]
	if !exists {
		return false
	}

	c.removeElement(elem)
	return true
}

// evictOldest removes the least recently used item
func (c *LRUCache) evictOldest() {
	elem := c.items.Back()
	if elem != nil {
		c.removeElement(elem)
	}
}

// removeElement removes an element from the cache
func (c *LRUCache) removeElement(elem *list.Element) {
	item := elem.Value.(*CacheItem)
	c.items.Remove(elem)
	delete(c.lookup, item.Key)
	c.currentSize -= item.Size
}

// Clear removes all items from the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = list.New()
	c.lookup = make(map[string]*list.Element)
	c.currentSize = 0
}

// Size returns the current size of the cache in bytes
func (c *LRUCache) Size() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentSize
}

// Len returns the number of items in the cache
func (c *LRUCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.items.Len()
}

// Capacity returns the max capacity in bytes
func (c *LRUCache) Capacity() int64 {
	return c.capacity
}

// Stats returns cache statistics
func (c *LRUCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	hitRate := float64(0)
	total := c.hits + c.misses
	if total > 0 {
		hitRate = float64(c.hits) / float64(total)
	}

	return CacheStats{
		Hits:        c.hits,
		Misses:      c.misses,
		HitRate:     hitRate,
		CurrentSize: c.currentSize,
		Capacity:    c.capacity,
		ItemCount:   c.items.Len(),
	}
}

// CacheStats contains cache statistics
type CacheStats struct {
	Hits        int64
	Misses      int64
	HitRate     float64
	CurrentSize int64
	Capacity    int64
	ItemCount   int
}

// CleanupExpired removes all expired items
func (c *LRUCache) CleanupExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove []*list.Element
	now := time.Now()

	for elem := c.items.Back(); elem != nil; elem = elem.Prev() {
		item := elem.Value.(*CacheItem)
		if item.TTL > 0 && now.Sub(item.CreatedAt) > item.TTL {
			toRemove = append(toRemove, elem)
		}
	}

	for _, elem := range toRemove {
		c.removeElement(elem)
	}

	return len(toRemove)
}

// Keys returns all keys in the cache
func (c *LRUCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	keys := make([]string, 0, c.items.Len())
	for elem := c.items.Front(); elem != nil; elem = elem.Next() {
		item := elem.Value.(*CacheItem)
		keys = append(keys, item.Key)
	}
	return keys
}

// Contains checks if a key exists in the cache
func (c *LRUCache) Contains(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	_, exists := c.lookup[key]
	return exists
}
