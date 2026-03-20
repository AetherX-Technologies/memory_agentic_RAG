package generator

import (
	"crypto/sha256"
	"fmt"
	"sync"
)

// Cache provides an in-memory content-hash cache for generated summaries.
// Keys are SHA256(content) + level, values are the generated text.
type Cache struct {
	mu    sync.RWMutex
	store map[string]string
}

// NewCache creates a new empty cache.
func NewCache() *Cache {
	return &Cache{
		store: make(map[string]string),
	}
}

// Get retrieves a cached summary. Returns (value, true) on hit, ("", false) on miss.
func (c *Cache) Get(content string, level int) (string, bool) {
	key := cacheKey(content, level)
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.store[key]
	return val, ok
}

// Set stores a generated summary in the cache.
func (c *Cache) Set(content string, level int, value string) {
	key := cacheKey(content, level)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store[key] = value
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.store)
}

// cacheKey produces a unique key from content hash + level.
func cacheKey(content string, level int) string {
	hash := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x:L%d", hash, level)
}
