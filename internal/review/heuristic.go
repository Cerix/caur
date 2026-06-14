package review

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"caur/internal/aur"
)

// Heuristics performs a fast, deterministic scan of the package files for a few
// high-confidence malware patterns. It is a backstop to the AI review: it runs
// offline and flags the kind of obfuscation seen in real AUR compromises (e.g.
// hex/octal-escaped commands hidden in .install scriptlets) even if the model
// misses them. The returned note is added to the prompt so the model focuses on
// the same spots.
//
// Patterns are intentionally narrow to keep false positives low: each one is
// rare in legitimate packaging.
func Heuristics(pf aur.PkgFiles) (findings []Finding, note string) {
	names := make([]string, 0, len(pf.Files))
	for name := range pf.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	var noteB strings.Builder
	for _, name := range names {
		content := pf.Files[name]
		install := isInstallScript(name)
		for _, f := range scanContent(name, content, install) {
			findings = append(findings, f)
			fmt.Fprintf(&noteB, "- Heuristic flag in %s: %s.\n", f.File, f.Title)
		}
	}
	return findings, noteB.String()
}

// isInstallScript reports whether a file is a pacman .install scriptlet, whose
// hooks run as root — anything suspicious there is more severe.
func isInstallScript(name string) bool {
	return strings.HasSuffix(name, ".install")
}

var (
	reHexEsc      = regexp.MustCompile(`\\x[0-9a-fA-F]{2}`)
	reOctEsc      = regexp.MustCompile(`\\[0-7]{3}`)
	rePipeToShell = regexp.MustCompile(`(?i)\b(curl|wget|fetch)\b[^\n|]*\|\s*(sudo\s+)?(bash|sh|zsh|python[0-9.]*)\b`)
	reBase64Exec  = regexp.MustCompile(`(?i)base64\s+(-d|--decode|-D)\b[^\n]*\|\s*(bash|sh|zsh|python[0-9.]*)\b`)
	reEvalDecode  = regexp.MustCompile(`(?i)eval\b[^\n]*(base64|\\x[0-9a-fA-F]{2}|xxd|openssl\s+enc|tr\s+)`)
	// Redirection or tee into an absolute system path. A leading $pkgdir/$srcdir
	// quote would put the variable (not a slash) right after the operator, so a
	// bare "/etc", "/usr", ... after > / >> / tee means a write to the live
	// system, which legitimate packaging never does (it writes under $pkgdir).
	reSysWrite = regexp.MustCompile(`(?i)(>>?|\btee\b(\s+-a)?)\s*["']?/(etc|usr|bin|sbin|boot|lib|lib64|opt|srv|root|home|var/spool/cron)\b`)
	reCrontab  = regexp.MustCompile(`(?i)\bcrontab\b`)
)

// scanContent applies the patterns to one file. install raises the severity of
// findings, since .install hooks execute as root during package operations.
func scanContent(name, content string, install bool) []Finding {
	var out []Finding
	bump := func(base string) string {
		if install {
			return "critical"
		}
		return base
	}

	if m := reHexEsc.FindAllString(content, -1); len(m) >= 4 {
		out = append(out, Finding{
			Severity: bump("high"), File: name, Title: "Hex-escaped string obfuscation",
			Detail:   "Multiple \\xNN hex escapes are used to assemble strings/commands at runtime, a common way to hide a malicious command from a casual reader.",
			Evidence: snippet(content, reHexEsc),
		})
	}
	if m := reOctEsc.FindAllString(content, -1); len(m) >= 4 {
		out = append(out, Finding{
			Severity: bump("high"), File: name, Title: "Octal-escaped string obfuscation",
			Detail:   "Multiple \\NNN octal escapes are used to assemble strings/commands at runtime, a common obfuscation technique.",
			Evidence: snippet(content, reOctEsc),
		})
	}
	if rePipeToShell.MatchString(content) {
		out = append(out, Finding{
			Severity: bump("high"), File: name, Title: "Remote code piped to a shell",
			Detail:   "Downloaded content is piped straight into a shell interpreter, executing unverified remote code.",
			Evidence: snippet(content, rePipeToShell),
		})
	}
	if reBase64Exec.MatchString(content) {
		out = append(out, Finding{
			Severity: bump("high"), File: name, Title: "Base64-decoded payload executed",
			Detail:   "A base64-decoded blob is piped into an interpreter, a typical way to smuggle a hidden payload.",
			Evidence: snippet(content, reBase64Exec),
		})
	}
	if reEvalDecode.MatchString(content) {
		out = append(out, Finding{
			Severity: bump("high"), File: name, Title: "eval of decoded/obfuscated data",
			Detail:   "eval is applied to decoded or escaped data, executing commands hidden from a plain reading of the file.",
			Evidence: snippet(content, reEvalDecode),
		})
	}
	if reSysWrite.MatchString(content) {
		out = append(out, Finding{
			Severity: bump("high"), File: name, Title: "Writes to a system path outside $pkgdir",
			Detail:   "Output is redirected into a live system location (e.g. /etc, /usr). Legitimate packaging only writes under $pkgdir/$srcdir; writing to the real system bypasses pacman's file tracking and is a common persistence/tampering vector (shell rc files, /etc/profile.d, autostart).",
			Evidence: snippet(content, reSysWrite),
		})
	}
	if reCrontab.MatchString(content) {
		out = append(out, Finding{
			Severity: bump("medium"), File: name, Title: "Modifies crontab",
			Detail:   "The package touches cron, which can schedule code to run later. This is unusual for a build/install and a known persistence vector.",
			Evidence: snippet(content, reCrontab),
		})
	}
	return out
}

// snippet returns the first line matching re, trimmed, for the finding evidence.
func snippet(content string, re *regexp.Regexp) string {
	loc := re.FindStringIndex(content)
	if loc == nil {
		return ""
	}
	start := strings.LastIndexByte(content[:loc[0]], '\n') + 1
	end := strings.IndexByte(content[loc[1]:], '\n')
	if end < 0 {
		end = len(content)
	} else {
		end += loc[1]
	}
	line := strings.TrimSpace(content[start:end])
	if len(line) > 200 {
		line = line[:200] + "…"
	}
	return line
}
