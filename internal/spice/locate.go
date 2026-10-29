// Package spice runs circuit simulations with ngspice: it finds the
// ngspice program the user installed, checks an agent's netlist against
// what a simulation may do, builds the deck ngspice runs (with the
// part models it uses and SheetDiver's own control block), runs it in a
// folder of its own with time and size limits, and reads back the
// results: every analysis's vectors from the raw file and the .meas
// values from the log. It knows nothing of projects or the library.
package spice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// Install is an ngspice program found on this computer and its version.
type Install struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	// From says how it was found: "settings", "PATH" or "default".
	From string `json:"from"`
}

// Finder is one way of finding ngspice: a path that may be it, or "".
type Finder interface {
	Find() (path, from string)
}

// configured is the path the user set.
type configured string

// Find is the configured path, when one is set.
func (c configured) Find() (string, string) { return string(c), "settings" }

// onPath finds ngspice on the PATH.
type onPath struct{}

// Find looks up ngspice (on Windows its console build first) on the PATH.
func (onPath) Find() (string, string) {
	for _, name := range []string{"ngspice_con", "ngspice"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, "PATH"
		}
	}
	return "", ""
}

// knownPlaces finds ngspice where its installers and package managers
// put it on each system.
type knownPlaces struct {
	goos string
}

// Find is the first of the system's usual places that exists.
func (k knownPlaces) Find() (string, string) {
	for _, p := range defaultPaths(k.goos) {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, "default"
		}
	}
	return "", ""
}

// defaultPaths are where ngspice usually is: the Windows zip's Spice64
// folder (its console build, which runs in batch mode), Homebrew on
// macOS, the distributions' packages on Linux.
func defaultPaths(goos string) []string {
	switch goos {
	case "windows":
		var out []string
		for _, root := range []string{`C:\Spice64`, filepath.Join(os.Getenv("ProgramFiles"), "Spice64"), filepath.Join(os.Getenv("ProgramFiles"), "ngspice")} {
			out = append(out, filepath.Join(root, "bin", "ngspice_con.exe"), filepath.Join(root, "bin", "ngspice.exe"))
		}
		return out
	case "darwin":
		return []string{"/opt/homebrew/bin/ngspice", "/usr/local/bin/ngspice", "/Applications/ngspice/bin/ngspice"}
	}
	return []string{"/usr/bin/ngspice", "/usr/local/bin/ngspice", "/snap/bin/ngspice"}
}

// Finders is how ngspice is looked for, in order: the configured path
// (when set, the only place looked), the PATH, then the usual places.
func Finders(path string) []Finder {
	if strings.TrimSpace(path) != "" {
		return []Finder{configured(strings.TrimSpace(path))}
	}
	return []Finder{onPath{}, knownPlaces{runtime.GOOS}}
}

// ErrNotFound is returned when ngspice isn't installed where it was
// looked for.
var ErrNotFound = errors.New("ngspice wasn't found: install it (ngspice.sourceforge.io; apt install ngspice; brew install ngspice) or set its path in Settings")

// Locate finds ngspice and asks it its version.
func Locate(ctx context.Context, path string) (Install, error) {
	for _, f := range Finders(path) {
		p, from := f.Find()
		if p == "" {
			continue
		}
		v, err := version(ctx, p)
		if err != nil {
			return Install{Path: p, From: from}, err
		}
		return Install{Path: p, Version: v, From: from}, nil
	}
	return Install{}, ErrNotFound
}

// versionRe is ngspice's version in its banner ("ngspice-42").
var versionRe = regexp.MustCompile(`ngspice-(\S+)`)

// version runs ngspice -v and reads its version.
func version(ctx context.Context, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "-v").CombinedOutput() // #nosec G204 -- the user's configured ngspice
	if m := versionRe.FindStringSubmatch(string(out)); m != nil {
		return m[1], nil
	}
	if err != nil {
		return "", fmt.Errorf("%s doesn't run: %w", path, err)
	}
	return "", fmt.Errorf("%s doesn't say it's ngspice", path)
}
