// Package review defines the review engine interface and its implementations.
// Reviews are produced by shelling out to an AI command-line tool (see Agent)
// in headless mode: caur ships profiles for Claude, Codex/GPT, Ollama and
// Gemini, and more can be added behind the same interface.
package review

import (
	"context"
	"fmt"
	"strings"

	"caur/internal/aur"
	"caur/internal/config"
)

// Finding is a single security issue found in the package files.
type Finding struct {
	Severity string `json:"severity"` // low | medium | high | critical
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	File     string `json:"file"`
	Evidence string `json:"evidence"`
}

// Result is the structured outcome of reviewing a pkgbase.
type Result struct {
	Verdict  string    `json:"verdict"` // clean | suspicious | malicious
	Score    int       `json:"score"`   // risk 0-100
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Reviewer is the contract for any review backend.
type Reviewer interface {
	// Review analyzes a package's files in full. notes is extra context
	// (e.g. supply-chain signals) to include in the prompt; it may be empty.
	// An error means the review did not complete: the caller applies the
	// fail-closed policy (do not install).
	Review(ctx context.Context, pf aur.PkgFiles, notes string) (Result, error)
	// ReviewDiff analyzes only the changes between an already-approved version
	// (prev) and the new one (cur), assessing whether the changes add risk.
	ReviewDiff(ctx context.Context, prev, cur aur.PkgFiles, notes string) (Result, error)
	// Name identifies the backend, for logs and diagnostics.
	Name() string
}

// New selects the review backend based on the configuration.
func New(cfg config.Config) (Reviewer, error) {
	ag, ok := lookupAgent(cfg.Backend)
	if !ok {
		return nil, fmt.Errorf("unsupported review backend: %s (available: %s)",
			cfg.Backend, strings.Join(backendNames(), ", "))
	}
	if ag.NeedsModel && strings.TrimSpace(cfg.Model) == "" {
		return nil, fmt.Errorf("backend %q requires a model: set `model` in the config (e.g. model = \"llama3.1\")", ag.Backend)
	}
	return &CLIReviewer{agent: ag, Model: cfg.Model}, nil
}

// InteractiveCommand returns the executable and arguments to open the
// configured agent in an interactive session, for hands-on deep inspection of a
// package after the automated review. If seed is non-empty and the agent
// supports it, the session starts pre-seeded with that message (caur's review
// result), so the agent has context instead of starting cold. ok is false if
// the backend is unknown.
func InteractiveCommand(cfg config.Config, seed string) (bin string, args []string, ok bool) {
	ag, found := lookupAgent(cfg.Backend)
	if !found {
		return "", nil, false
	}
	if seed != "" && ag.replSeedArgs != nil {
		return ag.Bin, ag.replSeedArgs(cfg.Model, seed), true
	}
	return ag.Bin, ag.replArgs(cfg.Model), true
}
