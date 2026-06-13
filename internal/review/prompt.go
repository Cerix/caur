package review

import (
	"fmt"
	"sort"
	"strings"

	"caur/internal/aur"
)

// buildPrompt costruisce il prompt da inviare al modello: istruzioni da auditor
// di sicurezza, schema JSON di output e contenuto dei file del pacchetto.
func buildPrompt(pf aur.PkgFiles) string {
	var b strings.Builder

	b.WriteString(`Sei un auditor di sicurezza per pacchetti AUR di Arch Linux.
Analizza i file qui sotto (PKGBUILD, .install, .SRCINFO, patch, script) e
individua qualsiasi comportamento malevolo o sospetto. Presta attenzione a:

- pipe verso shell (curl/wget ... | bash/sh), download ed esecuzione di codice
  remoto non verificato;
- URL/IP sospetti o domini non correlati al progetto upstream;
- offuscamento: base64/hex/eval, stringhe codificate, comandi nascosti;
- esfiltrazione di dati (invio di file, variabili d'ambiente, chiavi);
- scritture fuori dalla directory di build ($srcdir/$pkgdir), modifiche a file
  di sistema, /etc, unità systemd, crontab, file di autostart;
- uso di sudo/escalation e hook .install (pre/post install/upgrade/remove);
- miner di criptovalute, backdoor, persistenza;
- checksum disabilitati (sha256sums=SKIP) su sorgenti remote non-VCS.

Considera "clean" un PKGBUILD che fa solo build/install legittimi dal sorgente
upstream dichiarato. Non segnalare prassi normali di packaging.

Rispondi ESCLUSIVAMENTE con un oggetto JSON valido, senza testo prima o dopo,
senza markdown, con questo schema:

{
  "verdict": "clean" | "suspicious" | "malicious",
  "score": <intero 0-100, rischio complessivo>,
  "summary": "<breve sintesi in italiano>",
  "findings": [
    {
      "severity": "low" | "medium" | "high" | "critical",
      "title": "<titolo breve>",
      "detail": "<spiegazione>",
      "file": "<nome file>",
      "evidence": "<riga o frammento incriminato>"
    }
  ]
}

Se non trovi nulla di sospetto: verdict "clean", score basso, findings [].

`)

	fmt.Fprintf(&b, "=== Pacchetto: %s ===\n\n", pf.PkgBase)

	// Ordina i nomi per un output deterministico (utile anche per la cache).
	names := make([]string, 0, len(pf.Files))
	for name := range pf.Files {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fmt.Fprintf(&b, "----- FILE: %s -----\n", name)
		b.WriteString(pf.Files[name])
		if !strings.HasSuffix(pf.Files[name], "\n") {
			b.WriteString("\n")
		}
		b.WriteString("----- FINE FILE -----\n\n")
	}

	return b.String()
}
