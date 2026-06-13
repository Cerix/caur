// Package config loads caur's configuration from ~/.config/caur/config.toml.
//
// To stay dependency-free it uses a minimal parser supporting the flat keys we
// need: strings, booleans, integers and string arrays. If the file does not
// exist, defaults are used.
package config

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds caur's options.
type Config struct {
	Backend          string   // claude-cli (default) | anthropic | ollama (future)
	Model            string   // model alias for the backend; "" = CLI default
	BlockThreshold   int      // number of significant (medium+) findings that block (>=)
	AutoApproveClean bool     // if true, a "clean" verdict proceeds without prompting
	CacheReviews     bool     // reuse reviews for unchanged PKGBUILDs
	DiffReview       bool     // on updates, review only the PKGBUILD changes
	MaintainerChange bool     // flag/block on maintainer change or orphaned package
	TrustedPackages  []string // pkgbase allowlist: skip review
	YayPath          string   // path/executable of the underlying AUR engine
}

// Default returns the base configuration.
func Default() Config {
	return Config{
		Backend:          "claude-cli",
		Model:            "", // use the model the claude CLI is logged in with
		BlockThreshold:   1,
		AutoApproveClean: true,
		CacheReviews:     true,
		DiffReview:       true,
		MaintainerChange: true,
		TrustedPackages:  nil,
		YayPath:          "yay",
	}
}

// Path returns the location of the config file.
func Path() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "caur", "config.toml")
}

// Load reads the config from disk, applying defaults for missing keys.
func Load() Config {
	cfg := Default()
	f, err := os.Open(Path())
	if err != nil {
		return cfg // file missing or unreadable: use defaults
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

// parseStringArray parses a form like ["a", "b"] into a []string.
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
