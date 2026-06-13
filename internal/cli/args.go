// Package cli classifica gli argomenti stile pacman/yay per decidere se
// l'operazione richiede una review (install/upgrade di pacchetti AUR) oppure
// va passata invariata a yay (ricerca, query, rimozione, ...).
package cli

import "strings"

// Op è il tipo di operazione richiesta.
type Op int

const (
	// OpPassthrough: niente review, esegui yay con gli stessi argomenti.
	OpPassthrough Op = iota
	// OpInstall: install di pacchetti espliciti -> review prima di installare.
	OpInstall
	// OpUpgrade: aggiornamento di sistema -> review dei PKGBUILD AUR cambiati.
	OpUpgrade
)

// Parsed è il risultato della classificazione.
type Parsed struct {
	Op      Op
	Targets []string // nomi di pacchetto espliciti (senza i flag)
	Args    []string // argomenti da passare a yay (eventualmente riscritti)
}

// Classify analizza gli argomenti della riga di comando.
func Classify(args []string) Parsed {
	// Disinstallazione: `-Uni`/`--uninstall` è un alias di caur (non un'op pacman
	// valida) tradotto in una rimozione yay completa. Non serve review.
	if rewritten, ok := rewriteUninstall(args); ok {
		return Parsed{Op: OpPassthrough, Args: rewritten}
	}

	var targets []string
	letters := map[rune]bool{}
	long := map[string]bool{}

	for _, a := range args {
		switch {
		case strings.HasPrefix(a, "--"):
			long[a] = true
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for _, c := range a[1:] {
				letters[c] = true
			}
		default:
			targets = append(targets, a)
		}
	}

	isSync := letters['S'] || long["--sync"]

	// Nessuna operazione di sync: parole nude -> ricerca; tutto il resto passa.
	if !isSync {
		if len(targets) > 0 && len(targets) == len(args) {
			// Solo parole nude: trattale come ricerca per non bypassare la review.
			return Parsed{Op: OpPassthrough, Args: append([]string{"-Ss"}, targets...)}
		}
		return Parsed{Op: OpPassthrough, Args: args}
	}

	// Sotto-operazioni di sync in sola lettura: passthrough.
	readOnly := letters['s'] || long["--search"] ||
		letters['i'] || long["--info"] ||
		letters['c'] || long["--clean"] ||
		letters['l'] || long["--list"] ||
		letters['g'] || long["--groups"]
	if readOnly {
		return Parsed{Op: OpPassthrough, Args: args}
	}

	upgrade := letters['u'] || long["--sysupgrade"]
	if upgrade {
		return Parsed{Op: OpUpgrade, Targets: targets, Args: args}
	}
	if len(targets) > 0 {
		return Parsed{Op: OpInstall, Targets: targets, Args: args}
	}
	// Es. -Sy (solo refresh dei db): nessun pacchetto da revisionare.
	return Parsed{Op: OpPassthrough, Args: args}
}

// rewriteUninstall riconosce l'alias di disinstallazione di caur (`-Uni` o
// `--uninstall`) e lo riscrive in `yay -Rns <targets>`, conservando gli altri
// argomenti (es. --noconfirm). Ritorna ok=false se l'alias non è presente.
func rewriteUninstall(args []string) ([]string, bool) {
	found := false
	out := []string{"-Rns"}
	for _, a := range args {
		if a == "-Uni" || a == "--uninstall" {
			found = true
			continue
		}
		out = append(out, a)
	}
	if !found {
		return nil, false
	}
	return out, true
}
