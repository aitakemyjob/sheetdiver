package server

import (
	"bytes"
	"context"
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
