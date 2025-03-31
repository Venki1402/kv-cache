package main

import (
	"container/list"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"sync"
	"time"
)

const (
	// Configuration
	NumShards         = 32                       // Number of shards for the map
	MaxKeySize        = 256                      // Maximum key length
	MaxValueSize      = 256                      // Maximum value length
	MemoryThreshold   = 0.7                      // Memory threshold (70%)
	CleanupInterval   = 5 * time.Second          // Interval to check memory usage
	MaxMemoryBytes    = 1.5 * 1024 * 1024 * 1024 // 1.5GB max memory (leaving headroom)
	EvictionBatchSize = 100                      // Number of items to evict in one batch
)

// Cache entry with metadata for LRU
type CacheEntry struct {
	value     string
	timestamp time.Time
	size      int // Size in bytes
}

// Shard is a single shard of the cache
type Shard struct {
	items     map[string]*list.Element
	evictList *list.List
	lock      sync.RWMutex
	size      int64 // Track size in bytes
}

// ShardedCache is our main cache structure
type ShardedCache struct {
	shards    [NumShards]*Shard
	totalSize int64
	sizeLock  sync.RWMutex
}

// KeyValue is used for API requests
type KeyValue struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// Response is the API response format
type Response struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
	Key     string `json:"key,omitempty"`
	Value   string `json:"value,omitempty"`
}

// Initialize a new sharded cache
func NewShardedCache() *ShardedCache {
	cache := &ShardedCache{}
	for i := 0; i < NumShards; i++ {
		cache.shards[i] = &Shard{
			items:     make(map[string]*list.Element),
			evictList: list.New(),
			lock:      sync.RWMutex{},
		}
	}
	return cache
}

// Get the shard for a key
func (c *ShardedCache) getShard(key string) *Shard {
	// Simple hash function to determine shard
	hash := 0
	for _, char := range key {
		hash = 31*hash + int(char)
	}
	if hash < 0 {
		hash = -hash
	}
	return c.shards[hash%NumShards]
}

// Add or update a key-value pair
func (c *ShardedCache) Put(key, value string) error {
	if len(key) > MaxKeySize || len(value) > MaxValueSize {
		return fmt.Errorf("key or value exceeds maximum size")
	}

	// Calculate entry size (key + value + overhead)
	entrySize := len(key) + len(value) + 64 // 64 bytes overhead estimate

	shard := c.getShard(key)
	shard.lock.Lock()
	defer shard.lock.Unlock()

	// If key exists, update it and move to front of eviction list
	if element, found := shard.items[key]; found {
		entry := element.Value.(*CacheEntry)
		oldSize := entry.size

		// Update the entry
		entry.value = value
		entry.timestamp = time.Now()
		entry.size = entrySize
		shard.evictList.MoveToFront(element)

		// Update size tracking
		sizeChange := entrySize - oldSize
		shard.size += int64(sizeChange)

		c.sizeLock.Lock()
		c.totalSize += int64(sizeChange)
		c.sizeLock.Unlock()
	} else {
		// Key doesn't exist, create new entry
		entry := &CacheEntry{
			value:     value,
			timestamp: time.Now(),
			size:      entrySize,
		}
		element := shard.evictList.PushFront(entry)
		shard.items[key] = element

		// Update size tracking
		shard.size += int64(entrySize)

		c.sizeLock.Lock()
		c.totalSize += int64(entrySize)
		c.sizeLock.Unlock()
	}

	return nil
}

// Get a value by key
func (c *ShardedCache) Get(key string) (string, bool) {
	shard := c.getShard(key)
	shard.lock.RLock()
	defer shard.lock.RUnlock()

	if element, found := shard.items[key]; found {
		// Update access time and move to front (under read lock)
		entry := element.Value.(*CacheEntry)

		// Clone the lock to write - this is a critical section
		shard.lock.RUnlock()
		shard.lock.Lock()

		// Move to front now that we have write lock
		entry.timestamp = time.Now()
		shard.evictList.MoveToFront(element)

		// Downgrade lock
		value := entry.value
		shard.lock.Unlock()
		shard.lock.RLock()

		return value, true
	}

	return "", false
}

// Evict a single entry from a shard
func (shard *Shard) evictOne() int64 {
	if shard.evictList.Len() == 0 {
		return 0
	}

	// Get the oldest entry
	element := shard.evictList.Back()
	if element == nil {
		return 0
	}

	entry := element.Value.(*CacheEntry)

	// Remove from list and map
	shard.evictList.Remove(element)
	for key, el := range shard.items {
		if el == element {
			delete(shard.items, key)
			break
		}
	}

	freedSize := int64(entry.size)
	shard.size -= freedSize
	return freedSize
}

// EvictBatch evicts multiple items at once
func (c *ShardedCache) EvictBatch(count int) int64 {
	var totalFreed int64 = 0

	// Distribute evictions across shards
	perShard := count / NumShards
	if perShard < 1 {
		perShard = 1
	}

	for i := 0; i < NumShards; i++ {
		shard := c.shards[i]
		shard.lock.Lock()

		var shardFreed int64 = 0
		for j := 0; j < perShard && shard.evictList.Len() > 0; j++ {
			shardFreed += shard.evictOne()
		}

		shard.lock.Unlock()

		c.sizeLock.Lock()
		c.totalSize -= shardFreed
		totalFreed += shardFreed
		c.sizeLock.Unlock()
	}

	return totalFreed
}

// CheckMemory checks and manages memory usage
func (c *ShardedCache) CheckMemory() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	memUsage := float64(m.Alloc) / float64(MaxMemoryBytes)

	if memUsage >= MemoryThreshold {
		log.Printf("Memory usage at %.2f%%, evicting cache entries", memUsage*100)
		// Aggressive eviction under high memory pressure
		c.EvictBatch(EvictionBatchSize)
	}
}

// RunMemoryMonitor starts a goroutine to monitor memory
func (c *ShardedCache) RunMemoryMonitor() {
	ticker := time.NewTicker(CleanupInterval)
	go func() {
		for range ticker.C {
			c.CheckMemory()
		}
	}()
}

func main() {
	cache := NewShardedCache()
	cache.RunMemoryMonitor()

	// PUT endpoint
	http.HandleFunc("/put", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return	
		}

		var kv KeyValue
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&kv); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{
				Status:  "ERROR",
				Message: "Invalid request format",
			})
			return
		}

		if err := cache.Put(kv.Key, kv.Value); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{
				Status:  "ERROR",
				Message: err.Error(),
			})
			return
		}

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(Response{
			Status:  "OK",
			Message: "Key inserted/updated successfully.",
		})
	})

	// GET endpoint
	http.HandleFunc("/get", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		key := r.URL.Query().Get("key")
		if key == "" {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(Response{
				Status:  "ERROR",
				Message: "Key parameter is required",
			})
			return
		}

		if value, found := cache.Get(key); found {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(Response{
				Status: "OK",
				Key:    key,
				Value:  value,
			})
		} else {
			w.WriteHeader(http.StatusNotFound)
			json.NewEncoder(w).Encode(Response{
				Status:  "ERROR",
				Message: "Key not found.",
			})
		}
	})

	// Start the server
	fmt.Println("Starting server on port 7171...")
	log.Fatal(http.ListenAndServe(":7171", nil))
}
