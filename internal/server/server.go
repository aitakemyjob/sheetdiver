// Package server hosts SheetDiver's local HTTP endpoint: the MCP streamable
// transport for agents and, in browser mode, the UI, its JSON API and an
// event stream. It only ever binds to loopback.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/api"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/mcpserver"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// HeaderName must accompany API calls. Browsers cannot attach custom headers
// cross-origin without a CORS preflight, which this server never approves,
// so the header doubles as CSRF protection.
const HeaderName = "X-SheetDiver"

type Options struct {
	Port int
	// UI, when set, serves the frontend and the /api endpoints (browser mode).
	UI fs.FS
}

type Server struct {
	core *core.Service
	api  *api.API
	mcp  *mcpserver.Server
	opts Options
	http *http.Server
	ln   net.Listener
	// stopStreams cancels the base context of every request, ending
	// long-lived streams (agent MCP sessions, the UI event stream).
	stopStreams context.CancelFunc
}

// shutdownGrace bounds how long Shutdown waits when its context has no
// deadline (the desktop app passes one that never expires).
const shutdownGrace = 3 * time.Second

// New creates the HTTP server; the MCP endpoint is always served and the
// UI/API only when opts.UI is set.
func New(c *core.Service, a *api.API, opts Options) *Server {
	return &Server{core: c, api: a, mcp: mcpserver.New(c), opts: opts}
}

// Handler returns the routed, loopback-only HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/mcp", s.mcp.Handler())
	mux.Handle("/mcp/", s.mcp.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "app": "sheetdiver", "version": s.core.Version})
	})
	if s.opts.UI != nil {
		mux.HandleFunc("/api/invoke", s.guard(s.invoke))
		mux.HandleFunc("/api/events", s.guard(s.events))
		mux.HandleFunc("/api/upload", s.guard(s.upload))
		mux.HandleFunc("/api/page.png", s.guard(s.pageImage))
		mux.HandleFunc("/api/figure.png", s.guard(s.figureImage))
		mux.HandleFunc("/api/table.png", s.guard(s.tableImage))
		mux.HandleFunc("/api/datasheet.pdf", s.guard(s.datasheetPDF))
		mux.Handle("/", spa(s.opts.UI))
	}
	return localOnly(mux)
}

// AssetHandler serves the binary endpoints (page and figure images,
// PDFs, uploads) to the desktop webview through Wails' asset server, which
// only the app's own window can reach.
func (s *Server) AssetHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/upload", s.upload)
	mux.HandleFunc("/api/page.png", s.pageImage)
	mux.HandleFunc("/api/figure.png", s.figureImage)
	mux.HandleFunc("/api/table.png", s.tableImage)
	mux.HandleFunc("/api/datasheet.pdf", s.datasheetPDF)
	return mux
}

// localOnly rejects requests whose Host isn't loopback (DNS rebinding).
func localOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		switch strings.Trim(host, "[]") {
		case "127.0.0.1", "localhost", "::1":
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "forbidden host", http.StatusForbidden)
		}
	})
}

// guard enforces same-origin API access.
func (s *Server) guard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && !sameHost(origin, r.Host) {
			http.Error(w, "cross-origin request refused", http.StatusForbidden)
			return
		}
		// EventSource and <img> can't set headers; they are read-only GETs,
		// so allow a query flag for those.
		if r.Header.Get(HeaderName) == "" && (r.Method != http.MethodGet || r.URL.Query().Get("sd") != "1") {
			http.Error(w, "missing "+HeaderName+" header", http.StatusForbidden)
			return
		}
		h(w, r)
	}
}

// sameHost reports whether an Origin header names the requested host.
func sameHost(origin, host string) bool {
	o := strings.TrimPrefix(strings.TrimPrefix(origin, "http://"), "https://")
	return o == host
}

type invokeReq struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
}

