package server

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aitakemyjob/sheetdiver/internal/api"
	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// setup serves the UI, API and MCP over a temporary library.
func setup(t *testing.T) (*httptest.Server, *Server) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	store, err := config.Open(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(store, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>sheetdiver</html>")}}
	srv := New(svc, api.New(svc), Options{UI: ui})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, srv
}

// do sends a request with optional header and body.
func do(t *testing.T, method, url string, body []byte, header bool) (*http.Response, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, url, bytes.NewReader(body))
	if header {
		req.Header.Set(HeaderName, "1")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestUploadInvokeAndImages(t *testing.T) {
	ts, _ := setup(t)
	resp, body := do(t, http.MethodPost, ts.URL+"/api/upload?filename=sd.pdf", testutil.Datasheet(), true)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"mpn":"SD1234"`) {
		t.Fatalf("upload: %d %s", resp.StatusCode, body)
	}
	resp, body = do(t, http.MethodPost, ts.URL+"/api/invoke", []byte(`{"method":"part.detail","args":{"part":"SD1235DDCR"}}`), true)
	var out struct {
		Result core.PartDetail `json:"result"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Result.Part.MPN != "SD1235" {
		t.Fatalf("invoke: %d %s", resp.StatusCode, body)
	}
	dsID := out.Result.Datasheet.ID
	imgURL := ts.URL + "/api/page.png?sd=1&page=1&dpi=40&ds=" + itoa(dsID)
	resp, img := do(t, http.MethodGet, imgURL, nil, false)
	if m, err := png.Decode(bytes.NewReader(img)); err != nil || m.Bounds().Dx() != 340 {
		t.Fatalf("page image: %v", err)
	}
	// Images are revalidated by content (IDs are reused after deletes).
	tag := resp.Header.Get("ETag")
	if tag == "" || !strings.Contains(resp.Header.Get("Cache-Control"), "no-cache") {
		t.Fatalf("image caching: %v", resp.Header)
	}
	req, _ := http.NewRequest(http.MethodGet, imgURL, nil)
	req.Header.Set("If-None-Match", tag)
	if r2, err := http.DefaultClient.Do(req); err != nil || r2.StatusCode != http.StatusNotModified {
		t.Errorf("revalidation: %v %v", r2, err)
	} else {
		r2.Body.Close()
	}
	resp, pdf := do(t, http.MethodGet, ts.URL+"/api/datasheet.pdf?sd=1&ds="+itoa(dsID), nil, false)
	if resp.Header.Get("Content-Type") != "application/pdf" || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Error("original PDF")
	}
	if resp, body := do(t, http.MethodPost, ts.URL+"/api/invoke", []byte(`{"method":"nope"}`), true); resp.StatusCode != 200 || !strings.Contains(string(body), "unknown method") {
		t.Errorf("unknown method: %s", body)
	}
}

func TestGuards(t *testing.T) {
	ts, _ := setup(t)
	if resp, _ := do(t, http.MethodPost, ts.URL+"/api/invoke", []byte(`{"method":"app.state"}`), false); resp.StatusCode != http.StatusForbidden {
		t.Errorf("missing header: %d", resp.StatusCode)
	}
	// The query flag only works for read-only GETs (images, events).
	if resp, _ := do(t, http.MethodPost, ts.URL+"/api/upload?sd=1", []byte("%PDF"), false); resp.StatusCode != http.StatusForbidden {
		t.Errorf("upload with query flag: %d", resp.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/invoke", strings.NewReader(`{"method":"app.state"}`))
	req.Header.Set(HeaderName, "1")
	req.Header.Set("Origin", "http://evil.example")
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin: %v %v", resp.StatusCode, err)
	}
	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
	req.Host = "attacker.example"
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("DNS rebinding: %v %v", resp.StatusCode, err)
	}
	if resp, body := do(t, http.MethodGet, ts.URL+"/healthz", nil, false); resp.StatusCode != 200 || !strings.Contains(string(body), `"app":"sheetdiver"`) {
		t.Errorf("healthz: %s", body)
	}
	if _, body := do(t, http.MethodGet, ts.URL+"/some/route", nil, false); !strings.Contains(string(body), "sheetdiver") {
		t.Error("client routes fall back to index.html")
	}
}

func TestMethodsAreRegistered(t *testing.T) {
	_, srv := setup(t)
	have := map[string]bool{}
	for _, m := range srv.api.Methods() {
		have[m] = true
	}
	for _, m := range []string{"app.state", "datasheets.import", "part.detail", "parameters.record", "figure.update", "search", "project.detail", "rails.set", "projectParts.add", "calc.run", "notes.add"} {
		if !have[m] {
			t.Errorf("method %s is missing", m)
		}
	}
}

// itoa formats an ID.
func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func TestTranslationFiles(t *testing.T) {
	files := testutil.StubTranslationModel(t)
	ts, srv := setup(t)
	// Nothing installed: an empty registry, and no files.
	if resp, body := do(t, http.MethodGet, ts.URL+"/api/translation/registry.json", nil, true); resp.StatusCode != 200 || string(body) != "{}" {
		t.Fatalf("empty registry: %d %s", resp.StatusCode, body)
	}
	if err := srv.core.Translate.Download(context.Background(), "zh", "en", nil); err != nil {
		t.Fatal(err)
	}
	// The engine's worker can't set headers: the sd flag lets it read.
	resp, body := do(t, http.MethodGet, ts.URL+"/api/translation/registry.json?sd=1", nil, false)
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"/api/translation/zhen/vocab.zhen.spm?sd=1"`) {
		t.Fatalf("registry: %d %s", resp.StatusCode, body)
	}
	if resp, body := do(t, http.MethodGet, ts.URL+"/api/translation/zhen/vocab.zhen.spm?sd=1", nil, false); resp.StatusCode != 200 || !bytes.Equal(body, files["vocab.zhen.spm"]) {
		t.Errorf("model file: %d %q", resp.StatusCode, body)
	}
	for _, bad := range []string{"zhen/../config.json", "zhen/other.bin", "enzh/vocab.zhen.spm", "vocab.zhen.spm"} {
		if resp, _ := do(t, http.MethodGet, ts.URL+"/api/translation/"+bad+"?sd=1", nil, false); resp.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", bad, resp.StatusCode)
		}
	}
	// Cross-origin pages can't read the models.
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/translation/registry.json?sd=1", nil)
	req.Header.Set("Origin", "https://example.com")
	if r, err := http.DefaultClient.Do(req); err != nil || r.StatusCode != http.StatusForbidden {
		t.Errorf("cross-origin: %v %v", r, err)
	} else {
		r.Body.Close()
	}
}

func TestTranslationMethods(t *testing.T) {
	testutil.StubTranslationModel(t)
	ts, _ := setup(t)
	call := func(method, args string) string {
		t.Helper()
		resp, body := do(t, http.MethodPost, ts.URL+"/api/invoke", []byte(`{"method":"`+method+`","args":`+args+`}`), true)
		if resp.StatusCode != 200 || strings.Contains(string(body), `"error"`) {
			t.Fatalf("%s: %d %s", method, resp.StatusCode, body)
		}
		return string(body)
	}
	if out := call("translate.status", `{}`); !strings.Contains(out, `"installed":false`) {
		t.Errorf("status: %s", out)
	}
	call("translations.save", `{"items":[{"lang":"zh","source":"静态电流","text":"Quiescent current"},{"lang":"zh","source":" ","text":"skipped"}]}`)
	if out := call("translations.get", `{"lang":"zh","texts":["静态电流","未知"]}`); !strings.Contains(out, `"静态电流":"Quiescent current"`) || strings.Contains(out, "未知") {
		t.Errorf("get: %s", out)
	}
	resp, body := do(t, http.MethodPost, ts.URL+"/api/upload?filename=sd.pdf", testutil.Datasheet(), true)
	if resp.StatusCode != 200 {
		t.Fatalf("upload: %s", body)
	}
	if out := call("datasheet.translationSources", `{"id":1}`); !strings.Contains(out, `"lang":"en"`) {
		t.Errorf("sources: %s", out)
	}
	if out := call("datasheet.reextract", `{"id":1,"translated":true}`); !strings.Contains(out, `"datasheet"`) || strings.Contains(out, `"translated":"en"`) {
		t.Errorf("an English datasheet isn't marked translated: %s", out)
	}
	if out := call("translate.remove", `{"from":"zh","to":"en"}`); !strings.Contains(out, `"installed":false`) {
		t.Errorf("remove: %s", out)
	}
}

// TestMCPPort gives MCP a port of its own: the UI port keeps the health
// check and loses /mcp, the MCP port serves /mcp and the health check and
// nothing else, and the status points at the MCP port.
func TestMCPPort(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	store, err := config.Open(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(store, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>sheetdiver</html>")}}
	srv := New(svc, api.New(svc), Options{Port: 0, MCPPort: freePort(t), UI: ui})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	if srv.MCPURL() == srv.URL() {
		t.Fatalf("MCP shares the UI port %s", srv.URL())
	}
	if st := svc.MCPStatus(); st.URL != srv.MCPURL()+"/mcp" {
		t.Errorf("status URL = %q, want %q", st.URL, srv.MCPURL()+"/mcp")
	}
	// On the UI port, /mcp is just another page of the app.
	for _, tc := range []struct {
		url  string
		want int
		body string
	}{
		{srv.URL() + "/healthz", http.StatusOK, `"status":"ok"`},
		{srv.URL() + "/", http.StatusOK, "<html>"},
		{srv.URL() + "/mcp", http.StatusOK, "<html>"},
		{srv.MCPURL() + "/healthz", http.StatusOK, `"status":"ok"`},
		{srv.MCPURL() + "/", http.StatusNotFound, ""},
	} {
		resp, body := do(t, http.MethodGet, tc.url, nil, false)
		if resp.StatusCode != tc.want || !strings.Contains(string(body), tc.body) {
			t.Errorf("GET %s = %d %q, want %d with %q", tc.url, resp.StatusCode, body, tc.want, tc.body)
		}
	}
	// The MCP endpoint answers on the MCP port (an initialize, as a client
	// sends it).
	init := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	req, _ := http.NewRequest(http.MethodPost, srv.MCPURL()+"/mcp", bytes.NewReader(init))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("initialize on the MCP port = %d", resp.StatusCode)
	}
	if err := srv.Shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
	if _, err := http.Get(srv.MCPURL() + "/healthz"); err == nil {
		t.Error("the MCP port is still open after shutdown")
	}
}

// TestHosts accepts a reverse proxy's names given in Options.Hosts, in any
// case and with a port, and still refuses every other host; the UI's API
// then works from the proxy's origin.
func TestHosts(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	store, err := config.Open(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(store, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	ui := fstest.MapFS{"index.html": {Data: []byte("<html>sheetdiver</html>")}}
	srv := New(svc, api.New(svc), Options{UI: ui, Hosts: []string{"sheets.example.com", " Lab.local "}})
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	for host, want := range map[string]int{
		"sheets.example.com":      http.StatusOK,
		"SHEETS.example.com:8443": http.StatusOK,
		"lab.local":               http.StatusOK,
		"127.0.0.1:4000":          http.StatusOK,
		"attacker.example":        http.StatusForbidden,
		"example.com":             http.StatusForbidden,
	} {
		req, _ := http.NewRequest(http.MethodGet, ts.URL+"/healthz", nil)
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("Host %q: %d, want %d", host, resp.StatusCode, want)
		}
	}
	// An API call as the browser makes it through the proxy.
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/invoke", strings.NewReader(`{"method":"app.state"}`))
	req.Host = "sheets.example.com"
	req.Header.Set(HeaderName, "1")
	req.Header.Set("Origin", "https://sheets.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("API through the proxy: %d", resp.StatusCode)
	}
	// MCP too, past the SDK's own loopback check.
	init := []byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"t","version":"0"}}}`)
	req, _ = http.NewRequest(http.MethodPost, ts.URL+"/mcp", bytes.NewReader(init))
	req.Host = "sheets.example.com"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("MCP through the proxy: %d", resp.StatusCode)
	}
	// Nothing in the list: loopback only, as before.
	plain := httptest.NewServer(New(svc, api.New(svc), Options{UI: ui}).Handler())
	t.Cleanup(plain.Close)
	req, _ = http.NewRequest(http.MethodGet, plain.URL+"/healthz", nil)
	req.Host = "sheets.example.com"
	if resp, err := http.DefaultClient.Do(req); err != nil || resp.StatusCode != http.StatusForbidden {
		t.Errorf("without hosts: %v %v", resp.StatusCode, err)
	}
}

