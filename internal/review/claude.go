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

// ClaudeCLIReviewer uses the `claude` CLI in headless mode (-p) as the review
// engine. It needs no API key handling: it relies on the existing login.
type ClaudeCLIReviewer struct {
	Model string // model alias; "" uses the CLI default
}

func (r *ClaudeCLIReviewer) Name() string { return "claude-cli" }

// claudeEnvelope is the JSON envelope produced by `claude --output-format json`.
type claudeEnvelope struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

func (r *ClaudeCLIReviewer) Review(ctx context.Context, pf aur.PkgFiles, notes string) (Result, error) {
	return r.run(ctx, buildPrompt(pf, notes))
}

func (r *ClaudeCLIReviewer) ReviewDiff(ctx context.Context, prev, cur aur.PkgFiles, notes string) (Result, error) {
	return r.run(ctx, buildDiffPrompt(prev, cur, notes))
}

// run sends a prompt to the claude CLI and decodes the structured outcome.
func (r *ClaudeCLIReviewer) run(ctx context.Context, prompt string) (Result, error) {
	args := []string{"-p", prompt, "--output-format", "json"}
	if r.Model != "" {
		args = append(args, "--model", r.Model)
	}

	cmd := exec.CommandContext(ctx, "claude", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Result{}, fmt.Errorf("running claude: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	text, err := extractResultText(stdout.Bytes())
	if err != nil {
		return Result{}, err
	}

	res, err := parseResult(text)
	if err != nil {
		return Result{}, fmt.Errorf("parse review outcome: %w (response: %s)", err, truncate(text, 400))
	}
	return res, nil
}

// extractResultText pulls the model text out of the CLI envelope. If the output
// is not the expected envelope, it tries to use it directly.
func extractResultText(out []byte) (string, error) {
	var env claudeEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(out), &env); err == nil && env.Result != "" {
		if env.IsError {
			return "", fmt.Errorf("claude returned an error: %s", truncate(env.Result, 400))
		}
		return env.Result, nil
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("empty response from claude")
	}
	return s, nil
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
