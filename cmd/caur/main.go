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

// reviewedItem raccoglie l'esito della review di un pacchetto.
type reviewedItem struct {
	base     string
	result   review.Result // include i findings supply-chain
	decision policy.Decision
	mode     string // full | diff | cache | trusted
	entry    cache.Entry
	persist  bool // true se c'è qualcosa da salvare in cache su proceed
}

// reviewAll risolve i target AUR e li revisiona, scegliendo per ciascuno la
// modalità (cache se invariato, diff se c'è una versione approvata, altrimenti
// completa) e aggiungendo i segnali supply-chain. Non scrive nulla su disco: la
// persistenza è decisa dal chiamante (solo se si procede).
func reviewAll(cfg config.Config, names []string) (items []reviewedItem, blocked bool, c *cache.Cache) {
	pkgs, err := aur.Resolve(names)
	if err != nil {
		fail("risoluzione AUR: %v", err)
	}
	reviewer, err := review.New(cfg)
	if err != nil {
		fail("%v", err)
	}
	c = cache.Load()

	trusted := map[string]bool{}
	for _, t := range cfg.TrustedPackages {
		trusted[t] = true
	}

	for _, p := range pkgs {
		if trusted[p.PackageBase] {
			items = append(items, reviewedItem{
				base:     p.PackageBase,
				mode:     "trusted",
				decision: policy.Decision{Allow: true, Reason: "trusted"},
			})
			continue
		}

		pf, err := aur.Fetch(p.PackageBase, cacheDir())
		if err != nil {
			fail("download %s: %v", p.PackageBase, err)
		}
		hash := cache.Hash(pf)
		prev, hasPrev := c.Get(p.PackageBase)

		meta, notes := supplyChainSignals(cfg, prev, hasPrev, p)

		var baseRes review.Result
		var mode string
		switch {
		case hasPrev && prev.Hash == hash && cfg.CacheReviews:
			baseRes, mode = prev.Result, "cache"
			fmt.Fprintf(os.Stderr, "caur: %s invariato dall'ultima review (cache)\n", p.PackageBase)
		case hasPrev && cfg.DiffReview && len(prev.Files) > 0:
			mode = "diff"
			fmt.Fprintf(os.Stderr, "caur: review delle modifiche di %s (diff)...\n", p.PackageBase)
			baseRes = runReview(func(ctx context.Context) (review.Result, error) {
				return reviewer.ReviewDiff(ctx, aur.PkgFiles{PkgBase: p.PackageBase, Files: prev.Files}, pf, notes)
			}, p.PackageBase)
		default:
			mode = "full"
			fmt.Fprintf(os.Stderr, "caur: review di %s...\n", p.PackageBase)
			baseRes = runReview(func(ctx context.Context) (review.Result, error) {
				return reviewer.Review(ctx, pf, notes)
			}, p.PackageBase)
		}

		finalRes := withSignals(baseRes, meta)
		dec := policy.Evaluate(finalRes, cfg)
		if dec.NeedConfirm {
			blocked = true
		}
		items = append(items, reviewedItem{
			base:     p.PackageBase,
			result:   finalRes,
			decision: dec,
			mode:     mode,
			persist:  true,
			entry: cache.Entry{
				Hash:         hash,
				Result:       baseRes, // senza i meta-findings, che vanno ricalcolati live
				Files:        pf.Files,
				Maintainer:   p.Maintainer,
				Version:      p.Version,
				LastModified: p.LastModified,
				ReviewedAt:   time.Now().Unix(),
			},
		})
	}
	return items, blocked, c
}

// runReview esegue una review con timeout, applicando il fail-closed.
func runReview(fn func(context.Context) (review.Result, error), base string) review.Result {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	res, err := fn(ctx)
	if err != nil {
		fail("review fallita per %s: %v", base, err)
	}
	return res
}

// withSignals fonde i findings supply-chain nel risultato e, se sono gravi,
// impedisce che il verdetto resti "clean".
func withSignals(r review.Result, meta []review.Finding) review.Result {
	if len(meta) == 0 {
		return r
	}
	out := r
	out.Findings = append(append([]review.Finding{}, r.Findings...), meta...)
	if out.Verdict == "clean" && hasConcerning(meta) {
		out.Verdict = "suspicious"
		if out.Score < 40 {
			out.Score = 40
		}
	}
	return out
}

func hasConcerning(fs []review.Finding) bool {
	for _, f := range fs {
		if f.Severity == "high" || f.Severity == "critical" {
			return true
		}
	}
	return false
}

