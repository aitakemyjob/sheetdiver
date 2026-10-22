//go:build !windows

package mcpbridge

// installedLocators: only Windows records installs where the bridge can
// look them up.
func installedLocators() []Locator { return nil }
