package storage

import (
	"container/list"
	"sync"
	"time"
)

// CacheConfig configures the cache behavior
type CacheConfig struct {
	MaxSize int           // Maximum number of entries
	TTL     time.Duration // Time-to-live for cache entries (0 = no expiration)
}

// DefaultCacheConfig returns default cache configuration
func DefaultCacheConfig() CacheConfig {
	return CacheConfig{
		MaxSize: 10000, // 10k entries
		TTL:     0,     // No expiration by default
	}
}

// cacheEntry represents a cached key-value pair with metadata
type cacheEntry struct {
	key       string
	value     []byte
	createdAt time.Time
	element   *list.Element // Pointer to element in LRU list
}

// LRUCache is a thread-safe LRU cache implementation
type LRUCache struct {
	mu       sync.RWMutex
	capacity int
	ttl      time.Duration
	items    map[string]*cacheEntry
	lruList  *list.List // Most recently used at front

	// Statistics
	hits   uint64
	misses uint64
	evicts uint64
}

// NewLRUCache creates a new LRU cache
func NewLRUCache(config CacheConfig) *LRUCache {
	return &LRUCache{
		capacity: config.MaxSize,
		ttl:      config.TTL,
		items:    make(map[string]*cacheEntry, config.MaxSize),
		lruList:  list.New(),
	}
}

// Get retrieves a value from the cache
func (c *LRUCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.items[key]
	if !exists {
		c.misses++
		return nil, false
	}

	// Check if entry has expired
	if c.ttl > 0 && time.Since(entry.createdAt) > c.ttl {
		c.remove(entry)
		c.misses++
		return nil, false
	}

	// Move to front (most recently used)
	c.lruList.MoveToFront(entry.element)
	c.hits++

	// Return a copy to prevent external mutation
	value := make([]byte, len(entry.value))
	copy(value, entry.value)
	return value, true
}

// Put adds or updates a value in the cache
func (c *LRUCache) Put(key string, value []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Make a copy of the value to prevent external mutation
	valueCopy := make([]byte, len(value))
	copy(valueCopy, value)

	// Check if key already exists
	if entry, exists := c.items[key]; exists {
		// Update existing entry
		entry.value = valueCopy
		entry.createdAt = time.Now()
		c.lruList.MoveToFront(entry.element)
		return
	}

	// Create new entry
	entry := &cacheEntry{
		key:       key,
		value:     valueCopy,
		createdAt: time.Now(),
	}

	// Add to front of LRU list
	entry.element = c.lruList.PushFront(entry)
	c.items[key] = entry

	// Evict least recently used if at capacity
	if c.lruList.Len() > c.capacity {
		c.evictOldest()
	}
}

// Delete removes a key from the cache
func (c *LRUCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry, exists := c.items[key]; exists {
		c.remove(entry)
	}
}

// Clear removes all entries from the cache
func (c *LRUCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*cacheEntry, c.capacity)
	c.lruList = list.New()
}

// Stats returns cache statistics
func (c *LRUCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	total := c.hits + c.misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(c.hits) / float64(total) * 100
	}

	return CacheStats{
		Size:    c.lruList.Len(),
		Hits:    c.hits,
		Misses:  c.misses,
		Evicts:  c.evicts,
		HitRate: hitRate,
	}
}

// CacheStats holds cache statistics
type CacheStats struct {
	Size    int     // Current number of entries
	Hits    uint64  // Number of cache hits
	Misses  uint64  // Number of cache misses
	Evicts  uint64  // Number of evictions
	HitRate float64 // Hit rate percentage
}

// remove removes an entry from the cache (must hold lock)
func (c *LRUCache) remove(entry *cacheEntry) {
	c.lruList.Remove(entry.element)
	delete(c.items, entry.key)
}

// evictOldest removes the least recently used entry (must hold lock)
func (c *LRUCache) evictOldest() {
	oldest := c.lruList.Back()
	if oldest != nil {
		entry := oldest.Value.(*cacheEntry)
		c.remove(entry)
		c.evicts++
	}
}

// Size returns the current number of entries in the cache
func (c *LRUCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lruList.Len()
}
