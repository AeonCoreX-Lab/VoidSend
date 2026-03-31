package template

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/AeonCoreX-Lab/VoidSend/core/database"
	"github.com/AeonCoreX-Lab/VoidSend/monitoring"
)

// TemplateCacheEntry ক্যাশে রাখা টেমপ্লেটের এন্ট্রি
type TemplateCacheEntry struct {
	Key        string
	Template   interface{}
	Size       int
	LastAccess time.Time
	Expiry     time.Time
}

// TemplateCache ক্যাশ ম্যানেজমেন্ট的结构
type TemplateCache struct {
	mu       sync.RWMutex
	items    map[string]*list.Element
	lruList  *list.List
	capacity int                    // সর্বোচ্চ ক্যাশ সাইজ (বাইটে)
	maxItems int                    // সর্বোচ্চ আইটেম সংখ্যা
	ttl      time.Duration          // Time to live
}

// LRUItem for doubly linked list
type lruItem struct {
	key   string
	value *TemplateCacheEntry
}

var (
	defaultCache *TemplateCache
	cacheOnce    sync.Once
)

// GetCache returns the singleton cache instance
func GetCache() *TemplateCache {
	cacheOnce.Do(func() {
		defaultCache = &TemplateCache{
			items:    make(map[string]*list.Element),
			lruList:  list.New(),
			capacity: 100 * 1024 * 1024, // 100MB default
			maxItems: 1000,               // 1000 templates
			ttl:      30 * time.Minute,    // 30 minutes
		}
		
		// Start cache maintenance
		go defaultCache.startMaintenance()
	})
	return defaultCache
}

// Set ক্যাশে টেমপ্লেট সংরক্ষণ করে
func (c *TemplateCache) Set(key string, value interface{}, size int, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if key already exists
	if elem, exists := c.items[key]; exists {
		c.lruList.MoveToFront(elem)
		entry := elem.Value.(*lruItem).value
		entry.LastAccess = time.Now()
		if ttl > 0 {
			entry.Expiry = time.Now().Add(ttl)
		}
		return
	}

	// Check capacity and evict if needed
	for c.lruList.Len() >= c.maxItems || c.getCurrentSize() > c.capacity {
		if !c.evictLRU() {
			break
		}
	}

	// Create new entry
	entry := &TemplateCacheEntry{
		Key:        key,
		Template:   value,
		Size:       size,
		LastAccess: time.Now(),
		Expiry:     time.Now().Add(ttl),
	}

	// Add to cache
	elem := c.lruList.PushFront(&lruItem{
		key:   key,
		value: entry,
	})
	c.items[key] = elem

	monitoring.Debug("Cache set: %s, size: %d bytes", key, size)
}

// Get ক্যাশ থেকে টেমপ্লেট রিট্রিভ করে
func (c *TemplateCache) Get(key string) interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		// Try Redis if not in local cache
		return c.getFromRedis(key)
	}

	entry := elem.Value.(*lruItem).value

	// Check expiry
	if !entry.Expiry.IsZero() && time.Now().After(entry.Expiry) {
		c.deleteFromCache(key)
		return nil
	}

	// Update access time and move to front
	entry.LastAccess = time.Now()
	c.lruList.MoveToFront(elem)

	return entry.Template
}

// GetWithTTL ক্যাশ থেকে টেমপ্লেট নিয়ে আসে TTL সহ
func (c *TemplateCache) GetWithTTL(key string) (interface{}, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, exists := c.items[key]
	if !exists {
		return nil, 0
	}

	entry := elem.Value.(*lruItem).value
	
	// Check expiry
	if !entry.Expiry.IsZero() && time.Now().After(entry.Expiry) {
		c.deleteFromCache(key)
		return nil, 0
	}

	// Calculate remaining TTL
	var remainingTTL time.Duration
	if !entry.Expiry.IsZero() {
		remainingTTL = time.Until(entry.Expiry)
	}

	entry.LastAccess = time.Now()
	c.lruList.MoveToFront(elem)

	return entry.Template, remainingTTL
}

