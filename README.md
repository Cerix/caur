# caur

A front-end for [`yay`](https://github.com/Jguer/yay) that has a Claude agent
**review the `PKGBUILD`** (and related files: `.install`, `.SRCINFO`, patches,
scripts) **before** building or installing a package from the AUR. The goal is
to reduce the risk of installing malware from hostile PKGBUILDs while keeping the
familiar yay/pacman workflow.

`caur` does not reimplement dependency resolution or `makepkg`: it downloads and
reviews, and on approval **delegates the actual install to yay**.

## How it works

```
caur -S <pkg>
   │
   ├─ identify AUR packages (RPC v5) and resolve AUR dependencies
   ├─ download PKGBUILD + related files (git, cached)
   ├─ review with `claude -p` → { verdict, score 0-100, findings[] }
   ├─ policy: "clean" proceeds; any significant finding BLOCKS and asks
   └─ if approved → yay -S <pkg>   (build, deps, install)
```

Packages from the **official repos** are not reviewed: they are signed and
handled by pacman/yay. The review only concerns the AUR.

**Fail-closed:** if the review does not complete (backend error, timeout), the
installation is blocked.

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

| key                  | default        | description                                          |
|----------------------|----------------|------------------------------------------------------|
| `backend`            | `claude-cli`   | review engine (for now only the `claude` CLI)        |
| `model`              | `""`           | model alias; empty = CLI default                     |
| `block_threshold`    | `1`            | number of significant findings that triggers a block |
| `auto_approve_clean` | `true`         | "clean" proceeds without confirmation                |
| `cache_reviews`      | `true`         | reuse the review if the files are unchanged          |
| `diff_review`        | `true`         | on updates, review only the diff vs the last version |
| `maintainer_change`  | `true`         | flag/block if orphaned or if the maintainer changed  |
| `trusted_packages`   | `[]`           | allowlist of pkgbases that skip the review           |
| `yay_path`           | `yay`          | executable of the underlying AUR engine              |

## Requirements

- Arch Linux with `yay`, `pacman`, `git`
- The `claude` CLI installed and logged in (`claude` in `PATH`)
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

Working MVP (yay wrapper, `claude-cli` backend). Later ideas: heuristic
pre-scan to reduce tokens, persistent audit log, per-maintainer allowlist,
checksum verification vs `.SRCINFO`, additional backends (Anthropic/OpenAI/
Ollama API) behind the `Reviewer` interface.

## License

[MIT](LICENSE) © Cerix
