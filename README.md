# caur

Un front-end per [`yay`](https://github.com/Jguer/yay) che fa **revisionare il
`PKGBUILD`** (e i file collegati: `.install`, `.SRCINFO`, patch, script) da un
agente Claude **prima** di costruire o installare un pacchetto dall'AUR. Lo scopo
è ridurre il rischio di installare malware da PKGBUILD ostili, mantenendo il
workflow familiare di yay/pacman.

`caur` non reimplementa la risoluzione delle dipendenze né `makepkg`: scarica e
revisiona, e su approvazione **delega a yay** l'installazione vera e propria.

## Come funziona

```
caur -S <pkg>
   │
   ├─ identifica i pacchetti AUR (RPC v5) e risolve le dipendenze AUR
   ├─ scarica PKGBUILD + file collegati (git, in cache)
   ├─ review con `claude -p` → { verdict, score 0-100, findings[] }
   ├─ policy: "clean" procede; qualsiasi rilievo BLOCCA e chiede conferma
   └─ se approvato → yay -S <pkg>   (build, deps, install)
```

I pacchetti dei **repo ufficiali** non vengono revisionati: sono firmati e li
gestisce pacman/yay. La review riguarda solo l'AUR.

**Fail-closed:** se la review non si completa (errore del backend, timeout),
l'installazione viene bloccata.

### Review incrementale (diff-only)

Ogni esito approvato viene memorizzato in `~/.cache/caur/reviews.json` insieme
allo snapshot dei file. Alla volta successiva:

- file **identici** → esito riusato dalla cache, nessuna chiamata al modello;
- file **cambiati** e versione precedente approvata → review del **solo diff**
  (`diff_review`): si invia al modello unicamente ciò che è cambiato, valutando
  se le modifiche introducono nuovi rischi (meno token, più focus);
- prima review o cache assente → review completa.

La cache (e il baseline del maintainer) viene aggiornata **solo se procedi**:
se rifiuti per via di un rilievo, alla volta dopo verrai riavvisato.

### Segnali supply-chain (`maintainer_change`)

Oltre al contenuto, caur usa i metadati dell'AUR come segnali deterministici,
mostrati nel report e iniettati nel prompt:

- **pacchetto orfano** (nessun maintainer su AUR) → finding ad alta severità;
- **maintainer cambiato** rispetto all'ultima review approvata → finding ad alta
  severità (classico vettore supply-chain). Il confronto è relativo all'ultima
  volta che *tu* hai approvato il pacchetto (l'AUR non espone lo storico dei
  maintainer via RPC);
- **out-of-date** → finding a bassa severità;
- data dell'ultima modifica e numero di voti → contesto per il modello.

Un finding ad alta severità fa scattare il blocco con richiesta di conferma.

## Uso

```sh
caur <termine>        # cerca pacchetti (passthrough a yay -Ss)
caur -Ss <termine>    # idem
caur -S <pkg>         # installa <pkg> dopo la review
caur -Syu             # aggiorna il sistema, revisionando gli update AUR
caur -Uni <pkg>       # disinstalla <pkg> (alias di `yay -Rns`)
caur review <pkg>     # audita <pkg> senza installarlo
caur -Q / -R / ...    # operazioni di sola lettura/rimozione: passthrough a yay
```

Con `--noconfirm`, un pacchetto con rilievi viene **bloccato** (non si
auto-approva del malware in contesti non interattivi).

## Configurazione

Copia `config.example.toml` in `~/.config/caur/config.toml`. Chiavi principali:

| chiave               | default        | descrizione                                          |
|----------------------|----------------|------------------------------------------------------|
| `backend`            | `claude-cli`   | motore di review (per ora solo il CLI `claude`)      |
| `model`              | `""`           | alias modello; vuoto = default del CLI               |
| `block_threshold`    | `1`            | n. di findings che fa scattare il blocco             |
| `auto_approve_clean` | `true`         | "clean" procede senza conferma                       |
| `cache_reviews`      | `true`         | riusa la review se i file non cambiano               |
| `diff_review`        | `true`         | sugli update revisiona solo il diff vs ultima versione |
| `maintainer_change`  | `true`         | segnala/blocca se orfano o se il maintainer è cambiato |
| `trusted_packages`   | `[]`           | allowlist di pkgbase che saltano la review           |
| `yay_path`           | `yay`          | eseguibile del motore AUR sottostante                |

## Requisiti

- Arch Linux con `yay`, `pacman`, `git`
- Il CLI `claude` installato e loggato (`claude` in `PATH`)
- Go ≥ 1.24 per compilare

## Build

```sh
go build -o caur ./cmd/caur
```

## Test

```sh
go test ./...                                   # unit test (offline)
CAUR_LIVE=1 go test ./internal/review/ -run Hostile -v   # review live (rete + claude)
```

## Stato e idee future

MVP funzionante (wrapper su yay, backend `claude-cli`). Idee successive:
pre-scan euristico per ridurre i token, audit log persistente, allowlist per
maintainer, verifica dei checksum vs `.SRCINFO`, backend aggiuntivi (API
Anthropic/OpenAI/Ollama) dietro l'interfaccia `Reviewer`.

## Licenza

[MIT](LICENSE) © Cerix
