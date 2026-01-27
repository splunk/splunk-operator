package data

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Cache provides local caching for datasets
type Cache struct {
	cacheDir string
	mu       sync.RWMutex
	index    map[string]*CacheEntry
}

// CacheEntry represents a cached dataset
type CacheEntry struct {
	Key          string    `json:"key"`
	Path         string    `json:"path"`
	Size         int64     `json:"size"`
	LastAccessed time.Time `json:"last_accessed"`
	Checksum     string    `json:"checksum"`
}

// NewCache creates a new cache
func NewCache(cacheDir string) (*Cache, error) {
	if cacheDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		cacheDir = filepath.Join(home, ".e2e-cache")
	}

	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &Cache{
		cacheDir: cacheDir,
		index:    make(map[string]*CacheEntry),
	}

	// Load existing cache index
	cache.loadIndex()

	return cache, nil
}

// Get retrieves a cached dataset
func (c *Cache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	entry, exists := c.index[key]
	if !exists {
		return nil, false
	}

	// Verify file still exists
	if _, err := os.Stat(entry.Path); os.IsNotExist(err) {
		return nil, false
	}

	// Update last accessed time
	entry.LastAccessed = time.Now()
	return entry, true
}

// Put adds a dataset to the cache
func (c *Cache) Put(key string, sourcePath string) (*CacheEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Generate cache file path
	cacheFile := filepath.Join(c.cacheDir, c.generateCacheFilename(key))

	// Copy file to cache
	if err := c.copyFile(sourcePath, cacheFile); err != nil {
		return nil, fmt.Errorf("failed to cache file: %w", err)
	}

	// Calculate checksum
	checksum, err := c.calculateChecksum(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate checksum: %w", err)
	}

	// Get file size
	stat, err := os.Stat(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to stat cached file: %w", err)
	}

	entry := &CacheEntry{
		Key:          key,
		Path:         cacheFile,
		Size:         stat.Size(),
		LastAccessed: time.Now(),
		Checksum:     checksum,
	}

	c.index[key] = entry
	c.saveIndex()

	return entry, nil
}

// PutReader adds a dataset from a reader to the cache
func (c *Cache) PutReader(key string, reader io.Reader) (*CacheEntry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Generate cache file path
	cacheFile := filepath.Join(c.cacheDir, c.generateCacheFilename(key))

	// Write to cache file
	file, err := os.Create(cacheFile)
	if err != nil {
		return nil, fmt.Errorf("failed to create cache file: %w", err)
	}
	defer file.Close()

	hasher := sha256.New()
	multiWriter := io.MultiWriter(file, hasher)

	size, err := io.Copy(multiWriter, reader)
	if err != nil {
		return nil, fmt.Errorf("failed to write to cache: %w", err)
	}

	checksum := hex.EncodeToString(hasher.Sum(nil))

	entry := &CacheEntry{
		Key:          key,
		Path:         cacheFile,
		Size:         size,
		LastAccessed: time.Now(),
		Checksum:     checksum,
	}

	c.index[key] = entry
	c.saveIndex()

	return entry, nil
}

// Delete removes a dataset from the cache
func (c *Cache) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, exists := c.index[key]
	if !exists {
		return nil
	}

	// Delete file
	if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cache file: %w", err)
	}

	delete(c.index, key)
	c.saveIndex()

	return nil
}

// Clear removes all cached datasets
func (c *Cache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	for key, entry := range c.index {
		if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to delete cache file: %w", err)
		}
		delete(c.index, key)
	}

	c.saveIndex()
	return nil
}

// Prune removes old or unused cache entries
func (c *Cache) Prune(maxAge time.Duration, maxSize int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var totalSize int64

	// Build list of entries sorted by last accessed
	type entryWithKey struct {
		key   string
		entry *CacheEntry
	}
	var entries []entryWithKey

	for key, entry := range c.index {
		entries = append(entries, entryWithKey{key, entry})
		totalSize += entry.Size
	}

	// Sort by last accessed (oldest first)
	// Simple bubble sort since cache size is typically small
	for i := 0; i < len(entries)-1; i++ {
		for j := i + 1; j < len(entries); j++ {
			if entries[i].entry.LastAccessed.After(entries[j].entry.LastAccessed) {
				entries[i], entries[j] = entries[j], entries[i]
			}
		}
	}

	// Prune based on age and size
	for _, e := range entries {
		shouldPrune := false

		// Prune if too old
		if maxAge > 0 && now.Sub(e.entry.LastAccessed) > maxAge {
			shouldPrune = true
		}

		// Prune if total size exceeds limit
		if maxSize > 0 && totalSize > maxSize {
			shouldPrune = true
			totalSize -= e.entry.Size
		}

		if shouldPrune {
			if err := os.Remove(e.entry.Path); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to delete cache file: %w", err)
			}
			delete(c.index, e.key)
		}
	}

	c.saveIndex()
	return nil
}

// Stats returns cache statistics
func (c *Cache) Stats() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var totalSize int64
	for _, entry := range c.index {
		totalSize += entry.Size
	}

	return map[string]interface{}{
		"entries":    len(c.index),
		"total_size": totalSize,
		"cache_dir":  c.cacheDir,
	}
}

// Helper functions

func (c *Cache) generateCacheFilename(key string) string {
	hash := sha256.Sum256([]byte(key))
	return hex.EncodeToString(hash[:])
}

func (c *Cache) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func (c *Cache) calculateChecksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func (c *Cache) loadIndex() {
	// TODO: Implement index loading from JSON file if needed
	// For now, cache index is managed in memory only
}

func (c *Cache) saveIndex() {
	// TODO: Implement index saving to JSON file if needed
	// For now, cache index is managed in memory only
}
