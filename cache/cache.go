// Package cache keeps a compact copy of each parsed transcript, keyed by
// the file's path, size and modification time, so later runs re-read only
// the transcripts that changed. Timelines are rebuilt from the cached
// entries on every run, so changing an attribution flag never needs a
// cache flush.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/bensyverson/wallclock/transcript"
)

// SchemaVersion changes whenever the cached form changes shape or meaning;
// entries written under another version are ignored.
const SchemaVersion = 1

// Cache stores compact parsed transcripts under a directory. A Cache with
// an empty directory is disabled and always parses.
type Cache struct {
	dir     string
	version int
	parse   func(path string) (*transcript.File, error)
}

// Open returns a cache rooted at dir; "" disables caching.
func Open(dir string) *Cache {
	return &Cache{dir: dir, version: SchemaVersion, parse: transcript.ParseFile}
}

// Reopen returns a fresh handle on the same directory, with c's settings.
func Reopen(c *Cache) *Cache {
	cp := *c
	return &cp
}

type record struct {
	Version int              `json:"version"`
	Path    string           `json:"path"`
	Size    int64            `json:"size"`
	ModTime int64            `json:"mod_time"`
	File    *transcript.File `json:"file"`
}

// Load returns the compacted transcript at path, from the cache when the
// file is unchanged.
func (c *Cache) Load(path string) (*transcript.File, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := c.keyPath(path)
	if c.dir != "" {
		if rec, ok := c.read(key); ok && rec.Version == c.version && rec.Path == path &&
			rec.Size == fi.Size() && rec.ModTime == fi.ModTime().UnixNano() {
			return rec.File, nil
		}
	}
	f, err := c.parse(path)
	if err != nil {
		return nil, err
	}
	f.Compact()
	if c.dir != "" {
		// A failed cache write costs only speed on the next run.
		_ = c.write(key, record{Version: c.version, Path: path, Size: fi.Size(), ModTime: fi.ModTime().UnixNano(), File: f})
	}
	return f, nil
}

func (c *Cache) versionDir() string {
	return filepath.Join(c.dir, "v"+strconv.Itoa(c.version))
}

func (c *Cache) keyPath(path string) string {
	sum := sha256.Sum256([]byte(path))
	return filepath.Join(c.versionDir(), hex.EncodeToString(sum[:16])+".json")
}

func (c *Cache) read(key string) (record, bool) {
	var rec record
	data, err := os.ReadFile(key)
	if err != nil {
		return rec, false
	}
	return rec, json.Unmarshal(data, &rec) == nil
}

// write stores rec atomically: a temp file renamed into place.
func (c *Cache) write(key string, rec record) error {
	if err := os.MkdirAll(filepath.Dir(key), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(key), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), key)
}

// Clear removes every cached entry.
func (c *Cache) Clear() error {
	if c.dir == "" {
		return nil
	}
	entries, err := os.ReadDir(c.dir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "v") {
			if err := os.RemoveAll(filepath.Join(c.dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

// Stats counts the current version's cached files and their total size.
func (c *Cache) Stats() (files int, size int64, err error) {
	if c.dir == "" {
		return 0, 0, nil
	}
	entries, err := os.ReadDir(c.versionDir())
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && strings.HasSuffix(e.Name(), ".json") {
			files++
			size += fi.Size()
		}
	}
	return files, size, nil
}

// Dir is the cache's root directory ("" when disabled).
func (c *Cache) Dir() string { return c.dir }
