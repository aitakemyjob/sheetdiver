// Package translate manages the offline translation models: Mozilla's
// Firefox Translations models (MPL-2.0), downloaded once when the user
// turns translation on and then used by the Bergamot engine, which runs
// inside SheetDiver's UI. Nothing being translated ever leaves the
// machine; the only network use is downloading a model, on request.
package translate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// File is one file of a translation model.
type File struct {
	// Part is the engine's name for it: model, vocab or lex.
	Part   string `json:"part"`
	Name   string `json:"name"`
	URL    string `json:"-"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Model is a language pair's model.
type Model struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label"`
	// Version is Mozilla's model version.
	Version string `json:"version"`
	Files   []File `json:"files"`
}

// pair names a model's language pair ("zhen").
func (m Model) pair() string { return m.From + m.To }

// size is the model's total download size.
func (m Model) size() int64 {
	var n int64
	for _, f := range m.Files {
		n += f.Size
	}
	return n
}

// mozilla is where Firefox's translation models are published.
const mozilla = "https://firefox-settings-attachments.cdn.mozilla.net/main-workspace/translations-models/"

// Models are the models SheetDiver can use, pinned to exact files and
// checksums (from Firefox's translations-models collection).
var Models = []Model{
	{From: "zh", To: "en", Label: "Chinese → English", Version: "2.1", Files: []File{
		{Part: "model", Name: "model.zhen.intgemm.alphas.bin", URL: mozilla + "715efbde-c974-4696-981a-83c0c5f82ad5.bin", SHA256: "5cd149601802fc8a18124a1c1306144dbbedc058630c4ddb2d53aa76fa9c7c06", Size: 43977787},
		{Part: "lex", Name: "lex.50.50.zhen.s2t.bin", URL: mozilla + "ae5f53d2-59ec-4aad-944d-999e50d6a65c.bin", SHA256: "8524dd4c93ffd7f0ae7be32f77a7c14fea7cdbbebe34ea6ad0ec368d5ddd8b13", Size: 9219192},
		{Part: "vocab", Name: "vocab.zhen.spm", URL: mozilla + "17ff5d42-6d4c-42f8-ad3c-b8042b5bafa7.spm", SHA256: "dff594318ab7d8b7b60b844ab98ebe6b932ae8045fab15235404c787715965b3", Size: 1359697},
	}},
}

// Find returns the model for a language pair.
func Find(from, to string) (Model, bool) {
	for _, m := range Models {
		if m.From == from && m.To == to {
			return m, true
		}
	}
	return Model{}, false
}

// Progress reports a download.
type Progress struct {
	Model string `json:"model"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// Manager keeps the downloaded models in a folder.
type Manager struct {
	dir string
	// client downloads models (replaced in tests).
	client *http.Client

	mu          sync.Mutex
	downloading map[string]bool
}

// NewManager manages the models in dir.
func NewManager(dir string) *Manager {
	return &Manager{dir: dir, client: &http.Client{Timeout: 30 * time.Minute}, downloading: map[string]bool{}}
}

// ModelStatus is a model with whether it's installed.
type ModelStatus struct {
	Model
	Installed   bool  `json:"installed"`
	Downloading bool  `json:"downloading"`
	SizeBytes   int64 `json:"sizeBytes"`
}

// Status lists the models and which are installed.
func (m *Manager) Status() []ModelStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]ModelStatus, 0, len(Models))
	for _, md := range Models {
		out = append(out, ModelStatus{Model: md, Installed: m.installed(md), Downloading: m.downloading[md.pair()], SizeBytes: md.size()})
	}
	return out
}

// installed reports whether every file of a model is present with the
// right size (checksums are checked when downloading).
func (m *Manager) installed(md Model) bool {
	for _, f := range md.Files {
		st, err := os.Stat(m.path(md, f))
		if err != nil || st.Size() != f.Size {
			return false
		}
	}
	return true
}

// path is where a model file is kept.
func (m *Manager) path(md Model, f File) string { return filepath.Join(m.dir, md.pair(), f.Name) }

// Download fetches a model's files, verifying each checksum, into place.
// A file already present and correct isn't downloaded again.
func (m *Manager) Download(ctx context.Context, from, to string, progress func(Progress)) error {
	md, ok := Find(from, to)
	if !ok {
		return fmt.Errorf("no %s→%s translation model", from, to)
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	m.mu.Lock()
	if m.downloading[md.pair()] {
		m.mu.Unlock()
		return errors.New("this model is already being downloaded")
	}
	m.downloading[md.pair()] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.downloading, md.pair())
		m.mu.Unlock()
	}()
	if err := os.MkdirAll(filepath.Join(m.dir, md.pair()), 0o755); err != nil {
		return err
	}
	total, done := md.size(), int64(0)
	for _, f := range md.Files {
		dst := m.path(md, f)
		if st, err := os.Stat(dst); err == nil && st.Size() == f.Size {
			done += f.Size
			continue
		}
		if err := m.fetch(ctx, f, dst, func(n int64) { progress(Progress{Model: md.Label, Done: done + n, Total: total}) }); err != nil {
			return err
		}
		done += f.Size
	}
	progress(Progress{Model: md.Label, Done: total, Total: total})
	return nil
}

// fetch downloads one file to a temporary name, checks its size and
// SHA-256, then moves it into place.
func (m *Manager) fetch(ctx context.Context, f File, dst string, progress func(int64)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, f.URL, nil)
	if err != nil {
		return err
	}
	resp, err := m.client.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", f.Name, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", f.Name, resp.Status)
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp) //nolint:errcheck // gone after the rename
	h := sha256.New()
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := out.Write(buf[:k]); err != nil {
				out.Close()
				return err
			}
			h.Write(buf[:k])
			n += int64(k)
			progress(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return fmt.Errorf("download %s: %w", f.Name, rerr)
		}
	}
	if err := out.Close(); err != nil {
		return err
	}
	if n != f.Size || hex.EncodeToString(h.Sum(nil)) != f.SHA256 {
		return fmt.Errorf("%s doesn't match its checksum; try again", f.Name)
	}
	return os.Rename(tmp, dst)
}

// Remove deletes a downloaded model.
func (m *Manager) Remove(from, to string) error {
	md, ok := Find(from, to)
	if !ok {
		return fmt.Errorf("no %s→%s translation model", from, to)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.downloading[md.pair()] {
		return errors.New("this model is being downloaded")
	}
	return os.RemoveAll(filepath.Join(m.dir, md.pair()))
}

// Registry is the engine's model registry for the installed models, with
// file URLs under base (where the server serves Open) followed by query.
func (m *Manager) Registry(base, query string) ([]byte, error) {
	reg := map[string]map[string]map[string]any{}
	for _, md := range Models {
		if !m.installed(md) {
			continue
		}
		files := map[string]map[string]any{}
		for _, f := range md.Files {
			files[f.Part] = map[string]any{"name": base + md.pair() + "/" + f.Name + query, "size": f.Size, "expectedSha256Hash": f.SHA256}
		}
		reg[md.pair()] = files
	}
	return json.Marshal(reg)
}

// Open opens an installed model file by its path under the registry base
// ("zhen/vocab.zhen.spm"). Only the pinned files can be opened.
func (m *Manager) Open(name string) (*os.File, error) {
	pair, file, ok := strings.Cut(name, "/")
	if !ok {
		return nil, os.ErrNotExist
	}
	for _, md := range Models {
		if md.pair() != pair {
			continue
		}
		for _, f := range md.Files {
			if f.Name == file {
				return os.Open(m.path(md, f))
			}
		}
	}
	return nil, os.ErrNotExist
}
