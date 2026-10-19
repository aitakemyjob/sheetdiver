//go:build !nogui

package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/aitakemyjob/sheetdiver/internal/api"
	"github.com/aitakemyjob/sheetdiver/internal/core"
)

// Desktop is bound into the webview as window.go.main.Desktop.
type Desktop struct {
	api  *api.API
	core *core.Service

	// Wails runs OnStartup in a goroutine concurrently with the page load
	// (on Linux), so the runtime context arrives asynchronously.
	ctx   atomic.Pointer[context.Context]
	ready chan struct{}
}

// newDesktop creates the bound object. The folder picker is registered up
// front (not in startup) so the UI never sees it as unavailable; it waits
// for the window's runtime context before opening the dialog.
func newDesktop(a *api.API, c *core.Service) *Desktop {
	d := &Desktop{api: a, core: c, ready: make(chan struct{})}
	a.PickPDFs = d.pickPDFs
	a.SaveFile = d.saveFile
	return d
}

// runtimeCtx returns the Wails runtime context, waiting briefly for
// startup if it hasn't run yet.
func (d *Desktop) runtimeCtx() (context.Context, error) {
	select {
	case <-d.ready:
		return *d.ctx.Load(), nil
	case <-time.After(5 * time.Second):
		return nil, errors.New("the window is still starting; try again")
	}
}

// pickPDFs opens the native file chooser for datasheets.
func (d *Desktop) pickPDFs() ([]string, error) {
	ctx, err := d.runtimeCtx()
	if err != nil {
		return nil, err
	}
	return wruntime.OpenMultipleFilesDialog(ctx, wruntime.OpenDialogOptions{
		Title:   "Import datasheets",
		Filters: []wruntime.FileFilter{{DisplayName: "PDF datasheets", Pattern: "*.pdf;*.PDF"}},
	})
}

// saveFile asks where to save an exported file and writes it there. An
// empty path means the developer cancelled.
func (d *Desktop) saveFile(name string, data []byte) (string, error) {
	ctx, err := d.runtimeCtx()
	if err != nil {
		return "", err
	}
	opts := wruntime.SaveDialogOptions{Title: "Export", DefaultFilename: name, CanCreateDirectories: true}
	if ext := filepath.Ext(name); ext != "" {
		opts.Filters = []wruntime.FileFilter{{DisplayName: strings.ToUpper(ext[1:]) + " files", Pattern: "*" + ext}}
	}
	path, err := wruntime.SaveFileDialog(ctx, opts)
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += filepath.Ext(name)
	}
	return path, os.WriteFile(path, data, 0o644)
}

// Invoke calls an API method; args and result are JSON strings. Calls made
// before startup completes use a background context.
func (d *Desktop) Invoke(method, args string) (string, error) {
	ctx := context.Background()
	if p := d.ctx.Load(); p != nil {
		ctx = *p
	}
	out, err := d.api.Invoke(ctx, method, json.RawMessage(args))
	return string(out), err
}

// startup runs when the window is ready: it records the runtime context
// and forwards bus events to the webview.
func (d *Desktop) startup(ctx context.Context) {
	d.ctx.Store(&ctx)
	close(d.ready)
	// Datasheets dropped on the window are imported directly.
	wruntime.OnFileDrop(ctx, func(_, _ int, paths []string) {
		d.core.Bus.Publish("drop", map[string]any{"paths": paths})
	})
	ch, cancel := d.core.Bus.Subscribe()
	go func() {
		defer cancel()
		for ev := range ch {
			wruntime.EventsEmit(ctx, "sd:event", ev)
		}
	}()
}

// runGUI starts the MCP server and opens the desktop window. A port
// conflict is shown in the UI rather than preventing startup.
func runGUI(port int) error {
	a, err := bootstrap(port, false)
	if err != nil {
		return err
	}
	// The MCP server failing to bind (e.g. port in use) must not stop the UI;
	// the status bar surfaces the error.
	_ = a.server.Start()
	desk := newDesktop(a.api, a.core)
	bg := &options.RGBA{R: 11, G: 17, B: 32, A: 255} // Deep Dive --bg
	return wails.Run(&options.App{
		Title:            "SheetDiver",
		Width:            1480,
		Height:           920,
		MinWidth:         980,
		MinHeight:        620,
		BackgroundColour: bg,
		AssetServer:      &assetserver.Options{Assets: assets, Handler: a.server.AssetHandler()},
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true, DisableWebViewDrop: true},
		OnStartup:        desk.startup,
		OnShutdown: func(context.Context) {
			// Wails' context never expires; Shutdown bounds its own wait.
			_ = a.server.Shutdown(context.Background())
			_ = a.core.Close()
		},
		Bind: []interface{}{desk},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarHiddenInset(),
			Appearance:           mac.DefaultAppearance,
			WebviewIsTransparent: false,
			About:                &mac.AboutInfo{Title: "SheetDiver", Message: "Your datasheet library, shared with your AI agent. " + version},
		},
		Windows: &windows.Options{
			WebviewIsTransparent: false,
			Theme:                windows.SystemDefault,
		},
		Linux: &linux.Options{
			ProgramName:         "SheetDiver",
			WebviewGpuPolicy:    linux.WebviewGpuPolicyOnDemand,
			WindowIsTranslucent: false,
		},
	})
}
