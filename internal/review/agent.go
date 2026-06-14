package review

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Agent describes how to drive a particular AI command-line tool as a review
// backend. caur shells out to the agent's own CLI in headless mode, so it
// reuses the user's existing login/configuration and needs no API keys of its
// own. New agents are added by appending to the agents list — the rest of caur
// is agent-agnostic.
type Agent struct {
	Backend    string   // canonical config name (e.g. "claude-cli")
	Aliases    []string // alternative names accepted in the config
	Bin        string   // executable to invoke
	NeedsModel bool     // backend cannot run without an explicit model

	// promptArgs builds the argv for a one-shot, headless prompt.
	promptArgs func(model, prompt string) []string
	// parse extracts the model's text answer from the command's stdout.
	parse func(stdout []byte) (string, error)
	// replArgs builds the argv for an interactive session (deep inspection).
	replArgs func(model string) []string
	// replSeedArgs builds the argv for an interactive session pre-seeded with an
	// initial message (caur's review result), so the agent starts with context
	// instead of cold. nil if the CLI can't seed and stay interactive.
	replSeedArgs func(model, seed string) []string
}

// agents is the registry of supported review backends. The first entry is the
// default when the config leaves backend empty.
var agents = []Agent{
	{
		Backend: "claude-cli", Aliases: []string{"claude"},
		Bin:   "claude",
		parse: parseClaudeEnvelope,
		promptArgs: func(model, prompt string) []string {
			a := []string{"-p", prompt, "--output-format", "json"}
			if model != "" {
				a = append(a, "--model", model)
			}
			return a
		},
		replArgs: func(model string) []string {
			if model != "" {
				return []string{"--model", model}
			}
			return nil
		},
		replSeedArgs: func(model, seed string) []string {
			var a []string
			if model != "" {
				a = append(a, "--model", model)
			}
			return append(a, seed) // positional prompt: stays interactive
		},
	},
	{
		// OpenAI Codex CLI ("gpt" is accepted as a friendly alias).
		Backend: "codex-cli", Aliases: []string{"codex", "gpt", "openai"},
		Bin:   "codex",
		parse: plainText,
		promptArgs: func(model, prompt string) []string {
			a := []string{"exec"}
			if model != "" {
				a = append(a, "-m", model)
			}
			return append(a, prompt)
		},
		replArgs: func(model string) []string {
			if model != "" {
				return []string{"-m", model}
			}
			return nil
		},
		replSeedArgs: func(model, seed string) []string {
			var a []string
			if model != "" {
				a = append(a, "-m", model)
			}
			return append(a, seed) // positional prompt opens the TUI seeded
		},
	},
	{
		// Local models via Ollama. A model is mandatory (e.g. "llama3.1").
		Backend: "ollama", Aliases: []string{"ollama-cli"},
		Bin: "ollama", NeedsModel: true,
		parse: plainText,
		promptArgs: func(model, prompt string) []string {
			return []string{"run", model, prompt}
		},
		replArgs: func(model string) []string {
			return []string{"run", model}
		},
		// `ollama run <model> "<prompt>"` is one-shot, not interactive: no seeding.
	},
	{
		// Google Gemini CLI.
		Backend: "gemini-cli", Aliases: []string{"gemini"},
		Bin:   "gemini",
		parse: plainText,
		promptArgs: func(model, prompt string) []string {
			var a []string
			if model != "" {
				a = append(a, "-m", model)
			}
			return append(a, "-p", prompt)
		},
		replArgs: func(model string) []string {
			if model != "" {
				return []string{"-m", model}
			}
			return nil
		},
		replSeedArgs: func(model, seed string) []string {
			var a []string
			if model != "" {
				a = append(a, "-m", model)
			}
			return append(a, "-i", seed) // -i: interactive with an initial prompt
		},
	},
}

// lookupAgent finds the agent profile for a backend name (canonical or alias),
// case-insensitively. An empty name selects the default (first) agent.
func lookupAgent(name string) (Agent, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return agents[0], true
	}
	for _, ag := range agents {
		if strings.ToLower(ag.Backend) == name {
			return ag, true
		}
		for _, al := range ag.Aliases {
			if strings.ToLower(al) == name {
				return ag, true
			}
		}
	}
	return Agent{}, false
}

// backendNames lists the canonical backend names, for diagnostics.
func backendNames() []string {
	names := make([]string, 0, len(agents))
	for _, ag := range agents {
		names = append(names, ag.Backend)
	}
	return names
}

// claudeEnvelope is the JSON envelope produced by `claude --output-format json`.
type claudeEnvelope struct {
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
}

// parseClaudeEnvelope pulls the model text out of the claude CLI envelope. If
// the output is not the expected envelope, it falls back to using it directly.
func parseClaudeEnvelope(out []byte) (string, error) {
	var env claudeEnvelope
	if err := json.Unmarshal(bytes.TrimSpace(out), &env); err == nil && env.Result != "" {
		if env.IsError {
			return "", fmt.Errorf("agent returned an error: %s", truncate(env.Result, 400))
		}
		return env.Result, nil
	}
	return plainText(out)
}

// plainText treats stdout as the model answer verbatim (the JSON object is then
// extracted by parseResult, tolerating surrounding log noise or markdown).
func plainText(out []byte) (string, error) {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "", fmt.Errorf("empty response from agent")
	}
	return s, nil
}
