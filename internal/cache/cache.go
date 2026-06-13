// Package cache memorizza gli esiti delle review indicizzati dall'hash dei file
// del pacchetto, così da non rivolgersi al modello quando il PKGBUILD non è
// cambiato (tipicamente sugli upgrade).
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

// Cache è una mappa persistente hash -> esito review.
type Cache struct {
	path    string
	Entries map[string]review.Result `json:"entries"`
}

// Dir restituisce la directory di cache di caur (~/.cache/caur).
func Dir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, "caur")
}

// Load carica la cache delle review da disco (vuota se assente).
func Load() *Cache {
	c := &Cache{path: filepath.Join(Dir(), "reviews.json"), Entries: map[string]review.Result{}}
	b, err := os.ReadFile(c.path)
	if err != nil {
		return c
	}
	var stored struct {
		Entries map[string]review.Result `json:"entries"`
	}
	if err := json.Unmarshal(b, &stored); err == nil && stored.Entries != nil {
		c.Entries = stored.Entries
	}
	return c
}

// Hash calcola un'impronta deterministica sui file del pacchetto.
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

// Get restituisce l'esito memorizzato per un hash, se presente.
func (c *Cache) Get(hash string) (review.Result, bool) {
	r, ok := c.Entries[hash]
	return r, ok
}

// Put memorizza un esito per un hash.
func (c *Cache) Put(hash string, r review.Result) {
	c.Entries[hash] = r
}

// Save persiste la cache su disco.
func (c *Cache) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(struct {
		Entries map[string]review.Result `json:"entries"`
	}{c.Entries}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.path, b, 0o644)
}
