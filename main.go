package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"

	"container/list"
	"github.com/gorilla/mux"
)

// CacheEntry represents a key-value pair in the cache
type CacheEntry struct {
	Key   string
	Value string
}

// Cache implements a simple in-memory cache with LRU eviction
type Cache struct {
	cache    map[string]*list.Element
	lruList  *list.List
	capacity int
	mu       sync.RWMutex
}

// NewCache returns a new cache instance
func NewCache(capacity int) *Cache {
	return &Cache{
		cache:    make(map[string]*list.Element),
		lruList:  list.New(),
		capacity: capacity,
	}
}

// Put inserts or updates a key-value pair in the cache
func (c *Cache) Put(key, value string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, ok := c.cache[key]; ok {
		// Update existing entry
		entry := c.cache[key]
		entry.Value.(*CacheEntry).Value = value
		c.lruList.MoveToFront(entry)
	} else {
		// Add new entry if cache is not full
		if c.lruList.Len() >= c.capacity {
			// Evict LRU entry if cache is full
			lruEntry := c.lruList.Back()
			delete(c.cache, lruEntry.Value.(*CacheEntry).Key)
			c.lruList.Remove(lruEntry)
		}

		newEntry := &CacheEntry{Key: key, Value: value}
		element := c.lruList.PushFront(newEntry)
		c.cache[key] = element
	}

	return nil
}

// Get retrieves the value associated with a given key
func (c *Cache) Get(key string) (string, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if entry, ok := c.cache[key]; ok {
		// Move accessed entry to front of LRU list
		c.lruList.MoveToFront(entry)
		return entry.Value.(*CacheEntry).Value, nil
	}

	return "", errors.New("key not found")
}

func main() {
	cache := NewCache(1000)

	router := mux.NewRouter()
	router.HandleFunc("/put/{key}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		key := vars["key"]
		value := r.URL.Query().Get("value")

		if len(key) > 256 || len(value) > 256 {
			http.Error(w, "Key or value exceeds maximum length of 256 characters", http.StatusBadRequest)
			return
		}

		if err := cache.Put(key, value); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
	}).Methods("PUT")

	router.HandleFunc("/get/{key}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		key := vars["key"]

		value, err := cache.Get(key)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]string{"value": value})
	}).Methods("GET")

	http.ListenAndServe(":7171", router)
}