// freePort finds a port nothing listens on right now.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// TestProjectFileEndpoints exports a project as a download and imports it
// back as an upload, both behind the API's guard.
func TestProjectFileEndpoints(t *testing.T) {
	ts, srv := setup(t)
	ctx := context.Background()
	p, err := srv.core.Lib.CreateProject(ctx, "Board", "a test board")
	if err != nil {
		t.Fatal(err)
	}
	v := 3.3
	if _, err := srv.core.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: "3V3", VNom: &v}); err != nil {
		t.Fatal(err)
	}
	if resp, _ := do(t, http.MethodGet, ts.URL+"/api/project.sdproj?project=Board", nil, false); resp.StatusCode != http.StatusForbidden {
		t.Errorf("export without the header: %d", resp.StatusCode)
	}
	resp, data := do(t, http.MethodGet, ts.URL+"/api/project.sdproj?project=Board", nil, true)
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "application/zip" || !strings.Contains(resp.Header.Get("Content-Disposition"), `filename="Board.sdproj"`) {
		t.Fatalf("export: %d %v", resp.StatusCode, resp.Header)
	}
	if resp, _ := do(t, http.MethodGet, ts.URL+"/api/project.sdproj?project=Nope", nil, true); resp.StatusCode != http.StatusNotFound {
		t.Errorf("a missing project: %d", resp.StatusCode)
	}
	if resp, _ := do(t, http.MethodPost, ts.URL+"/api/project-import?name=Copy", data, false); resp.StatusCode != http.StatusForbidden {
		t.Errorf("import without the header: %d", resp.StatusCode)
	}
	_, body := do(t, http.MethodPost, ts.URL+"/api/project-import?name=Copy&fetch=0", data, true)
	var out struct {
		Result core.ProjectImportResult `json:"result"`
		Error  string                   `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.Error != "" || out.Result.Project != "Copy" {
		t.Fatalf("import: %s", body)
	}
	cp, err := srv.core.Lib.Project(ctx, "Copy")
	if err != nil || cp.Description != "a test board" {
		t.Errorf("the imported project: %+v %v", cp, err)
	}
	if rails, _ := srv.core.Lib.Rails(ctx, cp.ID); len(rails) != 1 || rails[0].Name != "3V3" {
		t.Errorf("rails: %+v", rails)
	}
	_, body = do(t, http.MethodPost, ts.URL+"/api/project-import", []byte("not a zip"), true)
	if !strings.Contains(string(body), "not a project file") {
		t.Errorf("a bad file: %s", body)
	}
}
