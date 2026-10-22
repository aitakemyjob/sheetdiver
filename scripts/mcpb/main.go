// Command mcpb builds the Windows Claude Desktop extension
// (sheetdiver.mcpb) around a built console bridge, for releases. The app
// builds the same extension from its own install (Connect an agent ›
// Claude Desktop).
//
//	go run ./scripts/mcpb -bridge build/mcp/sheetdiver-mcp.exe -version v1.2.3 -o dist/sheetdiver_windows_amd64.mcpb
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/aitakemyjob/sheetdiver/internal/connect"
)

// main parses the flags and builds the extension.
func main() {
	bridge := flag.String("bridge", "", "the console bridge built for Windows (sheetdiver-mcp.exe)")
	version := flag.String("version", "dev", "SheetDiver version")
	out := flag.String("o", "sheetdiver.mcpb", "extension to write")
	flag.Parse()
	if err := run(*bridge, *version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mcpb:", err)
		os.Exit(1)
	}
}

// run builds the extension and writes it.
func run(bridge, version, out string) error {
	if bridge == "" {
		return fmt.Errorf("-bridge is required")
	}
	b, err := os.ReadFile(bridge)
	if err != nil {
		return err
	}
	zip, err := connect.Extension{Version: version, Platform: "windows", Bridge: b}.Build()
	if err != nil {
		return err
	}
	if err := os.WriteFile(out, zip, 0o644); err != nil {
		return err
	}
	fmt.Println(out)
	return nil
}
