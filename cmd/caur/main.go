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
	"caur/internal/ui"
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
			fail("cannot run %s: %v", cfg.YayPath, err)
		}
		os.Exit(code)

	case cli.OpInstall:
		names := parsed.Targets
		if !reviewAndDecide(cfg, names, hasNoConfirm(args)) {
			info("installation cancelled by review.")
			os.Exit(1)
		}
		code, err := passthrough.Exec(cfg.YayPath, parsed.Args)
		if err != nil {
			fail("cannot run %s: %v", cfg.YayPath, err)
		}
		os.Exit(code)

	case cli.OpUpgrade:
		names := append([]string{}, parsed.Targets...)
		upd, err := aurUpgradeNames()
		if err != nil {
			info("warning: cannot list AUR upgrades: %v", err)
		}
		names = append(names, upd...)
		if len(names) == 0 {
			info("no AUR upgrades to review.")
		} else if !reviewAndDecide(cfg, names, hasNoConfirm(args)) {
			info("upgrade cancelled by review.")
			os.Exit(1)
		}
		code, err := passthrough.Exec(cfg.YayPath, parsed.Args)
		if err != nil {
			fail("cannot run %s: %v", cfg.YayPath, err)
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
		fail("AUR resolution: %v", err)
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
			progress("%s unchanged since last review (cache)", p.PackageBase)
		case hasPrev && cfg.DiffReview && len(prev.Files) > 0:
			mode = "diff"
			progress("reviewing changes to %s (diff)…", p.PackageBase)
			baseRes = runReview(func(ctx context.Context) (review.Result, error) {
				return reviewer.ReviewDiff(ctx, aur.PkgFiles{PkgBase: p.PackageBase, Files: prev.Files}, pf, notes)
			}, p.PackageBase)
		default:
			mode = "full"
			progress("reviewing %s…", p.PackageBase)
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
		fail("review failed for %s: %v", base, err)
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
			Severity: "high", File: "(AUR)", Title: "Orphaned package (no maintainer)",
			Detail: "The package has no maintainer on AUR: an orphaned PKGBUILD can be adopted by anyone. Review its contents carefully.",
		})
		notes.WriteString("- The package is ORPHANED (no maintainer on AUR).\n")
		if hasPrev && prev.Maintainer != "" {
			notes.WriteString(fmt.Sprintf("- It was previously maintained by %q.\n", prev.Maintainer))
		}
	case hasPrev && prev.Maintainer != "" && prev.Maintainer != p.Maintainer:
		findings = append(findings, review.Finding{
			Severity: "high", File: "(AUR)", Title: "Maintainer changed",
			Detail: fmt.Sprintf("The maintainer changed from %q to %q since the last approved review: a change of ownership is a classic supply-chain attack vector.", prev.Maintainer, p.Maintainer),
		})
		notes.WriteString(fmt.Sprintf("- The MAINTAINER changed: was %q, now %q.\n", prev.Maintainer, p.Maintainer))
	}

	if p.OutOfDate != 0 {
		findings = append(findings, review.Finding{
			Severity: "low", File: "(AUR)", Title: "Flagged out-of-date",
			Detail: "The package is flagged out-of-date on AUR; it may be neglected by the maintainer.",
		})
	}

	if p.LastModified != 0 {
		notes.WriteString(fmt.Sprintf("- Package last modified on AUR: %s.\n", time.Unix(p.LastModified, 0).Format("2006-01-02")))
	}
	if p.Maintainer != "" {
		notes.WriteString(fmt.Sprintf("- Current maintainer: %q (AUR votes: %d).\n", p.Maintainer, p.NumVotes))
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
			info("security findings present and --noconfirm is set: blocking (fail-closed).")
			return false
		}
		proceed = confirm("Possible risks were detected. Proceed with the installation anyway?")
	}

	if proceed {
		persist(c, items)
	}
	return proceed
}

