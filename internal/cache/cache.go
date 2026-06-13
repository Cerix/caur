// Package cache stores, for each pkgbase, the last approved review outcome
// together with a snapshot of the files and the supply-chain metadata
// (maintainer, version, modification date). It is used to:
//   - skip the review when the files are unchanged (identical hash);
//   - do a "diff-only" review against the last approved version;
//   - detect a maintainer change relative to last time.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"caur/internal/aur"
	"caur/internal/review"
)

// Entry is the persistent record for a pkgbase.
type Entry struct {
	Hash         string            `json:"hash"`          // hash of the reviewed files
	Result       review.Result     `json:"result"`        // outcome (without supply-chain findings)
	Files        map[string]string `json:"files"`         // snapshot, for the diff
	Maintainer   string            `json:"maintainer"`    // maintainer at approval time
	Version      string            `json:"version"`       // approved version
	LastModified int64             `json:"last_modified"` // unix
	ReviewedAt   int64             `json:"reviewed_at"`   // unix
}

// Cache maps pkgbase -> Entry, persisted to disk.
type Cache struct {
	path string
	Pkgs map[string]Entry `json:"pkgs"`
}

// Dir returns caur's cache directory (~/.cache/caur).
func Dir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "caur")
}

// Load loads the cache from disk (empty if absent).
func Load() *Cache {
	c := &Cache{path: filepath.Join(Dir(), "reviews.json"), Pkgs: map[string]Entry{}}
	b, err := os.ReadFile(c.path)
	if err != nil {
		return c
	}
	var stored struct {
		Pkgs map[string]Entry `json:"pkgs"`
	}
	if err := json.Unmarshal(b, &stored); err == nil && stored.Pkgs != nil {
		c.Pkgs = stored.Pkgs
	}
	return c
}

// Hash computes a deterministic fingerprint over the package files.
func Hash(pf aur.PkgFiles) string {
	names := make([]string, 0, len(pf.Files))
	for name := range pf.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	h := sha256.New()
	for _, name := range names {
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write([]byte(pf.Files[name]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Get returns the Entry for a pkgbase, if present.
func (c *Cache) Get(base string) (Entry, bool) {
	e, ok := c.Pkgs[base]
	return e, ok
}

// Put stores/updates the Entry of a pkgbase.
func (c *Cache) Put(base string, e Entry) {
	c.Pkgs[base] = e
}

// Save persists the cache to disk.
func (c *Cache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		Pkgs map[string]Entry `json:"pkgs"`
	}{c.Pkgs}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o644)
}
