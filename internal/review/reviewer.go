// Package review defines the review engine interface and its implementations.
// The MVP uses the `claude` CLI in headless mode; other backends (Anthropic
// API, OpenAI, Ollama) can be added behind the same interface.
package review

import (
	"context"

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

// New selects the backend based on the configuration.
func New(cfg config.Config) (Reviewer, error) {
	switch cfg.Backend {
	case "", "claude-cli":
		return &ClaudeCLIReviewer{Model: cfg.Model}, nil
	default:
		return nil, errUnsupportedBackend(cfg.Backend)
	}
}

type unsupportedBackend string

func (b unsupportedBackend) Error() string {
	return "unsupported review backend: " + string(b) +
		" (available: claude-cli)"
}

func errUnsupportedBackend(name string) error { return unsupportedBackend(name) }
