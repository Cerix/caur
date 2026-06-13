// Package review definisce l'interfaccia del motore di review e le sue
// implementazioni. L'MVP usa il CLI `claude` in modalità headless; altri
// backend (API Anthropic, OpenAI, Ollama) possono essere aggiunti dietro la
// stessa interfaccia.
package review

import (
	"context"

	"caur/internal/aur"
	"caur/internal/config"
)

// Finding è un singolo rilievo di sicurezza individuato nei file del pacchetto.
type Finding struct {
	Severity string `json:"severity"` // low | medium | high | critical
	Title    string `json:"title"`
	Detail   string `json:"detail"`
	File     string `json:"file"`
	Evidence string `json:"evidence"`
}

// Result è l'esito strutturato della review di un pkgbase.
type Result struct {
	Verdict  string    `json:"verdict"` // clean | suspicious | malicious
	Score    int       `json:"score"`   // rischio 0-100
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
}

// Reviewer è il contratto per qualunque backend di review.
type Reviewer interface {
	// Review analizza per intero i file di un pacchetto. notes è contesto
	// aggiuntivo (es. segnali supply-chain) da includere nel prompt; può essere
	// vuoto. Un errore indica review non completata: il chiamante applica la
	// politica fail-closed (non installare).
	Review(ctx context.Context, pf aur.PkgFiles, notes string) (Result, error)
	// ReviewDiff analizza solo le modifiche tra una versione già approvata
	// (prev) e quella nuova (cur), valutando se le modifiche introducono rischi.
	ReviewDiff(ctx context.Context, prev, cur aur.PkgFiles, notes string) (Result, error)
	// Name identifica il backend, per log e diagnostica.
	Name() string
}

// New seleziona il backend in base alla configurazione.
func New(cfg config.Config) (Reviewer, error) {
	switch cfg.Backend {
	case "", "claude-cli":
		return &ClaudeCLIReviewer{Model: cfg.Model}, nil
	default:
		return nil, errUnsupportedBackend(cfg.Backend)
	}
}

type unsupportedBackend string

func (b unsupportedBackend) Error() string {
	return "backend di review non supportato: " + string(b) +
		" (disponibile: claude-cli)"
}

func errUnsupportedBackend(name string) error { return unsupportedBackend(name) }
