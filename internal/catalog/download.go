package catalog

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/flate"
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Progress reports a download: stage is downloading, extracting,
// indexing or done; Done and Total count bytes (or rows when indexing).
type Progress struct {
	Stage string `json:"stage"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

// maxParts bounds the split archive's parts.
const maxParts = 60

// client downloads the dataset.
var client = &http.Client{Timeout: 20 * time.Minute}

// part is one file of the split archive.
type part struct {
	name     string
	size     int64
	modified string
}

// Download fetches the latest dataset, builds the compact catalogue and
// swaps it in. The previous catalogue stays usable until the new one is
// ready. Only one download runs at a time.
func (c *Catalog) Download(ctx context.Context, progress func(Progress)) (err error) {
	if progress == nil {
		progress = func(Progress) {}
	}
	c.mu.Lock()
	if c.updating {
		c.mu.Unlock()
		return errors.New("the catalogue is already being downloaded")
	}
	c.updating = true
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.updating = false
		c.mu.Unlock()
	}()

	work := filepath.Join(c.dir, "download")
	if err := os.RemoveAll(work); err != nil {
		return err
	}
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	// The dataset is republished several times a day; a download that
	// straddles a republish gets parts of two archives, so it is retried.
	var parts []part
	for attempt := 0; ; attempt++ {
		parts, err = c.fetchParts(ctx, work, progress)
		if err == nil || !errors.Is(err, errMixed) || attempt == 2 {
			break
		}
	}
	if err != nil {
		return err
	}
	src := filepath.Join(work, fileName)
	if err := extract(work, parts, src, progress); err != nil {
		return err
	}
	published := publishedAt(parts)
	progress(Progress{Stage: "checking"})
	in, err := check(ctx, src)
	if err != nil {
		return err
	}
	in.Published, in.Downloaded = published.UTC(), time.Now().UTC()
	if err := c.swap(src, in); err != nil {
		return err
	}
	// Reported after the lock is released: callers read the status.
	progress(Progress{Stage: "done"})
	return nil
}

// swap installs a checked database as the catalogue.
func (c *Catalog) swap(src string, in info) error {
	b, _ := json.Marshal(in)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := retryBusy(func() error { return os.Rename(src, c.path()) }); err != nil {
		return err
	}
	return os.WriteFile(c.infoPath(), b, 0o644)
}

// errMixed means the archive parts come from different publications.
var errMixed = errors.New("the catalogue was republished during the download")

// publishWindow is how far apart the parts of one publication can be
// dated: they're written over a second or two, and the same file can
// report a slightly different date from different CDN edges or in its
// compressed form. Republishes are hours apart.
const publishWindow = 10 * time.Minute

// samePublication reports whether two Last-Modified dates belong to the
// same publication (exactly equal when either doesn't parse).
func samePublication(a, b string) bool {
	ta, errA := http.ParseTime(a)
	tb, errB := http.ParseTime(b)
	if errA != nil || errB != nil {
		return a == b
	}
	d := ta.Sub(tb)
	return d <= publishWindow && d >= -publishWindow
}

// publishedAt is when a publication was finished: its latest part's date.
func publishedAt(parts []part) time.Time {
	var latest time.Time
	for _, p := range parts {
		if t, err := http.ParseTime(p.modified); err == nil && t.After(latest) {
			latest = t
		}
	}
	return latest
}

// head asks for a file's size and modification time; ok is false for 404.
func (c *Catalog) head(ctx context.Context, name string) (part, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.Source+name, nil)
	if err != nil {
		return part{}, false, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return part{}, false, err
	}
	resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return part{}, false, nil
	case resp.StatusCode != http.StatusOK:
		return part{}, false, fmt.Errorf("%s: %s", name, resp.Status)
	}
	return part{name: name, size: resp.ContentLength, modified: resp.Header.Get("Last-Modified")}, true, nil
}

// fetchParts downloads every part of the split archive into dir.
func (c *Catalog) fetchParts(ctx context.Context, dir string, progress func(Progress)) ([]part, error) {
	last, ok, err := c.head(ctx, "cache.zip")
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("no catalogue published at %s", c.Source)
	}
	var parts []part
	for i := 1; i <= maxParts; i++ {
		p, ok, err := c.head(ctx, fmt.Sprintf("cache.z%02d", i))
		if err != nil {
			return nil, err
		}
		if !ok {
			break
		}
		parts = append(parts, p)
	}
	parts = append(parts, last)
	var total int64
	for _, p := range parts {
		if !samePublication(p.modified, last.modified) {
			return nil, errMixed
		}
		total += p.size
	}
	var done int64
	for _, p := range parts {
		n, err := c.get(ctx, p.name, filepath.Join(dir, p.name), func(n int64) {
			progress(Progress{Stage: "downloading", Done: done + n, Total: total})
		})
		if err != nil {
			return nil, err
		}
		if p.size > 0 && n != p.size {
			return nil, errMixed
		}
		done += n
	}
	// Republished while downloading: the last part is now from another
	// publication. (A mix that slips through fails the zip checksums.)
	again, ok, err := c.head(ctx, "cache.zip")
	if err != nil {
		return nil, err
	}
	if !ok || !samePublication(again.modified, last.modified) {
		return nil, errMixed
	}
	return parts, nil
}

// get downloads one file, reporting bytes as they arrive.
func (c *Catalog) get(ctx context.Context, name, to string, progress func(int64)) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Source+name, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download %s: %s", name, resp.Status)
	}
	f, err := os.Create(to)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var n int64
	buf := make([]byte, 256<<10)
	for {
		k, rerr := resp.Body.Read(buf)
		if k > 0 {
			if _, err := f.Write(buf[:k]); err != nil {
				return n, err
			}
			n += int64(k)
			progress(n)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return n, fmt.Errorf("download %s: %w", name, rerr)
		}
	}
	return n, f.Close()
}

// multiFile reads several files as one.
type multiFile struct {
	files []*os.File
	sizes []int64
	size  int64
}

// openMulti opens files in order.
func openMulti(paths []string) (*multiFile, error) {
	m := &multiFile{}
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			m.Close()
			return nil, err
		}
		st, err := f.Stat()
		if err != nil {
			f.Close()
			m.Close()
			return nil, err
		}
		m.files = append(m.files, f)
		m.sizes = append(m.sizes, st.Size())
		m.size += st.Size()
	}
	return m, nil
}

// ReadAt reads across file boundaries.
func (m *multiFile) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for i, f := range m.files {
		if off >= m.sizes[i] {
			off -= m.sizes[i]
			continue
		}
		k, err := f.ReadAt(p[n:min(len(p), n+int(m.sizes[i]-off))], off)
		n += k
		if err != nil && !errors.Is(err, io.EOF) {
			return n, err
		}
		off = 0
		if n == len(p) {
			return n, nil
		}
	}
	return n, io.EOF
}

// Close closes every file.
func (m *multiFile) Close() {
	for _, f := range m.files {
		f.Close()
	}
}

// extract inflates the archive's single file into dst. Split archives
// record offsets relative to their parts, which archive/zip doesn't
// follow, so the central directory only supplies the size and CRC and the
// data is streamed from the first local header (after the split marker).
func extract(dir string, parts []part, dst string, progress func(Progress)) error {
	var paths []string
	for _, p := range parts {
		paths = append(paths, filepath.Join(dir, p.name))
	}
	m, err := openMulti(paths)
	if err != nil {
		return err
	}
	defer m.Close()
	zr, err := zip.NewReader(m, m.size)
	if err != nil {
		return fmt.Errorf("read catalogue archive: %w", err)
	}
	if len(zr.File) != 1 || zr.File[0].Method != zip.Deflate {
		return fmt.Errorf("unexpected catalogue archive layout (%d files)", len(zr.File))
	}
	want := zr.File[0]
	if want.UncompressedSize64 > 1<<40 {
		return errors.New("the catalogue archive claims an impossible size")
	}
	head := make([]byte, 34)
	if _, err := m.ReadAt(head, 0); err != nil {
		return err
	}
	off := int64(0)
	if bytes.HasPrefix(head, []byte("PK\x07\x08")) {
		off = 4
	}
	h := make([]byte, 30)
	if _, err := m.ReadAt(h, off); err != nil {
		return err
	}
	if !bytes.HasPrefix(h, []byte("PK\x03\x04")) {
		return errors.New("catalogue archive: no local file header")
	}
	off += 30 + int64(binary.LittleEndian.Uint16(h[26:])) + int64(binary.LittleEndian.Uint16(h[28:]))
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	fr := flate.NewReader(bufio.NewReaderSize(io.NewSectionReader(m, off, m.size-off), 1<<20))
	defer fr.Close()
	crc := crc32.NewIEEE()
	w := bufio.NewWriterSize(io.MultiWriter(out, crc), 1<<20)
	total := int64(want.UncompressedSize64) //nolint:gosec // bounded above
	progress(Progress{Stage: "extracting", Total: total})
	var n int64
	buf := make([]byte, 1<<20)
	for {
		k, rerr := fr.Read(buf)
		if k > 0 {
			if _, err := w.Write(buf[:k]); err != nil {
				return err
			}
			n += int64(k)
			if n%(32<<20) < int64(k) {
				progress(Progress{Stage: "extracting", Done: n, Total: total})
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return fmt.Errorf("extract catalogue: %w", rerr)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if n != total || crc.Sum32() != want.CRC32 {
		return errors.New("the catalogue archive is damaged (size or checksum mismatch); try again")
	}
	return out.Close()
}

// sourceFormat is the jlcparts database format this code reads.
const sourceFormat = "source-db-v2"

// check confirms a downloaded file is a jlcparts database in the format
// this code reads, and counts its parts.
func check(ctx context.Context, path string) (info, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro&_pragma=query_only(1)")
	if err != nil {
		return info{}, err
	}
	defer db.Close()
	var format string
	if err := db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = 'format'`).Scan(&format); err != nil {
		return info{}, fmt.Errorf("the download isn't a jlcparts database: %w", err)
	}
	if format != sourceFormat {
		return info{}, fmt.Errorf("the jlcparts database changed format (%q, this SheetDiver reads %q); update SheetDiver", format, sourceFormat)
	}
	var in info
	err = db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(stock > 0), 0) FROM jlc_components WHERE present = 1`).Scan(&in.Parts, &in.InStock)
	return in, err
}
