// Package cache memorizza, per ogni pkgbase, l'ultimo esito di review approvato
// insieme allo snapshot dei file e ai metadati supply-chain (maintainer,
// versione, data di modifica). Serve a:
//   - saltare la review quando i file non sono cambiati (hash identico);
//   - fare una review "diff-only" rispetto all'ultima versione approvata;
//   - rilevare il cambio di maintainer rispetto all'ultima volta.
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

// Entry è il record persistente per un pkgbase.
type Entry struct {
	Hash         string            `json:"hash"`          // hash dei file revisionati
	Result       review.Result     `json:"result"`        // esito (senza i findings supply-chain)
	Files        map[string]string `json:"files"`         // snapshot, per il diff
	Maintainer   string            `json:"maintainer"`    // maintainer al momento dell'approvazione
	Version      string            `json:"version"`       // versione approvata
	LastModified int64             `json:"last_modified"` // unix
	ReviewedAt   int64             `json:"reviewed_at"`   // unix
}

// Cache mappa pkgbase -> Entry, persistita su disco.
type Cache struct {
	path string
	Pkgs map[string]Entry `json:"pkgs"`
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

// Load carica la cache da disco (vuota se assente).
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

// Get restituisce l'Entry per un pkgbase, se presente.
func (c *Cache) Get(base string) (Entry, bool) {
	e, ok := c.Pkgs[base]
	return e, ok
}

// Put memorizza/aggiorna l'Entry di un pkgbase.
func (c *Cache) Put(base string, e Entry) {
	c.Pkgs[base] = e
}

// Save persiste la cache su disco.
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
