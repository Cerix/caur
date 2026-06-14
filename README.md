# caur

**caur** = **Check AUR**. A front-end for [`yay`](https://github.com/Jguer/yay)
that has an **AI agent review the `PKGBUILD`** (and related files: `.install`,
`.SRCINFO`, patches, scripts) **before** building or installing a package from
the AUR. The goal is to reduce the risk of installing malware from hostile
PKGBUILDs while keeping the familiar yay/pacman workflow.

The reviewing agent is **pluggable** — caur is not tied to any single model. It
ships profiles for **Claude**, **Codex/GPT**, **Ollama** (local models) and
**Gemini**, and shells out to each agent's own CLI, reusing your existing login
(no API keys to manage).

`caur` does not reimplement dependency resolution or `makepkg`: it downloads and
reviews, and on approval **delegates the actual install to yay**.

## How it works

```
caur -S <pkg>
   │
   ├─ identify AUR packages (RPC v5) and resolve AUR dependencies
   ├─ download PKGBUILD + related files (git, cached)
   ├─ heuristic pre-scan (offline obfuscation/malware patterns)
   ├─ review with the configured agent → { verdict, score 0-100, findings[] }
   ├─ policy: "clean" proceeds; any significant finding BLOCKS and asks
   ├─ (optional) open the agent CLI to inspect the package hands-on
   └─ if approved → yay -S <pkg>   (build, deps, install)
```

Packages from the **official repos** are not reviewed: they are signed and
handled by pacman/yay. The review only concerns the AUR.

**Fail-closed:** if the review does not complete (backend error, timeout), the
installation is blocked.

### Review agents (backends)

Set `backend` (and optionally `model`) in the config. caur drives the agent's
own command-line tool in headless mode:

| backend      | CLI       | aliases              | notes                          |
|--------------|-----------|----------------------|--------------------------------|
| `claude-cli` | `claude`  | `claude`             | default                        |
| `codex-cli`  | `codex`   | `codex`, `gpt`       | OpenAI Codex CLI               |
| `ollama`     | `ollama`  | —                    | local models; `model` required |
| `gemini-cli` | `gemini`  | `gemini`             | Google Gemini CLI              |

Adding a new agent is a single profile entry in `internal/review/agent.go`; the
rest of caur is agent-agnostic.

### What gets reviewed (incl. `.install` hooks)

caur sends the agent every build-relevant file: `PKGBUILD`, `.SRCINFO`, patches,
helper scripts and — importantly — the **`.install` scriptlets**
(`pre_install`/`post_install`/`pre_upgrade`/`post_upgrade`/`pre_remove`/
`post_remove`). Those hooks **run as root** during package operations and are a
favorite hiding spot for malware, so they are scrutinized with extra weight.

Before the agent is even called, a fast **offline heuristic pre-scan** flags
high-confidence patterns — remote code piped to a shell, base64/`eval` payloads,
and **hex/octal-escaped obfuscation** (the exact trick used in a
[real AUR compromise](https://lists.archlinux.org/archives/list/aur-general@lists.archlinux.org/message/TND7HA2KBQ46OHHUMMIAHKGXZE4WALM6/)
that hid a command in a `post_install` hook). In an `.install` file these are
raised to **critical**. This is defense-in-depth: it catches the pattern even if
the model misses it or the agent CLI is unavailable.

### Hands-on inspection (`interactive_inspect`)

After a review, if anything was flagged, caur offers to open the **configured
agent's interactive CLI right in the package's clone directory** — so you can ask
follow-up questions ("is this `post_install` hook malicious?") with the PKGBUILD
and all related files at hand, then return to caur to decide.

The headless review and the interactive session are separate agent invocations,
so the conversation can't be literally resumed — but caur **seeds the session
with its review result** (verdict, score, findings) so the agent starts with
context rather than cold, and can re-read the files itself from the clone dir.
Seeding is supported for `claude`, `codex` and `gemini`; `ollama` opens a plain
session. Skipped when non-interactive (pipes, `--noconfirm`) or disabled in the
config.

### Incremental review (diff-only)

Each approved outcome is stored in `~/.cache/caur/reviews.json` together with a
snapshot of the files. Next time:

- **identical** files → outcome reused from the cache, no model call;
- **changed** files with a previously approved version → review of **only the
  diff** (`diff_review`): only what changed is sent to the model, assessing
  whether the changes introduce new risks (fewer tokens, more focus);
- first review or no cache → full review.

The cache (and the maintainer baseline) is updated **only if you proceed**: if
you decline because of a finding, you will be warned again next time.

### Supply-chain signals (`maintainer_change`)

Beyond the content, caur uses the AUR metadata as deterministic signals, shown
in the report and injected into the prompt:

- **orphaned package** (no maintainer on AUR) → high-severity finding;
- **maintainer changed** relative to the last approved review → high-severity
  finding (a classic supply-chain vector). The comparison is relative to the
  last time *you* approved the package (the AUR does not expose maintainer
  history via RPC);
- **out-of-date** → low-severity finding;
- last-modified date and vote count → context for the model.

A high-severity finding triggers the block with a confirmation prompt.

## Usage

```sh
caur <term>           # search packages (passthrough to yay -Ss)
caur -Ss <term>       # same
caur -S <pkg>         # install <pkg> after the review
caur -Syu             # upgrade the system, reviewing the AUR updates
caur -Uni <pkg>       # uninstall <pkg> (alias for `yay -Rns`)
caur review <pkg>     # audit <pkg> without installing it
caur -Q / -R / ...    # read-only/removal operations: passthrough to yay
```

With `--noconfirm`, a package with findings is **blocked** (it does not
auto-approve malware in non-interactive contexts).

## Configuration

Copy `config.example.toml` to `~/.config/caur/config.toml`. Main keys:

| key                   | default        | description                                            |
|-----------------------|----------------|--------------------------------------------------------|
| `backend`             | `claude-cli`   | review agent: `claude-cli`/`codex-cli`/`ollama`/`gemini-cli` |
| `model`               | `""`           | model alias; empty = agent default (required for `ollama`) |
| `block_threshold`     | `1`            | number of significant findings that triggers a block   |
| `auto_approve_clean`  | `true`         | "clean" proceeds without confirmation                  |
| `cache_reviews`       | `true`         | reuse the review if the files are unchanged            |
| `diff_review`         | `true`         | on updates, review only the diff vs the last version   |
| `maintainer_change`   | `true`         | flag/block if orphaned or if the maintainer changed    |
| `interactive_inspect` | `true`         | offer to open the agent CLI for hands-on inspection    |
| `trusted_packages`    | `[]`           | allowlist of pkgbases that skip the review             |
| `yay_path`            | `yay`          | executable of the underlying AUR engine                |

## Requirements

- Arch Linux with `yay`, `pacman`, `git`
- The CLI of your chosen agent installed and logged in, in `PATH`
  (e.g. `claude`, `codex`, `ollama`, or `gemini`)
- Go ≥ 1.24 to build

## Build

```sh
go build -o caur ./cmd/caur
```

## Test

```sh
go test ./...                                            # unit tests (offline)
CAUR_LIVE=1 go test ./internal/review/ -run Hostile -v   # live review (network + claude)
```

## Status and future ideas

Working MVP (yay wrapper) with pluggable agents (`claude-cli`, `codex-cli`,
`ollama`, `gemini-cli`), an offline heuristic pre-scan, and hands-on agent
inspection. Later ideas: persistent audit log, per-maintainer allowlist,
checksum verification vs `.SRCINFO`, direct API backends (no CLI) behind the
`Reviewer` interface.

## License

[MIT](LICENSE) © Cerix