// supplyChainSignals deriva findings deterministici e una nota di contesto dai
// metadati AUR: pacchetto orfano, cambio di maintainer rispetto all'ultima
// review approvata, out-of-date, recency.
func supplyChainSignals(cfg config.Config, prev cache.Entry, hasPrev bool, p aur.Pkg) ([]review.Finding, string) {
	if !cfg.MaintainerChange {
		return nil, ""
	}
	var findings []review.Finding
	var notes strings.Builder

	switch {
	case p.Orphaned():
		findings = append(findings, review.Finding{
			Severity: "high", File: "(AUR)", Title: "Pacchetto orfano (senza maintainer)",
			Detail: "Il pacchetto non ha un maintainer su AUR: un PKGBUILD orfano può essere adottato da chiunque. Verifica con attenzione il contenuto.",
		})
		notes.WriteString("- Il pacchetto è ORFANO (nessun maintainer su AUR).\n")
		if hasPrev && prev.Maintainer != "" {
			notes.WriteString(fmt.Sprintf("- In precedenza era mantenuto da %q.\n", prev.Maintainer))
		}
	case hasPrev && prev.Maintainer != "" && prev.Maintainer != p.Maintainer:
		findings = append(findings, review.Finding{
			Severity: "high", File: "(AUR)", Title: "Maintainer cambiato",
			Detail: fmt.Sprintf("Il maintainer è passato da %q a %q dall'ultima review approvata: un cambio di proprietà è un classico vettore di attacco supply-chain.", prev.Maintainer, p.Maintainer),
		})
		notes.WriteString(fmt.Sprintf("- Il MAINTAINER è cambiato: prima %q, ora %q.\n", prev.Maintainer, p.Maintainer))
	}

	if p.OutOfDate != 0 {
		findings = append(findings, review.Finding{
			Severity: "low", File: "(AUR)", Title: "Segnalato out-of-date",
			Detail: "Il pacchetto è marcato out-of-date su AUR; potrebbe essere trascurato dal maintainer.",
		})
	}

	if p.LastModified != 0 {
		notes.WriteString(fmt.Sprintf("- Ultima modifica del pacchetto su AUR: %s.\n", time.Unix(p.LastModified, 0).Format("2006-01-02")))
	}
	if p.Maintainer != "" {
		notes.WriteString(fmt.Sprintf("- Maintainer attuale: %q (voti AUR: %d).\n", p.Maintainer, p.NumVotes))
	}
	return findings, notes.String()
}

// reviewAndDecide revisiona i target e, se qualcosa è bloccato, mostra i report
// e chiede conferma. Su proceed persiste la cache (baseline maintainer + file).
func reviewAndDecide(cfg config.Config, names []string, noconfirm bool) bool {
	items, blocked, c := reviewAll(cfg, names)
	if len(items) == 0 {
		// Nessun pacchetto AUR coinvolto (tutti dai repo ufficiali, firmati).
		return true
	}

	renderItems(items)

	proceed := !blocked
	if blocked {
		if noconfirm {
			fmt.Fprintln(os.Stderr, "caur: rilievi di sicurezza presenti e --noconfirm attivo: blocco (fail-closed).")
			return false
		}
		proceed = confirm("Sono stati rilevati possibili rischi. Procedere comunque con l'installazione?")
	}

	if proceed {
		persist(c, items)
	}
	return proceed
}

// runReviewOnly esegue solo l'audit dei pacchetti dati, senza installare.
func runReviewOnly(cfg config.Config, names []string) int {
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "uso: caur review <pkg> [pkg...]")
		return 2
	}
	items, _, c := reviewAll(cfg, names)
	if len(items) == 0 {
		fmt.Fprintln(os.Stderr, "caur: nessuno dei pacchetti indicati è nell'AUR.")
		return 0
	}
	renderItems(items)
	persist(c, items)

	worst := 0
	for _, it := range items {
		if it.result.Score > worst {
			worst = it.result.Score
		}
	}
	if worst >= 50 {
		return 1
	}
	return 0
}

// persist salva nella cache gli esiti dei pacchetti revisionati.
func persist(c *cache.Cache, items []reviewedItem) {
	wrote := false
	for _, it := range items {
		if it.persist {
			c.Put(it.base, it.entry)
			wrote = true
		}
	}
	if wrote {
		_ = c.Save()
	}
}

// renderItems stampa i report di tutti i pacchetti.
func renderItems(items []reviewedItem) {
	fmt.Fprintln(os.Stderr)
	for _, it := range items {
		if it.mode == "trusted" {
			fmt.Fprintf(os.Stderr, "  ✓ %s  (allowlist)\n", it.base)
			continue
		}
		renderResult(it.base, it.result, it.decision, it.mode)
	}
	fmt.Fprintln(os.Stderr)
}

// renderResult stampa il report di una review.
func renderResult(base string, r review.Result, d policy.Decision, mode string) {
	mark := "✓"
	if d.NeedConfirm {
		mark = "⚠"
	}
	tag := ""
	if mode == "diff" {
		tag = " (diff)"
	} else if mode == "cache" {
		tag = " (cache)"
	}
	fmt.Fprintf(os.Stderr, "  %s %s%s  [%s, rischio %d/100]\n", mark, base, tag, strings.ToUpper(emptyTo(r.Verdict, "?")), r.Score)
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
