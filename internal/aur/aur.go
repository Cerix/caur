// Package aur queries the AUR (RPC v5) to identify packages present in the AUR
// and resolve their dependency closure, and downloads PKGBUILDs and related
// files via git so they can be reviewed.
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

// Pkg is the subset of the RPC response we care about.
type Pkg struct {
	Name         string   `json:"Name"`
	PackageBase  string   `json:"PackageBase"`
	Version      string   `json:"Version"`
	Depends      []string `json:"Depends"`
	MakeDepends  []string `json:"MakeDepends"`
	CheckDepends []string `json:"CheckDepends"`

	// Supply-chain signals.
	Maintainer   string `json:"Maintainer"`   // "" (null) = orphaned package
	LastModified int64  `json:"LastModified"` // unix: package last modified
	OutOfDate    int64  `json:"OutOfDate"`    // unix: flagged out-of-date (0 = no)
	NumVotes     int    `json:"NumVotes"`
}

// Orphaned reports whether the package has no maintainer.
func (p Pkg) Orphaned() bool { return strings.TrimSpace(p.Maintainer) == "" }

type rpcResponse struct {
	ResultCount int    `json:"resultcount"`
	Results     []Pkg  `json:"results"`
	Error       string `json:"error"`
}

var httpClient = &http.Client{Timeout: 15 * time.Second}

// Info queries the RPC for the given names and returns only those present in
// the AUR (official-repo packages do not appear in the results).
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
		return nil, fmt.Errorf("parse RPC response: %w", err)
	}
	if r.Error != "" {
		return nil, fmt.Errorf("AUR RPC: %s", r.Error)
	}
	return r.Results, nil
}

// Resolve starts from the requested names and recursively resolves the closure
// of dependencies that are themselves in the AUR. It returns the packages
// unique by PackageBase to review.
func Resolve(names []string) ([]Pkg, error) {
	seenName := map[string]bool{}
	seenBase := map[string]bool{}
	var out []Pkg

	queue := append([]string{}, names...)
	for len(queue) > 0 {
		// Take the names not yet queried.
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
			// Enqueue the dependencies (they might also be AUR packages).
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

// PkgFiles holds the content of the relevant files of a pkgbase.
type PkgFiles struct {
	PkgBase string
	Dir     string
	Files   map[string]string // file name -> content
}

// Fetch shallow-clones (or updates) the pkgbase git repo into the cache and
// collects PKGBUILD, *.install, .SRCINFO and the other versioned files
// (patches and local sources) that take part in the build.
func Fetch(pkgBase, cacheDir string) (PkgFiles, error) {
	dir := filepath.Join(cacheDir, pkgBase)
	repoURL := gitBase + "/" + pkgBase + ".git"

	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		// Repo already present: update it.
		cmd := exec.Command("git", "-C", dir, "fetch", "--depth", "1", "origin")
		if out, err := cmd.CombinedOutput(); err != nil {
			return PkgFiles{}, fmt.Errorf("git fetch %s: %v: %s", pkgBase, err, out)
		}
		cmd = exec.Command("git", "-C", dir, "reset", "--hard", "origin/HEAD")
		if out, err := cmd.CombinedOutput(); err != nil {
			// origin/HEAD may not be set: try FETCH_HEAD.
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
		return PkgFiles{}, fmt.Errorf("%s: PKGBUILD not found", pkgBase)
	}
	return PkgFiles{PkgBase: pkgBase, Dir: dir, Files: files}, nil
}

// collectFiles reads the versioned files relevant to the review.
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
		if err != nil || info.Size() > 1<<20 { // skip huge files
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

// relevant decides whether a file should be included in the review.
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

// stripConstraint removes version constraints from a dependency
// (e.g. "glibc>=2.0" -> "glibc", "foo: descr" -> "foo").
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
