// Package policy traduce l'esito di una review in una decisione, secondo la
// configurazione. Politica: il punteggio è sempre disponibile; qualsiasi
// pacchetto non "clean" (o con abbastanza finding) viene bloccato e richiede
// conferma esplicita dell'utente.
package policy

import (
	"strings"

	"caur/internal/config"
	"caur/internal/review"
)

// Decision è l'esito della valutazione per un singolo pacchetto.
type Decision struct {
	Allow       bool   // true: può procedere senza conferma
	NeedConfirm bool   // true: bloccato, serve conferma esplicita
	Reason      string // motivo sintetico
}

// Evaluate applica la politica a un esito di review. Bloccano: un verdetto non
// "clean", oppure un numero di findings *significativi* (severità medium o
// superiore) >= block_threshold. I rilievi low/info da soli non bloccano.
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

// isSignificant indica se una severità concorre al blocco.
func isSignificant(severity string) bool {
	switch strings.ToLower(strings.TrimSpace(severity)) {
	case "medium", "high", "critical":
		return true
	}
	return false
}

// Blocked indica se almeno una decisione richiede conferma.
func Blocked(ds []Decision) bool {
	for _, d := range ds {
		if d.NeedConfirm {
			return true
		}
	}
	return false
}
