// Package config carica la configurazione di caur da ~/.config/caur/config.toml.
//
// Per restare senza dipendenze esterne usa un parser minimale che supporta le
// chiavi piatte di cui abbiamo bisogno: stringhe, booleani, interi e array di
// stringhe. Se il file non esiste vengono usati i default.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config raccoglie le opzioni di caur.
type Config struct {
	Backend          string   // claude-cli (default) | anthropic | ollama (futuro)
	Model            string   // model alias per il backend; "" = default del CLI
	BlockThreshold   int      // n. di finding significativi (medium+) che blocca (>=)
	AutoApproveClean bool     // se true, verdetto "clean" procede senza prompt
	CacheReviews     bool     // riusa le review per PKGBUILD invariati
	DiffReview       bool     // su update, revisiona solo le modifiche al PKGBUILD
	MaintainerChange bool     // segnala/blocca al cambio di maintainer o se orfano
	TrustedPackages  []string // pkgbase in allowlist: saltano la review
	YayPath          string   // path/eseguibile del motore AUR sottostante
}

// Default restituisce la configurazione di base.
func Default() Config {
	return Config{
		Backend:          "claude-cli",
		Model:            "", // usa il modello con cui il CLI claude è loggato
		BlockThreshold:   1,
		AutoApproveClean: true,
		CacheReviews:     true,
		DiffReview:       true,
		MaintainerChange: true,
		TrustedPackages:  nil,
		YayPath:          "yay",
	}
}

// Path restituisce il percorso del file di config.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "caur", "config.toml")
}

// Load legge la config dal disco, applicando i default per le chiavi assenti.
func Load() Config {
	cfg := Default()
	f, err := os.Open(Path())
	if err != nil {
		return cfg // file assente o non leggibile: usa i default
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "backend":
			cfg.Backend = unquote(val)
		case "model":
			cfg.Model = unquote(val)
		case "block_threshold":
			if n, err := strconv.Atoi(val); err == nil {
				cfg.BlockThreshold = n
			}
		case "auto_approve_clean":
			cfg.AutoApproveClean = val == "true"
		case "cache_reviews":
			cfg.CacheReviews = val == "true"
		case "diff_review":
			cfg.DiffReview = val == "true"
		case "maintainer_change":
			cfg.MaintainerChange = val == "true"
		case "trusted_packages":
			cfg.TrustedPackages = parseStringArray(val)
		case "yay_path":
			cfg.YayPath = unquote(val)
		}
	}
	return cfg
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// parseStringArray interpreta una forma del tipo ["a", "b"] in []string.
func parseStringArray(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[")
	s = strings.TrimSuffix(s, "]")
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = unquote(strings.TrimSpace(part))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
