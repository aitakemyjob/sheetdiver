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
	"github.com/aitakemyjob/sheetdiver/internal/catalog"
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
  sheetdiver import FILE|DIR|URL|MPN…  import datasheets, several at a time (part numbers are looked up)
  sheetdiver bom FILE [-project NAME]  build a project from a CSV/KiCad BOM, fetching missing datasheets
  sheetdiver report PROJECT     print a project's design review (Markdown)
  sheetdiver kicad PROJECT [DIR]  export a project as a KiCad schematic with a PDF layout guide
  sheetdiver catalog download|status|search WORDS…  the JLCPCB parts catalogue
  sheetdiver inspect FILE         show what extraction finds in a PDF (nothing is stored)
  sheetdiver licenses             print SheetDiver's licence and third-party notices
  sheetdiver version              print the version

Flags:
  -port N      MCP/HTTP port (default from settings, %d)
  -open        (serve) open the browser automatically (default true)
  -mpn LIST    (import) comma-separated part numbers, overriding detection
  -project N   (bom) project name (default: the file name)
  -fetch=false (bom) don't download missing datasheets

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
	project := fl.String("project", "", "(bom) project to fill (default: the file name)")
	fetch := fl.Bool("fetch", true, "(bom) download missing datasheets by part number")
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
	case "bom":
		err = runBOM(fl.Args(), *project, *fetch)
	case "catalog":
		err = runCatalog(fl.Args())
	case "licenses", "licences":
		fmt.Print(licenseText, "\n", noticesText)
	case "report":
		err = runReport(fl.Args())
	case "kicad":
		err = runKiCad(fl.Args())
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
	go a.core.ReextractOutdated(context.Background()) //nolint:errcheck // progress and failures show in the import queue
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

