package server

import (
	"bytes"
	"encoding/json"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/aitakemyjob/sheetdiver/internal/api"
	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
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
	_, img := do(t, http.MethodGet, ts.URL+"/api/page.png?sd=1&page=1&dpi=40&ds="+itoa(dsID), nil, false)
	if m, err := png.Decode(bytes.NewReader(img)); err != nil || m.Bounds().Dx() != 340 {
		t.Fatalf("page image: %v", err)
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
