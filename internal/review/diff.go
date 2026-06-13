package review

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// unifiedDiff produces a unified diff between the previous and new versions of
// the files, file by file. It uses the system `diff -u`; if unavailable it
// falls back to a raw textual comparison.
func unifiedDiff(prev, cur map[string]string) string {
	names := map[string]bool{}
	for n := range prev {
		names[n] = true
	}
	for n := range cur {
		names[n] = true
	}
	sorted := make([]string, 0, len(names))
	for n := range names {
		sorted = append(sorted, n)
	}
	sort.Strings(sorted)

	var b strings.Builder
	for _, name := range sorted {
		o, hadOld := prev[name]
		n, hasNew := cur[name]
		if o == n {
			continue // unchanged
		}
		switch {
		case !hadOld:
			fmt.Fprintf(&b, "## NEW FILE: %s\n%s\n", name, n)
		case !hasNew:
			fmt.Fprintf(&b, "## REMOVED FILE: %s\n", name)
		default:
			fmt.Fprintf(&b, "## MODIFIED: %s\n%s\n", name, diffTwo(name, o, n))
		}
	}
	if b.Len() == 0 {
		return "(no differences in the relevant files)\n"
	}
	return b.String()
}

// diffTwo returns the unified diff of two versions of a file.
func diffTwo(name, old, new string) string {
	tmp, err := os.MkdirTemp("", "caur-diff-")
	if err != nil {
		return fallbackDiff(old, new)
	}
	defer os.RemoveAll(tmp)

	oldPath := filepath.Join(tmp, "old")
	newPath := filepath.Join(tmp, "new")
	if os.WriteFile(oldPath, []byte(old), 0o600) != nil ||
		os.WriteFile(newPath, []byte(new), 0o600) != nil {
		return fallbackDiff(old, new)
	}

	out, err := exec.Command("diff", "-u",
		"--label", "a/"+name, "--label", "b/"+name,
		oldPath, newPath).Output()
	if len(out) == 0 && err != nil {
		// diff unavailable or a real error (exit code 1 = differences, ok).
		return fallbackDiff(old, new)
	}
	return string(out)
}

// fallbackDiff is a minimal comparison when `diff` is unavailable.
func fallbackDiff(old, new string) string {
	return "--- approved version\n" + old + "\n+++ new version\n" + new + "\n"
}
