//go:build nogui

package main

import "errors"

// runGUI is unavailable in headless builds (-tags nogui), which exist so the
// app can be built and tested without a webview toolkit installed.
func runGUI(port int) error {
	return errors.New("this build has no desktop UI; run `sheetdiver serve` to use the browser UI")
}
