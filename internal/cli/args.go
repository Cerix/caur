// Package cli classifies pacman/yay-style arguments to decide whether an
// operation needs a review (install/upgrade of AUR packages) or should be
// passed unchanged to yay (search, query, removal, ...).
package cli

import "strings"

// Op is the kind of requested operation.
type Op int

const (
	// OpPassthrough: no review, run yay with the same arguments.
	OpPassthrough Op = iota
	// OpInstall: install of explicit packages -> review before installing.
	OpInstall
	// OpUpgrade: system upgrade -> review the changed AUR PKGBUILDs.
	OpUpgrade
)

// Parsed is the result of classification.
type Parsed struct {
	Op      Op
	Targets []string // explicit package names (without flags)
	Args    []string // arguments to pass to yay (possibly rewritten)
}

// Classify analyzes the command-line arguments.
func Classify(args []string) Parsed {
	// Uninstall: `-Uni`/`--uninstall` is a caur alias (not a valid pacman op)
	// rewritten into a full yay removal. No review needed.
	if rewritten, ok := rewriteUninstall(args); ok {
		return Parsed{Op: OpPassthrough, Args: rewritten}
	}

	var targets []string
	letters := map[rune]bool{}
	long := map[string]bool{}

	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--"):
			long[a] = true
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, c := range a[1:] {
				letters[c] = true
			}
		default:
			targets = append(targets, a)
		}
	}

	isSync := letters['S'] || long["--sync"]

	// Not a sync operation: bare words -> search; everything else passes through.
	if !isSync {
		if len(targets) > 0 && len(targets) == len(args) {
			// Only bare words: treat them as a search so the review isn't bypassed.
			return Parsed{Op: OpPassthrough, Args: append([]string{"-Ss"}, targets...)}
		}
		return Parsed{Op: OpPassthrough, Args: args}
	}

	// Read-only sync sub-operations: passthrough.
	readOnly := letters['s'] || long["--search"] ||
		letters['i'] || long["--info"] ||
		letters['c'] || long["--clean"] ||
		letters['l'] || long["--list"] ||
		letters['g'] || long["--groups"]
	if readOnly {
		return Parsed{Op: OpPassthrough, Args: args}
	}

	upgrade := letters['u'] || long["--sysupgrade"]
	if upgrade {
		return Parsed{Op: OpUpgrade, Targets: targets, Args: args}
	}
	if len(targets) > 0 {
		return Parsed{Op: OpInstall, Targets: targets, Args: args}
	}
	// e.g. -Sy (only refresh the db): no package to review.
	return Parsed{Op: OpPassthrough, Args: args}
}

// rewriteUninstall recognizes caur's uninstall alias (`-Uni` or `--uninstall`)
// and rewrites it into `yay -Rns <targets>`, preserving the other arguments
// (e.g. --noconfirm). Returns ok=false if the alias is not present.
func rewriteUninstall(args []string) ([]string, bool) {
	found := false
	out := []string{"-Rns"}
	for _, a := range args {
		if a == "-Uni" || a == "--uninstall" {
			found = true
			continue
		}
		out = append(out, a)
	}
	if !found {
		return nil, false
	}
	return out, true
}