// runReviewOnly esegue solo l'audit dei pacchetti dati, senza installare.
func runReviewOnly(cfg config.Config, names []string) int {
	if len(names) == 0 {
		fmt.Fprintln(os.Stderr, "usage: caur review <pkg> [pkg...]")
		return 2
	}
	items, _, c := reviewAll(cfg, names)
	if len(items) == 0 {
		info("none of the given packages are in the AUR.")
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

// renderItems stampa il report di tutti i pacchetti, allineato e colorato.
func renderItems(items []reviewedItem) {
	out := os.Stderr
	width := ui.Width()

	// Larghezza della colonna nome, comune a tutti i pacchetti.
	nameW := 12
	for _, it := range items {
		if len(it.base) > nameW {
			nameW = len(it.base)
		}
	}
	if nameW > 32 {
		nameW = 32
	}

	fmt.Fprintln(out)
	fmt.Fprintf(out, "%s %s\n\n", ui.Bold("caur"), ui.Dim("security review"))

	blocked := 0
	for _, it := range items {
		if it.mode == "trusted" {
			fmt.Fprintf(out, "  %s %s  %s\n", ui.Green("✓"), ui.Bold(ui.Pad(it.base, nameW)), ui.Dim("allowlist"))
			continue
		}
		if it.decision.NeedConfirm {
			blocked++
		}
		renderResult(out, it, nameW, width)
	}

	fmt.Fprintln(out)
	if blocked > 0 {
		fmt.Fprintf(out, "%s %s\n\n", ui.RedBold("✗"),
			ui.Bold(fmt.Sprintf("%d package(s) need your confirmation", blocked)))
	} else {
		fmt.Fprintf(out, "%s %s\n\n", ui.GreenBold("✓"), ui.Bold("all packages clear"))
	}
}

// renderResult stampa il report di una singola review.
func renderResult(out *os.File, it reviewedItem, nameW, width int) {
	r := it.result
	mark := ui.Green("✓")
	if it.decision.NeedConfirm {
		if strings.ToLower(r.Verdict) == "malicious" {
			mark = ui.RedBold("✗")
		} else {
			mark = ui.YellowBold("⚠")
		}
	}

	tag := ""
	switch it.mode {
	case "diff":
		tag = "  " + ui.Dim("(diff)")
	case "cache":
		tag = "  " + ui.Dim("(cache)")
	}

	fmt.Fprintf(out, "  %s %s  %s  %s%s\n",
		mark, ui.Bold(ui.Pad(it.base, nameW)),
		ui.Pad(verdictBadge(r.Verdict), 11), riskBadge(r.Score), tag)

	for _, line := range ui.Wrap(r.Summary, "", width-6) {
		fmt.Fprintf(out, "      %s\n", line)
	}

	for _, f := range r.Findings {
		head := severityBadge(f.Severity) + "  " + f.Title
		if f.File != "" {
			head += "  " + ui.Dim("· "+f.File)
		}
		fmt.Fprintf(out, "      %s %s\n", ui.Dim("•"), head)
		for _, line := range ui.Wrap(f.Detail, "", width-10) {
			fmt.Fprintf(out, "          %s\n", line)
		}
		if f.Evidence != "" {
			for _, line := range ui.Wrap(strings.TrimSpace(f.Evidence), "", width-12) {
				fmt.Fprintf(out, "          %s\n", ui.Dim("│ "+line))
			}
		}
	}
}

// verdictBadge restituisce il verdetto colorato (maiuscolo).
func verdictBadge(v string) string {
	up := strings.ToUpper(emptyTo(v, "?"))
	switch strings.ToLower(v) {
	case "clean":
		return ui.GreenBold(up)
	case "suspicious":
		return ui.YellowBold(up)
	default: // malicious, reject, sconosciuto
		return ui.RedBold(up)
	}
}

// riskBadge colora il punteggio di rischio in base alla soglia.
func riskBadge(score int) string {
	s := fmt.Sprintf("risk %d/100", score)
	switch {
	case score < 30:
		return ui.Green(s)
	case score < 70:
		return ui.Yellow(s)
	default:
		return ui.Red(s)
	}
}

// severityBadge restituisce la severità colorata e allineata.
func severityBadge(sev string) string {
	up := strings.ToUpper(emptyTo(sev, "INFO"))
	var colored string
	switch strings.ToLower(sev) {
	case "critical":
		colored = ui.RedBold(up)
	case "high":
		colored = ui.Red(up)
	case "medium":
		colored = ui.Yellow(up)
	case "low":
		colored = ui.Cyan(up)
	default:
		colored = ui.Gray(up)
	}
	return ui.Pad(colored, 8)
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
		for _, inf := range infos {
			if vercmp(local[inf.Name], inf.Version) < 0 {
				candidates = append(candidates, inf.Name)
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
	fmt.Fprintf(os.Stderr, "%s %s ", ui.YellowBold(question), ui.Dim("[y/N]"))
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
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

// info stampa un messaggio informativo con prefisso "caur".
func info(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.Cyan("caur"), fmt.Sprintf(format, a...))
}

// progress stampa una riga di stato attenuata.
func progress(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "  %s %s\n", ui.Dim("→"), ui.Dim(fmt.Sprintf(format, a...)))
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "%s %s\n", ui.RedBold("caur error:"), fmt.Sprintf(format, a...))
	os.Exit(1)
}
