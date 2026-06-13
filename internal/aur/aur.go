// Package aur interroga l'AUR (RPC v5) per identificare i pacchetti presenti
// nell'AUR e risolverne la chiusura delle dipendenze, e scarica i PKGBUILD e i
// file collegati tramite git per sottoporli alla review.
package aur

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const rpcBase = "https://aur.archlinux.org/rpc/v5/info"
const gitBase = "https://aur.archlinux.org"

// Pkg è il sottoinsieme della risposta RPC che ci interessa.
type Pkg struct {
	Name         string   `json:"Name"`
	PackageBase  string   `json:"PackageBase"`
	Version      string   `json:"Version"`
	Depends      []string `json:"Depends"`
	MakeDepends  []string `json:"MakeDepends"`
	CheckDepends []string `json:"CheckDepends"`

	// Segnali supply-chain.
	Maintainer   string `json:"Maintainer"`   // "" (null) = pacchetto orfano
	LastModified int64  `json:"LastModified"` // unix: ultima modifica del pacchetto
	OutOfDate    int64  `json:"OutOfDate"`    // unix: segnalato out-of-date (0 = no)
	NumVotes     int    `json:"NumVotes"`
}

// Orphaned indica se il pacchetto è senza maintainer.
func (p Pkg) Orphaned() bool { return strings.TrimSpace(p.Maintainer) == "" }

type rpcResponse struct {
	ResultCount int   `json:"resultcount"`
	Results     []Pkg `json:"results"`
	Error       string `json:"error"`
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Info interroga l'RPC per i nomi dati e restituisce solo quelli presenti
// nell'AUR (i pacchetti dei repo ufficiali non compaiono nei risultati).
func Info(names []string) ([]Pkg, error) {
	if len(names) == 0 {
		return nil, nil
	}
	q := url.Values{}
	for _, n := range names {
		q.Add("arg[]", n)
	}
	resp, err := httpClient.Get(rpcBase + "?" + q.Encode())
	if err != nil {
		return nil, fmt.Errorf("query AUR RPC: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	var r rpcResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("parse risposta RPC: %w", err)
	}
	if r.Error != "" {
		return nil, fmt.Errorf("AUR RPC: %s", r.Error)
	}
	return r.Results, nil
}

// Resolve parte dai nomi richiesti e risolve ricorsivamente la chiusura delle
// dipendenze che sono anch'esse nell'AUR. Restituisce i pacchetti unici per
// PackageBase da revisionare.
func Resolve(names []string) ([]Pkg, error) {
	seenName := map[string]bool{}
	seenBase := map[string]bool{}
	var out []Pkg

	queue := append([]string{}, names...)
	for len(queue) > 0 {
		// Prendi i nomi non ancora interrogati.
		var batch []string
		for _, n := range queue {
			if !seenName[n] {
				seenName[n] = true
				batch = append(batch, n)
			}
		}
		queue = nil
		if len(batch) == 0 {
			break
		}

		pkgs, err := Info(batch)
		if err != nil {
			return nil, err
		}
		for _, p := range pkgs {
			if !seenBase[p.PackageBase] {
				seenBase[p.PackageBase] = true
				out = append(out, p)
			}
			// Accoda le dipendenze (potrebbero a loro volta essere AUR).
			for _, d := range concat(p.Depends, p.MakeDepends, p.CheckDepends) {
				name := stripConstraint(d)
				if name != "" && !seenName[name] {
					queue = append(queue, name)
				}
			}
		}
	}
	return out, nil
}

// PkgFiles raccoglie il contenuto dei file rilevanti di un pkgbase.
type PkgFiles struct {
	PkgBase string
	Dir     string
	Files   map[string]string // nome file -> contenuto
}

// Fetch fa uno shallow clone (o aggiorna) del repo git del pkgbase nella cache
// e raccoglie PKGBUILD, *.install, .SRCINFO e gli altri file versionati
// (patch e sorgenti locali) che concorrono alla build.
func Fetch(pkgBase, cacheDir string) (PkgFiles, error) {
	dir := filepath.Join(cacheDir, pkgBase)
	repoURL := gitBase + "/" + pkgBase + ".git"

	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// Repo già presente: aggiorna.
		cmd := exec.Command("git", "-C", dir, "fetch", "--depth", "1", "origin")
		if out, err := cmd.CombinedOutput(); err != nil {
			return PkgFiles{}, fmt.Errorf("git fetch %s: %v: %s", pkgBase, err, out)
		}
		cmd = exec.Command("git", "-C", dir, "reset", "--hard", "origin/HEAD")
		if out, err := cmd.CombinedOutput(); err != nil {
			// origin/HEAD può non essere impostato: prova FETCH_HEAD.
			cmd = exec.Command("git", "-C", dir, "reset", "--hard", "FETCH_HEAD")
			if out2, err2 := cmd.CombinedOutput(); err2 != nil {
				return PkgFiles{}, fmt.Errorf("git reset %s: %v: %s / %s", pkgBase, err, out, out2)
			}
		}
	} else {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return PkgFiles{}, err
		}
		cmd := exec.Command("git", "clone", "--depth", "1", repoURL, dir)
		if out, err := cmd.CombinedOutput(); err != nil {
			return PkgFiles{}, fmt.Errorf("git clone %s: %v: %s", pkgBase, err, out)
		}
	}

	files, err := collectFiles(dir)
	if err != nil {
		return PkgFiles{}, err
	}
	if _, ok := files["PKGBUILD"]; !ok {
		return PkgFiles{}, fmt.Errorf("%s: PKGBUILD non trovato", pkgBase)
	}
	return PkgFiles{PkgBase: pkgBase, Dir: dir, Files: files}, nil
}

// collectFiles legge i file versionati rilevanti per la review.
func collectFiles(dir string) (map[string]string, error) {
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() || e.Name() == ".git" {
			continue
		}
		name := e.Name()
		if !relevant(name) {
			continue
		}
		full := filepath.Join(dir, name)
		info, err := e.Info()
		if err != nil || info.Size() > 1<<20 { // salta file enormi
			continue
		}
		b, err := os.ReadFile(full)
		if err != nil {
			continue
		}
		files[name] = string(b)
	}
	return files, nil
}

// relevant decide se un file va incluso nella review.
func relevant(name string) bool {
	switch name {
	case "PKGBUILD", ".SRCINFO":
		return true
	}
	if strings.HasSuffix(name, ".install") || strings.HasSuffix(name, ".sh") ||
		strings.HasSuffix(name, ".patch") || strings.HasSuffix(name, ".diff") ||
		strings.HasSuffix(name, ".hook") || strings.HasSuffix(name, ".service") {
		return true
	}
	return false
}

// stripConstraint rimuove i vincoli di versione da una dipendenza
// (es. "glibc>=2.0" -> "glibc", "foo: descr" -> "foo").
func stripConstraint(dep string) string {
	dep = strings.TrimSpace(dep)
	for _, sep := range []string{">=", "<=", "=", ">", "<", ":"} {
		if i := strings.Index(dep, sep); i >= 0 {
			dep = dep[:i]
		}
	}
	return strings.TrimSpace(dep)
}

func concat(slices ...[]string) []string {
	var out []string
	for _, s := range slices {
		out = append(out, s...)
	}
	return out
}
