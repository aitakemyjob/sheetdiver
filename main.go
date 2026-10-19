// Command sheetdiver is a desktop app that turns component datasheets
// into a searchable library of parts, parameters, figures, pins and
// registers, with projects and design calculators, and a built-in MCP
// server so coding agents can work with all of it.
//
//	sheetdiver                 launch the desktop app
//	sheetdiver serve           run headless and open the UI in a browser
//	sheetdiver mcp             stdio MCP bridge to the running app (for stdio-only agents)
//	sheetdiver import FILE…    import datasheets (PDF paths or URLs) into the library
//	sheetdiver inspect FILE    print what extraction finds in a PDF, without importing
//	sheetdiver version
package main

import (
	"context"
	"embed"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/api"
	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/server"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `SheetDiver %s — your datasheet library, with an MCP server for agents.

Usage:
  sheetdiver [flags]              launch the desktop app
  sheetdiver serve [flags]        run without a window and use the UI in your browser
  sheetdiver mcp [flags]          stdio MCP bridge to the running app (Claude Desktop & other stdio-only clients)
  sheetdiver import FILE|URL|MPN… import datasheets (a part number is looked up at manufacturer sites)
  sheetdiver inspect FILE         show what extraction finds in a PDF (nothing is stored)
  sheetdiver version              print the version

Flags:
  -port N      MCP/HTTP port (default from settings, %d)
  -open        (serve) open the browser automatically (default true)
  -mpn LIST    (import) comma-separated part numbers, overriding detection

The MCP endpoint is http://127.0.0.1:<port>/mcp (streamable HTTP).
The library lives in %s (set SHEETDIVER_LIBRARY or Settings to move it).
`

// app is the wired-up application.
type app struct {
	core   *core.Service
	api    *api.API
	server *server.Server
}

// bootstrap loads the config, opens the library and wires the service,
// API and HTTP server. withUI also serves the embedded frontend.
func bootstrap(port int, withUI bool) (*app, error) {
	cfgPath, err := config.DefaultPath()
	if err != nil {
		return nil, err
	}
	store, err := config.Open(cfgPath)
	if err != nil {
		return nil, err
	}
	svc, err := core.New(store, version)
	if err != nil {
		return nil, err
	}
	if port == 0 {
		port = store.Get().Settings.MCPPort
	}
	a := api.New(svc)
	opts := server.Options{Port: port}
	if withUI {
		ui, err := fs.Sub(assets, "frontend/dist")
		if err != nil {
			return nil, err
		}
		opts.UI = ui
	}
	return &app{core: svc, api: a, server: server.New(svc, a, opts)}, nil
}

// main dispatches the subcommands.
func main() {
	args := os.Args[1:]
	cmd := ""
	if len(args) > 0 && len(args[0]) > 0 && args[0][0] != '-' {
		cmd, args = args[0], args[1:]
	}
	fl := flag.NewFlagSet("sheetdiver", flag.ExitOnError)
	port := fl.Int("port", 0, "MCP/HTTP port")
	open := fl.Bool("open", true, "open the browser (serve)")
	standalone := fl.Bool("standalone", false, "(mcp) serve stdio directly instead of bridging to the app")
	mpn := fl.String("mpn", "", "(import) comma-separated part numbers")
	fl.Usage = func() { fmt.Fprintf(os.Stderr, usage, version, config.DefaultMCPPort, config.DefaultLibraryDir()) }
	fl.Parse(args)

	var err error
	switch cmd {
	case "", "gui":
		err = runGUI(*port)
	case "serve":
		err = runServe(*port, *open)
	case "mcp":
		err = runMCP(*port, *standalone)
	case "import":
		err = runImport(fl.Args(), *mpn)
	case "inspect":
		err = runInspect(fl.Args())
	case "version", "--version":
		fmt.Println("sheetdiver", version)
	case "help":
		fl.Usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		fl.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sheetdiver:", err)
		os.Exit(1)
	}
}

// runServe runs headless, serving the UI and MCP over HTTP until
// interrupted, and optionally opens the browser.
func runServe(port int, open bool) error {
	a, err := bootstrap(port, true)
	if err != nil {
		return err
	}
	defer a.core.Close()
	if err := a.server.Start(); err != nil {
		return err
	}
	url := a.server.URL()
	fmt.Printf("SheetDiver %s\n  UI:      %s\n  MCP:     %s/mcp\n  Library: %s\n", version, url, url, a.core.Config.LibraryDir())
	if open {
		openBrowser(url)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return a.server.Shutdown(shutdown)
}

// runImport imports datasheets from the command line.
func runImport(files []string, mpns string) error {
	if len(files) == 0 {
		return fmt.Errorf("give one or more PDF paths or URLs")
	}
	a, err := bootstrap(0, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	ctx := context.Background()
	failed := 0
	for _, f := range files {
		req := core.ImportRequest{Path: f}
		if strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://") {
			req = core.ImportRequest{URL: f}
		} else if _, err := os.Stat(f); err != nil && !strings.ContainsAny(f, `/\.`) {
			// Not a file: treat it as a part number to look up.
			req = core.ImportRequest{Lookup: f}
		}
		if mpns != "" {
			req.MPNs = strings.Split(mpns, ",")
		}
		res, err := a.core.Import(ctx, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", f, err)
			failed++
			continue
		}
		var names []string
		for _, p := range res.Parts {
			names = append(names, p.MPN)
		}
		state := "imported"
		if res.Existing {
			state = "already in library"
		}
		fmt.Printf("%s: %s as %s (%s, %s) %v %s\n", f, state, strings.Join(names, ", "), res.Datasheet.Manufacturer, firstPartCategory(res), res.Counts, res.Took)
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d imports failed", failed, len(files))
	}
	return nil
}

// firstPartCategory returns the category of an import's first part.
func firstPartCategory(r core.ImportResult) string {
	if len(r.Parts) == 0 {
		return "?"
	}
	return r.Parts[0].Category
}

// openBrowser opens url in the default browser (best effort).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("could not open browser", "err", err)
	}
}
