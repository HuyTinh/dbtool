package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"dbtool/internal/config"
)

const (
	maxCacheBytes   = 500 * 1024 * 1024 // 500MB
	maxCacheEntries = 1000
)

type Fingerprint struct {
	Path    string
	Size    int64
	ModTime time.Time
}

func ComputeFingerprint(filePath string) (Fingerprint, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return Fingerprint{}, err
	}
	return Fingerprint{
		Path:    filePath,
		Size:    info.Size(),
		ModTime: info.ModTime(),
	}, nil
}

func (f Fingerprint) Key() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", f.Path, f.Size, f.ModTime.UnixNano())))
	return hex.EncodeToString(h[:])
}

type cacheEntry struct {
	SourcePath string          `json:"source_path"`
	ExpiresAt  time.Time      `json:"expires_at"`
	Value      json.RawMessage `json:"value"`
}

type FileCache struct {
	baseDir string
}

func NewFileCache() (*FileCache, error) {
	configDir, err := config.GetConfigDir()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(configDir, "cache")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &FileCache{baseDir: dir}, nil
}

func (c *FileCache) getNamespaceDir(namespace string) (string, error) {
	dir := filepath.Join(c.baseDir, namespace)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return dir, nil
}

func (c *FileCache) Get(namespace, key string, dest interface{}) (bool, error) {
	nsDir, err := c.getNamespaceDir(namespace)
	if err != nil {
		return false, err
	}

	path := filepath.Join(nsDir, key+".json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return false, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}

	var entry cacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return false, err
	}

	// Lazy validation: verify if source file still exists and matches the key
	currentFp, err := ComputeFingerprint(entry.SourcePath)
	if err != nil || currentFp.Key() != key {
		_ = c.Invalidate(namespace, key) // source file changed/deleted, invalidate cache
		return false, nil
	}

	// TTL check
	if !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt) {
		_ = c.Invalidate(namespace, key)
		return false, nil
	}

	// Update mod time for LRU purposes (cache HIT)
	now := time.Now()
	_ = os.Chtimes(path, now, now)

	if err := json.Unmarshal(entry.Value, dest); err != nil {
		return false, err
	}

	return true, nil
}

func (c *FileCache) Set(namespace, key string, sourcePath string, value interface{}, ttl time.Duration) error {
	nsDir, err := c.getNamespaceDir(namespace)
	if err != nil {
		return err
	}

	valBytes, err := json.Marshal(value)
	if err != nil {
		return err
	}

	var expiresAt time.Time
	if ttl > 0 {
		expiresAt = time.Now().Add(ttl)
	}

	entry := cacheEntry{
		SourcePath: sourcePath,
		ExpiresAt:  expiresAt,
		Value:      valBytes,
	}

	entryBytes, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	path := filepath.Join(nsDir, key+".json")
	if err := writeCacheAtomic(path, entryBytes); err != nil {
		return err
	}

	// Trigger LRU eviction in the background
	go c.evictIfNeeded(nsDir)

	return nil
}

func (c *FileCache) Invalidate(namespace, key string) error {
	nsDir, err := c.getNamespaceDir(namespace)
	if err != nil {
		return err
	}
	path := filepath.Join(nsDir, key+".json")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (c *FileCache) Clear() error {
	return os.RemoveAll(c.baseDir)
}

func (c *FileCache) ClearNamespace(namespace string) error {
	nsDir := filepath.Join(c.baseDir, namespace)
	return os.RemoveAll(nsDir)
}

func writeCacheAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *FileCache) evictIfNeeded(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	type entryInfo struct {
		path    string
		size    int64
		modTime time.Time
	}

	var infos []entryInfo
	var totalSize int64

	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		infos = append(infos, entryInfo{
			path:    filepath.Join(dir, e.Name()),
			size:    fi.Size(),
			modTime: fi.ModTime(),
		})
		totalSize += fi.Size()
	}

	if totalSize <= maxCacheBytes && len(infos) <= maxCacheEntries {
		return
	}

	// Sort by modTime ascending: oldest modified first
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].modTime.Before(infos[j].modTime)
	})

	for _, info := range infos {
		if totalSize <= maxCacheBytes && len(infos) <= maxCacheEntries {
			break
		}
		if err := os.Remove(info.path); err == nil {
			totalSize -= info.size
			// Remove from slice
			infos = infos[1:]
		}
	}
}
