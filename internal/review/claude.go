package review

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"caur/internal/aur"
)

// ClaudeCLIReviewer usa il CLI `claude` in modalità headless (-p) come motore
// di review. Non richiede gestione di API key: sfrutta il login esistente.
type ClaudeCLIReviewer struct {
	Model string // alias modello; "" usa il default del CLI
}

func (r *ClaudeCLIReviewer) Name() string { return "claude-cli" }

// claudeEnvelope è l'involucro JSON prodotto da `claude --output-format json`.
type claudeEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

func (r *ClaudeCLIReviewer) Review(ctx context.Context, pf aur.PkgFiles) (Result, error) {
	prompt := buildPrompt(pf)

	args := []string{"-p", prompt, "--output-format", "json"}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("esecuzione claude: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	text, err := extractResultText(stdout.Bytes())
	if err != nil {
		return Result{}, err
	}

	res, err := parseResult(text)
	if err != nil {
		return Result{}, fmt.Errorf("parse esito review: %w (risposta: %s)", err, truncate(text, 400))
	}
	return res, nil
}

// extractResultText ricava il testo del modello dall'involucro del CLI. Se
// l'output non è l'involucro atteso, prova a usarlo direttamente.
func extractResultText(out []byte) (string, error) {
	var env claudeEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(out), &env); err == nil && env.Result != "" {
		if env.IsError {
			return "", fmt.Errorf("claude ha restituito un errore: %s", truncate(env.Result, 400))
		}
		return env.Result, nil
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("risposta vuota da claude")
	}
	return s, nil
}

// parseResult estrae l'oggetto JSON del nostro schema dal testo del modello,
// tollerando eventuali fence ```json o testo attorno.
func parseResult(text string) (Result, error) {
	jsonStr := extractJSONObject(text)
	if jsonStr == "" {
		return Result{}, fmt.Errorf("nessun oggetto JSON trovato")
	}
	var res Result
	if err := json.Unmarshal([]byte(jsonStr), &res); err != nil {
		return Result{}, err
	}
	res.Verdict = strings.ToLower(strings.TrimSpace(res.Verdict))
	if res.Verdict == "" {
		res.Verdict = "suspicious" // prudenza: senza verdetto, non fidarsi
	}
	return res, nil
}

// extractJSONObject restituisce il primo oggetto JSON bilanciato presente nel
// testo (gestendo le stringhe per non confondere le graffe).
func extractJSONObject(text string) string {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(text); i++ {
		c := text[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