// runImport imports datasheets from the command line: PDF paths, folders
// (recursively), URLs and part numbers, several at a time.
func runImport(args []string, mpns string) error {
	if len(args) == 0 {
		return fmt.Errorf("give one or more PDF paths, folders, URLs or part numbers")
	}
	a, err := bootstrap(0, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	var reqs []core.ImportRequest
	for _, f := range args {
		st, statErr := os.Stat(f)
		switch {
		case strings.HasPrefix(f, "http://") || strings.HasPrefix(f, "https://"):
			reqs = append(reqs, core.ImportRequest{URL: f})
		case statErr == nil && st.IsDir():
			fr, err := core.FolderRequests(f, true)
			if err != nil {
				return err
			}
			reqs = append(reqs, fr...)
		case statErr != nil && !strings.ContainsAny(f, `/\.`):
			// Not a file: treat it as a part number to look up.
			reqs = append(reqs, core.ImportRequest{Lookup: f})
		default:
			reqs = append(reqs, core.ImportRequest{Path: f})
		}
	}
	if mpns != "" {
		for i := range reqs {
			reqs[i].MPNs = strings.Split(mpns, ",")
		}
	}
	failed := 0
	for _, o := range a.core.ImportBatch(context.Background(), reqs) {
		j := o.Job
		if j.Status == core.JobFailed {
			fmt.Fprintf(os.Stderr, "%s: %s\n", j.Source, j.Error)
			failed++
			continue
		}
		state := "imported"
		if j.Status == core.JobExisting {
			state = "already in library"
		}
		fmt.Printf("%s: %s as %s", j.Source, state, strings.Join(j.Parts, ", "))
		if o.Result != nil {
			fmt.Printf(" (%s, %s) %v %s", o.Result.Datasheet.Manufacturer, firstPartCategory(*o.Result), o.Result.Counts, o.Result.Took)
		}
		fmt.Println()
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d imports failed", failed, len(reqs))
	}
	return nil
}

// runCatalog downloads, describes or searches the parts catalogue.
func runCatalog(args []string) error {
	if len(args) == 0 {
		args = []string{"status"}
	}
	a, err := bootstrap(0, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	ctx := context.Background()
	switch args[0] {
	case "download", "refresh":
		last := ""
		err := a.core.Catalog.Download(ctx, func(p catalog.Progress) {
			line := p.Stage
			if p.Total > 0 {
				line = fmt.Sprintf("%s %d%%", p.Stage, 100*p.Done/p.Total)
			}
			if line != last {
				fmt.Fprintf(os.Stderr, "\r%-24s", line)
				last = line
			}
		})
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		fallthrough
	case "status":
		st := a.core.CatalogStatus(ctx)
		if !st.Installed {
			fmt.Println("not downloaded; run: sheetdiver catalog download")
			return nil
		}
		fmt.Printf("%d parts (%d in stock), published %s, %d days old, %d MB\n", st.Parts, st.InStock, st.Published.Format("2006-01-02 15:04"), st.AgeDays, st.SizeBytes>>20)
		return nil
	case "search":
		items, err := a.core.CatalogSearch(ctx, catalog.Query{Text: strings.Join(args[1:], " "), Limit: 30})
		if err != nil {
			return err
		}
		for _, it := range items {
			lib := ""
			if it.InLibrary != "" {
				lib = " [in library]"
			}
			fmt.Printf("%-10s %-22s %-24s %-12s %-9s %8d  %s%s\n", it.LCSC, it.MPN, it.Manufacturer, it.Package, it.Library, it.Stock, it.Description, lib)
		}
		return nil
	}
	return fmt.Errorf("catalog takes download, status or search")
}

// runReport prints a project's design-review report as Markdown.
func runReport(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("give one project name")
	}
	a, err := bootstrap(0, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	md, err := a.core.ProjectReport(context.Background(), args[0])
	if err != nil {
		return err
	}
	fmt.Print(md)
	return nil
}

// runKiCad exports a project as a KiCad schematic and layout guide.
func runKiCad(args []string) error {
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("give a project name and, optionally, the folder to write into")
	}
	a, err := bootstrap(0, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	dir := ""
	if len(args) == 2 {
		dir = args[1]
	}
	out, err := a.core.ExportKiCad(context.Background(), args[0], dir)
	if err != nil {
		return err
	}
	fmt.Printf("wrote %s to %s\n%d symbols, %d with a footprint\n", strings.Join(out.Files, ", "), out.Dir, out.Components, out.Footprints)
	for _, u := range out.Unplaced {
		fmt.Println("  no footprint:", u)
	}
	for _, o := range out.OffBoard {
		fmt.Println("  not on the board:", o)
	}
	return nil
}

// runBOM builds a project from a BOM file.
func runBOM(args []string, project string, fetch bool) error {
	if len(args) != 1 {
		return fmt.Errorf("give one BOM file (CSV/TSV)")
	}
	a, err := bootstrap(0, false)
	if err != nil {
		return err
	}
	defer a.core.Close()
	res, err := a.core.ImportBOM(context.Background(), core.BOMRequest{Path: args[0], Project: project, Fetch: fetch})
	if err != nil {
		return err
	}
	fmt.Printf("project %s: %d in library, %d imported, %d without datasheet, %d skipped\n", res.Project,
		res.Counts[core.BOMInLibrary], res.Counts[core.BOMImported], res.Counts[core.BOMNoDatasheet], res.Counts[core.BOMSkipped])
	for _, r := range res.Rows {
		note := ""
		if r.Note != "" {
			note = " — " + r.Note
		}
		fmt.Printf("  %-12s %-20s %s%s\n", strings.Join(r.Designators, ","), firstNonEmptyStr(r.MPN, r.Value), r.Status, note)
	}
	return nil
}

// firstNonEmptyStr returns a, or b when a is empty.
func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
	if err := openExternal(url); err != nil {
		slog.Warn("could not open browser", "err", err)
	}
}

// openExternal opens a URL or file with the system's default application
// (a PDF in the user's PDF viewer).
func openExternal(target string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	// Reap the opener in the background; it exits once it hands off.
	go func() { _ = cmd.Wait() }()
	return nil
}
