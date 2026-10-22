package mcpbridge

import (
	"strings"

	"golang.org/x/sys/windows/registry"
)

// uninstallKey is where the NSIS installer records SheetDiver
// (company + product name, as Wails' installer names it).
const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\SheetDiverSheetDiver`

// Installed finds the app through the installer's uninstall entry, whose
// DisplayIcon is the installed executable.
type Installed struct {
	// Root is registry.CURRENT_USER (a per-user install) or
	// registry.LOCAL_MACHINE (an all-users install).
	Root registry.Key
}

// Locate reads the entry's DisplayIcon.
func (i Installed) Locate() (string, bool) {
	k, err := registry.OpenKey(i.Root, uninstallKey, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if err != nil {
		return "", false
	}
	defer k.Close() //nolint:errcheck // a read-only key
	icon, _, err := k.GetStringValue("DisplayIcon")
	if err != nil {
		return "", false
	}
	// An icon reference may carry ",<index>".
	if n := strings.LastIndex(icon, ","); n > strings.LastIndex(icon, `\`) {
		icon = icon[:n]
	}
	return existing(strings.Trim(icon, `"`))
}

// installedLocators checks a per-user install, then an all-users one.
func installedLocators() []Locator {
	return []Locator{Installed{Root: registry.CURRENT_USER}, Installed{Root: registry.LOCAL_MACHINE}}
}
