// Command caur è un front-end per yay che fa revisionare il PKGBUILD (e i file
// collegati) da un agente Claude prima di costruire/installare un pacchetto AUR.
//
//	caur <termine>        cerca pacchetti (passthrough a yay -Ss)
//	caur -S <pkg>         installa <pkg> dopo la review
//	caur -Syu             aggiorna il sistema, revisionando gli aggiornamenti AUR
//	caur -Uni <pkg>       disinstalla <pkg> (alias di yay -Rns)
//	caur review <pkg>     audita <pkg> senza installarlo
//	caur -Q / -R / ...    operazioni di sola lettura/rimozione: passthrough
package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"caur/internal/aur"
	"caur/internal/cache"
	"caur/internal/cli"
	"caur/internal/config"
	"caur/internal/passthrough"
	"caur/internal/policy"
	"caur/internal/review"
)

func main() {
	args := os.Args[1:]
	cfg := config.Load()

	// Sotto-comando di solo audit, senza installare.
	if len(args) > 0 && args[0] == "review" {
		os.Exit(runReviewOnly(cfg, args[1:]))
	}

	parsed := cli.Classify(args)

	switch parsed.Op {
	case cli.OpPassthrough:
		code, err := passthrough.Exec(cfg.YayPath, parsed.Args)
		if err != nil {
			fail("impossibile eseguire %s: %v", cfg.YayPath, err)
		}
		os.Exit(code)

	case cli.OpInstall:
		names := parsed.Targets
		if !reviewAndDecide(cfg, names, hasNoConfirm(args)) {
			fmt.Fprintln(os.Stderr, "caur: installazione annullata dalla review.")
			os.Exit(1)
		}
		code, err := passthrough.Exec(cfg.YayPath, parsed.Args)
		if err != nil {
			fail("impossibile eseguire %s: %v", cfg.YayPath, err)
		}
		os.Exit(code)

	case cli.OpUpgrade:
		names := append([]string{}, parsed.Targets...)
		upd, err := aurUpgradeNames()
		if err != nil {
			fmt.Fprintf(os.Stderr, "caur: avviso, impossibile elencare gli aggiornamenti AUR: %v\n", err)
		}
		names = append(names, upd...)
		if len(names) == 0 {
			fmt.Fprintln(os.Stderr, "caur: nessun aggiornamento AUR da revisionare.")
		} else if !reviewAndDecide(cfg, names, hasNoConfirm(args)) {
			fmt.Fprintln(os.Stderr, "caur: aggiornamento annullato dalla review.")
			os.Exit(1)
		}
		code, err := passthrough.Exec(cfg.YayPath, parsed.Args)
		if err != nil {
			fail("impossibile eseguire %s: %v", cfg.YayPath, err)
		}
		os.Exit(code)
	}
}

// reviewAndDecide risolve i target AUR, li revisiona (usando la cache) e, se
// qualcosa è bloccato, mostra i report e chiede conferma. Ritorna true se si
// può procedere con l'installazione.
func reviewAndDecide(cfg config.Config, names []string, noconfirm bool) bool {
	pkgs, err := aur.Resolve(names)
	if err != nil {
		fail("risoluzione AUR: %v", err)
	}
	if len(pkgs) == 0 {
		// Nessun pacchetto AUR coinvolto (tutti dai repo ufficiali, firmati).
		return true
	}

	reviewer, err := review.New(cfg)
	if err != nil {
		fail("%v", err)
	}
	c := cache.Load()

	trusted := map[string]bool{}
	for _, t := range cfg.TrustedPackages {
		trusted[t] = true
	}

	type item struct {
		base     string
		result   review.Result
		decision policy.Decision
		trusted  bool
	}
	var items []item
	blocked := false

	for _, p := range pkgs {
		if trusted[p.PackageBase] {
			items = append(items, item{base: p.PackageBase, trusted: true,
				decision: policy.Decision{Allow: true, Reason: "trusted"}})
			continue
		}

		fmt.Fprintf(os.Stderr, "caur: review di %s...\n", p.PackageBase)
		pf, err := aur.Fetch(p.PackageBase, cacheDir())
		if err != nil {
			fail("download %s: %v", p.PackageBase, err)
		}

		hash := cache.Hash(pf)
		res, ok := c.Get(hash)
		if !ok || !cfg.CacheReviews {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			res, err = reviewer.Review(ctx, pf)
			cancel()
			if err != nil {
				// Fail-closed: se la review non si completa, non si installa.
				fail("review fallita per %s: %v", p.PackageBase, err)
			}
			if cfg.CacheReviews {
				c.Put(hash, res)
			}
		}

		dec := policy.Evaluate(res, cfg)
		if dec.NeedConfirm {
			blocked = true
		}
		items = append(items, item{base: p.PackageBase, result: res, decision: dec})
	}

	if cfg.CacheReviews {
		_ = c.Save()
	}

	// Report.
	fmt.Fprintln(os.Stderr)
	for _, it := range items {
		if it.trusted {
			fmt.Fprintf(os.Stderr, "  ✓ %s  (allowlist)\n", it.base)
			continue
		}
		renderResult(it.base, it.result, it.decision)
	}
	fmt.Fprintln(os.Stderr)

	if !blocked {
		return true
	}

	// Almeno un pacchetto è bloccato: serve conferma esplicita.
	if noconfirm {
		fmt.Fprintln(os.Stderr, "caur: rilievi di sicurezza presenti e --noconfirm attivo: blocco (fail-closed).")
		return false
	}
	return confirm("Sono stati rilevati possibili rischi. Procedere comunque con l'installazione?")
}