// invoke handles POST /api/invoke: {method, args} → {result} or {error}.
func (s *Server) invoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	var req invokeReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<20)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	out, err := s.api.Invoke(r.Context(), req.Method, req.Args)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"result":%s}`, out)
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

// events streams bus events to the browser as Server-Sent Events, with
// periodic keepalive comments.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	ch, cancel := s.core.Bus.Subscribe()
	defer cancel()
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		case ev, ok := <-ch:
			if !ok {
				return
			}
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

// upload handles POST /api/upload: a PDF body (filename in the query)
// imported into the library.
func (s *Server) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, 200<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	q := r.URL.Query()
	req := core.ImportRequest{Data: data, Filename: q.Get("filename"), Manufacturer: q.Get("manufacturer"), Category: q.Get("category")}
	if m := strings.TrimSpace(q.Get("mpns")); m != "" {
		req.MPNs = strings.FieldsFunc(m, func(r rune) bool { return r == ',' || r == ' ' })
	}
	res, err := s.core.Import(r.Context(), req)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"result": res})
}

// intParam reads an integer query parameter.
func intParam(r *http.Request, name string) int64 {
	v, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return v
}

// floatParam reads a float query parameter.
func floatParam(r *http.Request, name string) float64 {
	v, _ := strconv.ParseFloat(r.URL.Query().Get(name), 64)
	return v
}

// sendPNG writes a cacheable PNG response.
func sendPNG(w http.ResponseWriter, png []byte, err error) {
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(png)
}

// pageImage serves GET /api/page.png?ds=&page=&dpi=[&x0&y0&x1&y1].
func (s *Server) pageImage(w http.ResponseWriter, r *http.Request) {
	var box *pdf.Rect
	if r.URL.Query().Has("x1") {
		box = &pdf.Rect{X0: floatParam(r, "x0"), Y0: floatParam(r, "y0"), X1: floatParam(r, "x1"), Y1: floatParam(r, "y1")}
	}
	png, err := s.core.RenderPage(r.Context(), intParam(r, "ds"), int(intParam(r, "page")), box, int(intParam(r, "dpi")))
	sendPNG(w, png, err)
}

// figureImage serves GET /api/figure.png?id=&dpi=.
func (s *Server) figureImage(w http.ResponseWriter, r *http.Request) {
	_, png, err := s.core.RenderFigure(r.Context(), intParam(r, "id"), int(intParam(r, "dpi")))
	sendPNG(w, png, err)
}

// tableImage serves GET /api/table.png?id=&dpi=.
func (s *Server) tableImage(w http.ResponseWriter, r *http.Request) {
	_, png, err := s.core.RenderTable(r.Context(), intParam(r, "id"), int(intParam(r, "dpi")))
	sendPNG(w, png, err)
}

// datasheetPDF serves GET /api/datasheet.pdf?ds=: the original PDF.
func (s *Server) datasheetPDF(w http.ResponseWriter, r *http.Request) {
	d, data, err := s.core.DatasheetPDF(r.Context(), intParam(r, "ds"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", d.Filename))
	w.Write(data)
}

// spa serves static files, falling back to index.html for client routes.
func spa(ui fs.FS) http.Handler {
	files := http.FileServer(http.FS(ui))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			p = "index.html"
		}
		if _, err := fs.Stat(ui, p); err != nil {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		files.ServeHTTP(w, r)
	})
}

// Start binds 127.0.0.1:port and serves in the background.
func (s *Server) Start() error {
	addr := fmt.Sprintf("127.0.0.1:%d", s.opts.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		s.core.SetMCPStatus(core.MCPStatus{Error: fmt.Sprintf("cannot listen on %s: %v", addr, err)})
		return err
	}
	s.ln = ln
	base, stop := context.WithCancel(context.Background())
	s.stopStreams = stop
	s.http = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return base },
	}
	url := "http://" + ln.Addr().String()
	s.core.SetMCPStatus(core.MCPStatus{Running: true, URL: url + "/mcp"})
	go func() {
		if err := s.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("http server stopped", "err", err)
			s.core.SetMCPStatus(core.MCPStatus{Error: err.Error()})
		}
	}()
	return nil
}

// URL is the base URL once started.
func (s *Server) URL() string {
	if s.ln == nil {
		return ""
	}
	return "http://" + s.ln.Addr().String()
}

// Shutdown stops the server and marks MCP as stopped. Streaming requests
// are cancelled first, since they never finish on their own; whatever is
// still open after the grace period is closed forcibly.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.http == nil {
		return nil
	}
	s.core.SetMCPStatus(core.MCPStatus{})
	s.stopStreams()
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, shutdownGrace)
		defer cancel()
	}
	if err := s.http.Shutdown(ctx); err != nil {
		_ = s.http.Close()
		return err
	}
	return nil
}
