package review

import (
	"fmt"
	"sort"
	"strings"

	"caur/internal/aur"
)

// buildPrompt costruisce il prompt da inviare al modello: istruzioni da auditor
// di sicurezza, schema JSON di output e contenuto dei file del pacchetto.
func buildPrompt(pf aur.PkgFiles, notes string) string {
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
	writeNotes(&b, notes)
	writeFiles(&b, pf)
	return b.String()
}

// buildDiffPrompt chiede di valutare solo le modifiche tra la versione già
// approvata e quella nuova. Lo schema di output è identico, ma il verdetto e lo
// score si riferiscono al rischio introdotto DALLE MODIFICHE.
func buildDiffPrompt(prev, cur aur.PkgFiles, notes string) string {
	var b strings.Builder

	b.WriteString(`Sei un auditor di sicurezza per pacchetti AUR di Arch Linux.
Una versione PRECEDENTE di questo pacchetto era già stata revisionata e
approvata. Qui sotto trovi il DIFF unificato verso la nuova versione dei file
(PKGBUILD, .install, .SRCINFO, patch, script). Valuta se le MODIFICHE
introducono nuovi rischi di sicurezza. Cerca in particolare: nuovi download/pipe
verso shell, nuovi URL/IP, nuove fonti, offuscamento, esfiltrazione, scritture
fuori dalla build dir, hook .install aggiunti/modificati, escalation, miner,
backdoor, checksum disabilitati.

Considera "clean" un diff che contiene solo aggiornamenti legittimi (bump di
versione, nuovi checksum coerenti, ritocchi di packaging). Lo score e il verdict
si riferiscono al rischio complessivo considerando le modifiche introdotte.

Rispondi ESCLUSIVAMENTE con un oggetto JSON valido (stesso schema della review
completa: verdict, score, summary, findings[]), senza testo o markdown attorno.

`)

	fmt.Fprintf(&b, "=== Pacchetto: %s ===\n\n", cur.PkgBase)
	writeNotes(&b, notes)
	b.WriteString("----- DIFF (versione approvata -> nuova) -----\n")
	b.WriteString(unifiedDiff(prev.Files, cur.Files))
	b.WriteString("----- FINE DIFF -----\n\n")
	return b.String()
}

func writeNotes(b *strings.Builder, notes string) {
	if strings.TrimSpace(notes) == "" {
		return
	}
	b.WriteString("CONTESTO (segnali da considerare nella valutazione):\n")
	b.WriteString(notes)
	if !strings.HasSuffix(notes, "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeFiles(b *strings.Builder, pf aur.PkgFiles) {
	// Ordina i nomi per un output deterministico (utile anche per la cache).
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
		b.WriteString("----- FINE FILE -----\n\n")
	}
}