// runReviewOnly esegue solo l'audit dei pacchetti dati, senza installare.
func runReviewOnly(cfg config.Config, names []string) int {
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "uso: caur review <pkg> [pkg...]")
		return 2
	}
	pkgs, err := aur.Resolve(names)
	if err != nil {
		fmt.Fprintf(os.Stderr, "caur: %v\n", err)
		return 1
	}
	if len(pkgs) == 0 {
		fmt.Fprintln(os.Stderr, "caur: nessuno dei pacchetti indicati è nell'AUR.")
		return 0
	}
	reviewer, err := review.New(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "caur: %v\n", err)
		return 1
	}
	c := cache.Load()
	worst := 0
	for _, p := range pkgs {
		fmt.Fprintf(os.Stderr, "caur: review di %s...\n", p.PackageBase)
		pf, err := aur.Fetch(p.PackageBase, cacheDir())
		if err != nil {
			fmt.Fprintf(os.Stderr, "caur: download %s: %v\n", p.PackageBase, err)
			return 1
		}
		hash := cache.Hash(pf)
		res, ok := c.Get(hash)
		if !ok || !cfg.CacheReviews {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			res, err = reviewer.Review(ctx, pf)
			cancel()
			if err != nil {
				fmt.Fprintf(os.Stderr, "caur: review fallita per %s: %v\n", p.PackageBase, err)
				return 1
			}
			if cfg.CacheReviews {
				c.Put(hash, res)
			}
		}
		renderResult(p.PackageBase, res, policy.Evaluate(res, cfg))
		if res.Score > worst {
			worst = res.Score
		}
	}
	if cfg.CacheReviews {
		_ = c.Save()
	}
	if worst >= 50 {
		return 1
	}
	return 0
}

// renderResult stampa il report di una review.
func renderResult(base string, r review.Result, d policy.Decision) {
	mark := "✓"
	if d.NeedConfirm {
		mark = "⚠"
	}
	fmt.Fprintf(os.Stderr, "  %s %s  [%s, rischio %d/100]\n", mark, base, strings.ToUpper(emptyTo(r.Verdict, "?")), r.Score)
	if r.Summary != "" {
		fmt.Fprintf(os.Stderr, "      %s\n", r.Summary)
	}
	for _, f := range r.Findings {
		fmt.Fprintf(os.Stderr, "      - [%s] %s", strings.ToUpper(f.Severity), f.Title)
		if f.File != "" {
			fmt.Fprintf(os.Stderr, " (%s)", f.File)
		}
		fmt.Fprintln(os.Stderr)
		if f.Detail != "" {
			fmt.Fprintf(os.Stderr, "        %s\n", f.Detail)
		}
		if f.Evidence != "" {
			fmt.Fprintf(os.Stderr, "        > %s\n", strings.TrimSpace(f.Evidence))
		}
	}
}

// aurUpgradeNames elenca i pacchetti foreign (-Qm) che hanno una versione più
// recente nell'AUR.
func aurUpgradeNames() ([]string, error) {
	out, err := exec.Command("pacman", "-Qm").Output()
	if err != nil {
		return nil, err
	}
	local := map[string]string{}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			local[fields[0]] = fields[1]
			names = append(names, fields[0])
		}
	}
	if len(names) == 0 {
		return nil, nil
	}

	var candidates []string
	// Interroga l'AUR a blocchi per non superare la lunghezza dell'URL.
	const chunk = 150
	for i := 0; i < len(names); i += chunk {
		end := i + chunk
		if end > len(names) {
			end = len(names)
		}
		infos, err := aur.Info(names[i:end])
		if err != nil {
			return nil, err
		}
		for _, info := range infos {
			if vercmp(local[info.Name], info.Version) < 0 {
				candidates = append(candidates, info.Name)
			}
		}
	}
	return candidates, nil
}

// vercmp confronta due versioni usando l'utility `vercmp` di pacman.
// Ritorna <0 se a<b, 0 se uguali, >0 se a>b.
func vercmp(a, b string) int {
	if a == "" || b == "" {
		return 0
	}
	out, err := exec.Command("vercmp", a, b).Output()
	if err != nil {
		return 0
	}
	switch strings.TrimSpace(string(out)) {
	case "-1":
		return -1
	case "1":
		return 1
	default:
		return 0
	}
}

func confirm(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes" || line == "s" || line == "si"
}

func hasNoConfirm(args []string) bool {
	for _, a := range args {
		if a == "--noconfirm" {
			return true
		}
	}
	return false
}

func cacheDir() string {
	return cache.Dir() + "/pkg"
}

func emptyTo(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "caur: "+format+"\n", a...)
	os.Exit(1)
}
