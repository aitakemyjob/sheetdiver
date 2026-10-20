package testutil

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/translate"
)

// StubTranslationModel replaces the pinned translation models with a
// small zh→en stand-in served by a test server, for the length of a test.
// It returns the files' contents by name.
func StubTranslationModel(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{"model.zhen.bin": []byte("model-bytes"), "lex.zhen.bin": []byte("lex"), "vocab.zhen.spm": []byte("vocab!")}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	var fs []translate.File
	for _, f := range [][2]string{{"model", "model.zhen.bin"}, {"lex", "lex.zhen.bin"}, {"vocab", "vocab.zhen.spm"}} {
		sum := sha256.Sum256(files[f[1]])
		fs = append(fs, translate.File{Part: f[0], Name: f[1], URL: srv.URL + "/" + f[1], SHA256: hex.EncodeToString(sum[:]), Size: int64(len(files[f[1]]))})
	}
	saved := translate.Models
	translate.Models = []translate.Model{{From: "zh", To: "en", Label: "Chinese → English", Version: "test", Files: fs}}
	t.Cleanup(func() { translate.Models = saved })
	return files
}
