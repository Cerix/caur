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

// CLIReviewer drives an AI command-line tool (see Agent) in headless mode as
// the review engine. It needs no API key handling: it relies on the agent's
// existing login/configuration.
type CLIReviewer struct {
	agent Agent
	Model string // model alias; "" uses the agent default (where supported)
}

func (r *CLIReviewer) Name() string { return r.agent.Backend }

func (r *CLIReviewer) Review(ctx context.Context, pf aur.PkgFiles, notes string) (Result, error) {
	return r.run(ctx, buildPrompt(pf, notes))
}

func (r *CLIReviewer) ReviewDiff(ctx context.Context, prev, cur aur.PkgFiles, notes string) (Result, error) {
	return r.run(ctx, buildDiffPrompt(prev, cur, notes))
}

// run sends a prompt to the agent CLI and decodes the structured outcome.
func (r *CLIReviewer) run(ctx context.Context, prompt string) (Result, error) {
	cmd := exec.CommandContext(ctx, r.agent.Bin, r.agent.promptArgs(r.Model, prompt)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("running %s: %w: %s", r.agent.Bin, err, strings.TrimSpace(stderr.String()))
	}

	text, err := r.agent.parse(stdout.Bytes())
	if err != nil {
		return Result{}, err
	}

	res, err := parseResult(text)
	if err != nil {
		return Result{}, fmt.Errorf("parse review outcome: %w (response: %s)", err, truncate(text, 400))
	}
	return res, nil
}

// parseResult extracts our schema's JSON object from the model text, tolerating
// ```json fences or surrounding text.
func parseResult(text string) (Result, error) {
	jsonStr := extractJSONObject(text)
	if jsonStr == "" {
		return Result{}, fmt.Errorf("no JSON object found")
	}
	var res Result
	if err := json.Unmarshal([]byte(jsonStr), &res); err != nil {
		return Result{}, err
	}
	res.Verdict = strings.ToLower(strings.TrimSpace(res.Verdict))
	if res.Verdict == "" {
		res.Verdict = "suspicious" // caution: with no verdict, don't trust it
	}
	return res, nil
}

// extractJSONObject returns the first balanced JSON object in the text
// (handling strings so braces inside them are not miscounted).
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
