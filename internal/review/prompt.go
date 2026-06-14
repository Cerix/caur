package review

import (
	"fmt"
	"sort"
	"strings"

	"caur/internal/aur"
)

// buildPrompt builds the prompt sent to the model: security-auditor
// instructions, the output JSON schema and the package file contents.
func buildPrompt(pf aur.PkgFiles, notes string) string {
	var b strings.Builder

	b.WriteString(`You are a security auditor for Arch Linux AUR packages.
Analyze the files below (PKGBUILD, .install, .SRCINFO, patches, scripts) and
find any malicious or suspicious behavior. Pay attention to:

- pipes to a shell (curl/wget ... | bash/sh), downloading and executing
  unverified remote code;
- suspicious URLs/IPs or domains unrelated to the upstream project;
- obfuscation of any kind: base64/hex/octal-escaped strings (\xNN, \NNN),
  string concatenation that hides a command, eval, printf-built commands,
  reversed strings, character arithmetic — treat obfuscation in build or
  install code as malicious until proven benign;
- .install scriptlets (pre_install/post_install/pre_upgrade/post_upgrade/
  pre_remove/post_remove): these run AS ROOT on the user's machine, so scrutinize
  them especially — any network access, eval, obfuscation or system modification
  there is high severity;
- data exfiltration (sending files, environment variables, keys, ~/.ssh);
- writes outside the build directory ($srcdir/$pkgdir), changes to system files,
  /etc, systemd units, crontab, autostart files;
- sudo/privilege escalation;
- cryptocurrency miners, backdoors, persistence;
- disabled checksums (sha256sums=SKIP) on remote non-VCS sources.

Treat as "clean" a PKGBUILD that only performs a legitimate build/install from
the declared upstream source. Do not flag normal packaging practices.

Reply with ONLY a valid JSON object, no text before or after, no markdown,
matching this schema:

{
  "verdict": "clean" | "suspicious" | "malicious",
  "score": <integer 0-100, overall risk>,
  "summary": "<short summary in English>",
  "findings": [
    {
      "severity": "low" | "medium" | "high" | "critical",
      "title": "<short title>",
      "detail": "<explanation>",
      "file": "<file name>",
      "evidence": "<offending line or snippet>"
    }
  ]
}

If you find nothing suspicious: verdict "clean", low score, findings [].

`)

	fmt.Fprintf(&b, "=== Package: %s ===\n\n", pf.PkgBase)
	writeNotes(&b, notes)
	writeFiles(&b, pf)
	return b.String()
}

// buildDiffPrompt asks the model to assess only the changes between the
// already-approved version and the new one. The output schema is identical, but
// the verdict and score refer to the risk introduced BY THE CHANGES.
func buildDiffPrompt(prev, cur aur.PkgFiles, notes string) string {
	var b strings.Builder

	b.WriteString(`You are a security auditor for Arch Linux AUR packages.
A PREVIOUS version of this package was already reviewed and approved. Below is
the unified DIFF to the new version of the files (PKGBUILD, .install, .SRCINFO,
patches, scripts). Assess whether the CHANGES introduce new security risks. Look
in particular for: new downloads/pipes to a shell, new URLs/IPs, new sources,
obfuscation (base64/hex/octal escapes, eval, string fragmentation), exfiltration,
writes outside the build dir, added or modified .install scriptlets (which run as
root: pre/post install/upgrade/remove), privilege escalation, miners, backdoors,
disabled checksums.

Treat as "clean" a diff that only contains legitimate updates (version bump,
consistent new checksums, packaging tweaks). The score and verdict refer to the
overall risk considering the introduced changes.

Reply with ONLY a valid JSON object (same schema as the full review: verdict,
score, summary, findings[]), with no text or markdown around it.

`)

	fmt.Fprintf(&b, "=== Package: %s ===\n\n", cur.PkgBase)
	writeNotes(&b, notes)
	b.WriteString("----- DIFF (approved version -> new) -----\n")
	b.WriteString(unifiedDiff(prev.Files, cur.Files))
	b.WriteString("----- END DIFF -----\n\n")
	return b.String()
}

func writeNotes(b *strings.Builder, notes string) {
	if strings.TrimSpace(notes) == "" {
		return
	}
	b.WriteString("CONTEXT (signals to consider in the assessment):\n")
	b.WriteString(notes)
	if !strings.HasSuffix(notes, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeFiles(b *strings.Builder, pf aur.PkgFiles) {
	// Sort the names for deterministic output (also useful for the cache).
	names := make([]string, 0, len(pf.Files))
	for name := range pf.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(b, "----- FILE: %s -----\n", name)
		b.WriteString(pf.Files[name])
		if !strings.HasSuffix(pf.Files[name], "\n") {
			b.WriteString("\n")
		}
		b.WriteString("----- END FILE -----\n\n")
	}
}