// Delete ক্যাশ থেকে টেমপ্লেট ডিলিট করে
func (c *TemplateCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deleteFromCache(key)
}

// Clear ক্যাশ полностью পরিষ্কার করে
func (c *TemplateCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items = make(map[string]*list.Element)
	c.lruList.Init()
	monitoring.Info("Cache cleared")
}

// GetStats ক্যাশের পরিসংখ্যান দেয়
func (c *TemplateCache) GetStats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	return map[string]interface{}{
		"item_count":  len(c.items),
		"memory_used": c.getCurrentSize(),
		"capacity":    c.capacity,
		"max_items":   c.maxItems,
		"hit_rate":    c.calculateHitRate(),
	}
}

// Private methods

func (c *TemplateCache) evictLRU() bool {
	elem := c.lruList.Back()
	if elem == nil {
		return false
	}

	item := elem.Value.(*lruItem)
	c.deleteFromCache(item.key)
	monitoring.Debug("Evicted LRU item: %s", item.key)
	return true
}

func (c *TemplateCache) deleteFromCache(key string) {
	if elem, exists := c.items[key]; exists {
		c.lruList.Remove(elem)
		delete(c.items, key)
	}
}

func (c *TemplateCache) getCurrentSize() int {
	var totalSize int
	for _, elem := range c.items {
		totalSize += elem.Value.(*lruItem).value.Size
	}
	return totalSize
}

func (c *TemplateCache) calculateHitRate() float64 {
	// This would need hit/miss counters - simplified version
	return 0.0
}

// Redis integration

func (c *TemplateCache) getFromRedis(key string) interface{} {
	ctx := context.Background()
	
	// Try to get from Redis
	data, err := database.RedisClient.Get(ctx, "cache:"+key).Bytes()
	if err != nil {
		return nil
	}

	// Parse and return
	// Note: You'd need to implement proper serialization/deserialization
	return string(data)
}

func (c *TemplateCache) saveToRedis(key string, value interface{}, ttl time.Duration) {
	ctx := context.Background()
	
	// Save to Redis (simplified - you'd need proper serialization)
	database.RedisClient.Set(ctx, "cache:"+key, value, ttl)
}

// Maintenance

func (c *TemplateCache) startMaintenance() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

func (c *TemplateCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	for key, elem := range c.items {
		entry := elem.Value.(*lruItem).value
		
		// Remove expired items
		if !entry.Expiry.IsZero() && now.After(entry.Expiry) {
			c.deleteFromCache(key)
			continue
		}

		// Remove items not accessed for a long time
		if now.Sub(entry.LastAccess) > 24*time.Hour {
			c.deleteFromCache(key)
		}
	}
}

// Template-specific cache methods

// CacheTemplate টেমপ্লেট ক্যাশ করে
func (c *TemplateCache) CacheTemplate(devID, action string, tmpl interface{}, content string) {
	key := c.getTemplateKey(devID, action)
	size := len(content)
	c.Set(key, tmpl, size, 1*time.Hour)
}

// GetCachedTemplate ক্যাশ করা টেমপ্লেট নিয়ে আসে
func (c *TemplateCache) GetCachedTemplate(devID, action string) interface{} {
	key := c.getTemplateKey(devID, action)
	return c.Get(key)
}

// InvalidateTemplate টেমপ্লেট ক্যাশ invalidate করে
func (c *TemplateCache) InvalidateTemplate(devID, action string) {
	key := c.getTemplateKey(devID, action)
	c.Delete(key)
	
	// Also invalidate in Redis
	ctx := context.Background()
	database.RedisClient.Del(ctx, "cache:"+key)
}

func (c *TemplateCache) getTemplateKey(devID, action string) string {
	if devID == "" {
		return "file:" + action
	}
	return "dev:" + devID + ":" + action
}