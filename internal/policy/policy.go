// Package policy turns a review outcome into a decision, per the
// configuration. Policy: the score is always available; any package that is not
// "clean" (or has enough findings) is blocked and requires explicit user
// confirmation.
package policy

import (
	"strings"

	"caur/internal/config"
	"caur/internal/review"
)

// Decision is the evaluation outcome for a single package.
type Decision struct {
	Allow       bool   // true: may proceed without confirmation
	NeedConfirm bool   // true: blocked, explicit confirmation required
	Reason      string // short reason
}

// Evaluate applies the policy to a review outcome. Blocking conditions: a
// verdict other than "clean", or a number of *significant* findings (severity
// medium or higher) >= block_threshold. low/info findings alone do not block.
func Evaluate(r review.Result, cfg config.Config) Decision {
	clean := r.Verdict == "clean"
	significant := 0
	for _, f := range r.Findings {
		if isSignificant(f.Severity) {
			significant++
		}
	}

	if clean && significant < cfg.BlockThreshold && cfg.AutoApproveClean {
		return Decision{Allow: true, Reason: "clean"}
	}
	return Decision{
		NeedConfirm: true,
		Reason:      r.Verdict,
	}
}

// isSignificant reports whether a severity counts toward blocking.
func isSignificant(severity string) bool {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "medium", "high", "critical":
		return true
	}
	return false
}

// Blocked reports whether at least one decision requires confirmation.
func Blocked(ds []Decision) bool {
	for _, d := range ds {
		if d.NeedConfirm {
			return true
		}
	}
	return false
}
