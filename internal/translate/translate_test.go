package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stub replaces the pinned models with one served by a test server.
func stub(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	t.Cleanup(srv.Close)
	var fs []File
	for _, part := range []string{"model", "lex", "vocab"} {
		b := files[part]
		sum := sha256.Sum256(b)
		fs = append(fs, File{Part: part, Name: part + ".zhen.bin", URL: srv.URL + "/" + part, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))})
	}
	saved := Models
	Models = []Model{{From: "zh", To: "en", Label: "Chinese → English", Version: "t", Files: fs}}
	t.Cleanup(func() { Models = saved })
	return srv
}

func TestDownloadVerifyServeRemove(t *testing.T) {
	stub(t, map[string][]byte{"model": []byte("model-bytes"), "lex": []byte("lex"), "vocab": []byte("vocab!")})
	m := NewManager(t.TempDir())
	if st := m.Status(); len(st) != 1 || st[0].Installed || st[0].SizeBytes != 20 {
		t.Fatalf("status = %+v", st)
	}
	if reg, _ := m.Registry("/x/", ""); string(reg) != "{}" {
		t.Errorf("no models, empty registry: %s", reg)
	}
	var last Progress
	if err := m.Download(context.Background(), "zh", "en", func(p Progress) { last = p }); err != nil {
		t.Fatal(err)
	}
	if last.Done != 20 || last.Total != 20 || !m.Status()[0].Installed {
		t.Errorf("progress %+v, status %+v", last, m.Status())
	}
	var reg map[string]map[string]map[string]any
	b, _ := m.Registry("/api/translation/", "?sd=1")
	if err := json.Unmarshal(b, &reg); err != nil || reg["zhen"]["vocab"]["name"] != "/api/translation/zhen/vocab.zhen.bin?sd=1" {
		t.Errorf("registry = %s %v", b, err)
	}
	f, err := m.Open("zhen/vocab.zhen.bin")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(f)
	f.Close()
	if string(got) != "vocab!" {
		t.Errorf("served %q", got)
	}
	for _, bad := range []string{"zhen/../../etc/passwd", "zhen/other.bin", "enzh/vocab.zhen.bin", "vocab.zhen.bin"} {
		if f, err := m.Open(bad); err == nil {
			f.Close()
			t.Errorf("opened %q", bad)
		}
	}
	if err := m.Remove("zh", "en"); err != nil || m.Status()[0].Installed {
		t.Errorf("remove = %v", err)
	}
	if err := m.Download(context.Background(), "xx", "en", nil); err == nil {
		t.Error("unknown pairs are an error")
	}
}

func TestDownloadRejectsABadChecksum(t *testing.T) {
	files := map[string][]byte{"model": []byte("model-bytes"), "lex": []byte("lex"), "vocab": []byte("vocab!")}
	stub(t, files)
	files["lex"] = []byte("LEX") // served bytes no longer match the pinned checksum
	dir := t.TempDir()
	m := NewManager(dir)
	if err := m.Download(context.Background(), "zh", "en", nil); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v", err)
	}
	if m.Status()[0].Installed {
		t.Error("a bad download isn't installed")
	}
	if left, _ := filepath.Glob(filepath.Join(dir, "zhen", "*.part")); len(left) != 0 {
		t.Errorf("partial files left: %v", left)
	}
	if _, err := os.Stat(filepath.Join(dir, "zhen", "lex.zhen.bin")); !os.IsNotExist(err) {
		t.Error("the bad file isn't kept")
	}
}

func TestPinnedModels(t *testing.T) {
	md, ok := Find("zh", "en")
	if !ok || len(md.Files) != 3 || md.size() != 54556676 {
		t.Fatalf("zh-en = %+v", md)
	}
	for _, f := range md.Files {
		if len(f.SHA256) != 64 || !strings.HasPrefix(f.URL, "https://") {
			t.Errorf("%s isn't pinned: %+v", f.Name, f)
		}
	}
}
