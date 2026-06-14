# Installing caur locally

This is a step-by-step guide to build and run **caur** from source on your own
machine. caur is a thin front-end: it reviews AUR `PKGBUILD`s with an AI agent
and then delegates the actual install to `yay`.

## 1. Prerequisites

You need an Arch Linux system (or a derivative) with:

- **`yay`**, **`pacman`** and **`git`** in `PATH` (the underlying AUR engine)
- **Go ≥ 1.24** to build (`pacman -S go`)
- the **CLI of one AI agent**, installed and logged in (pick one):

  | backend (`config`) | CLI binary | install / login                                   |
  |--------------------|------------|---------------------------------------------------|
  | `claude-cli`       | `claude`   | install the Claude CLI, then run `claude` once to log in |
  | `codex-cli`        | `codex`    | install the OpenAI Codex CLI and authenticate     |
  | `ollama`           | `ollama`   | `pacman -S ollama`, `ollama serve`, `ollama pull <model>` |
  | `gemini-cli`       | `gemini`   | install the Gemini CLI and authenticate           |

Verify the basics:

```sh
go version          # >= 1.24
yay --version
claude --version    # or: codex --version / ollama --version / gemini --version
```

## 2. Get the source

```sh
git clone https://github.com/Cerix/caur.git
cd caur
```

## 3. Build

```sh
go build -o caur ./cmd/caur
```

This produces a self-contained `./caur` binary (caur has no external Go
dependencies). Quick check:

```sh
./caur --help-ish   # any non-install arg is passed through to yay
```

## 4. Install it on your PATH

Put the binary somewhere on your `PATH`. A per-user location needs no root:

```sh
install -Dm755 caur ~/.local/bin/caur
```

Make sure `~/.local/bin` is on your `PATH` (add to `~/.bashrc` / `~/.zshrc` if
needed):

```sh
export PATH="$HOME/.local/bin:$PATH"
```

Alternatively, install system-wide:

```sh
sudo install -Dm755 caur /usr/local/bin/caur
```

Or let Go install it for you (binary goes to `$(go env GOBIN)` or
`~/go/bin`):

```sh
go install ./cmd/caur     # from inside the repo
```

## 5. Configure caur (optional)

caur runs with sensible defaults (Claude backend, fail-closed). To customize,
copy the example config:

```sh
mkdir -p ~/.config/caur
cp config.example.toml ~/.config/caur/config.toml
```

Then edit `~/.config/caur/config.toml`, most commonly to choose the agent:

```toml
backend = "ollama"        # claude-cli | codex-cli | ollama | gemini-cli
model   = "llama3.1"      # required for ollama; optional for the others
```

See the full key list in the [README](README.md#configuration).

## 6. First run

Audit a package **without installing** it (safe way to verify your setup):

```sh
caur review yay-bin
```

You should see a colored security report with a verdict, a risk score and any
findings. A normal search is a plain passthrough to yay:

```sh
caur firefox          # search
caur -S <some-aur-pkg># review, then (on approval) install via yay
```

If anything is flagged, caur can drop you into your agent's interactive CLI,
seeded with the review findings, right in the package's clone directory, so you
can inspect it hands-on before deciding.

## 7. Updating caur

```sh
cd caur
git pull
go build -o caur ./cmd/caur
install -Dm755 caur ~/.local/bin/caur
```

## 8. Uninstalling caur

```sh
rm ~/.local/bin/caur            # the binary
rm -rf ~/.config/caur           # config (optional)
rm -rf ~/.cache/caur            # review cache + cloned PKGBUILDs (optional)
```

## Troubleshooting

- **`unsupported review backend: …`**: `backend` in the config must be one of
  `claude-cli`, `codex-cli`, `ollama`, `gemini-cli`.
- **`backend "ollama" requires a model`**: set `model` in the config (e.g.
  `model = "llama3.1"`) and make sure you've `ollama pull`ed it.
- **`running <agent>: … executable file not found`**: the agent CLI isn't
  installed or isn't on your `PATH`.
- **The review fails / times out**: caur is fail-closed, it blocks the install
  rather than proceeding blindly. Re-run once the agent CLI works (try the agent
  command directly to confirm it's logged in).
