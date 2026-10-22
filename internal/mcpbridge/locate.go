package mcpbridge

import (
	"os"
	"path/filepath"
	"runtime"
)

// Locator finds the SheetDiver app's executable. A locator that can't
// returns ok=false and the next one in a chain is asked.
type Locator interface {
	Locate() (path string, ok bool)
}

// Chain asks each locator in turn.
type Chain []Locator

// Locate returns the first executable found.
func (c Chain) Locate() (string, bool) {
	for _, l := range c {
		if p, ok := l.Locate(); ok {
			return p, true
		}
	}
	return "", false
}

// AppPath is an executable path given explicitly (the --app flag); ""
// means none was given.
type AppPath string

// Locate returns the path when the file exists.
func (p AppPath) Locate() (string, bool) { return existing(string(p)) }

// Beside finds the app next to an executable, as the installer places
// sheetdiver-mcp.exe beside sheetdiver.exe. Dir "" means this program's
// own folder.
type Beside struct{ Dir string }

// Locate looks for the app executable in the folder.
func (b Beside) Locate() (string, bool) {
	dir := b.Dir
	if dir == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", false
		}
		if r, err := filepath.EvalSymlinks(exe); err == nil {
			exe = r
		}
		dir = filepath.Dir(exe)
	}
	return existing(filepath.Join(dir, AppExecutable))
}

// AppExecutable is the app's file name on this platform.
var AppExecutable = func() string {
	if runtime.GOOS == "windows" {
		return "sheetdiver.exe"
	}
	return "sheetdiver"
}()

// DefaultLocators is where the app is looked for when no path is given:
// beside this program, then wherever the platform records installs (the
// Windows installer's registry entry).
func DefaultLocators() Chain {
	return append(Chain{Beside{}}, installedLocators()...)
}

// existing returns path when it names a regular file.
func existing(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return "", false
	}
	return path, true
}
