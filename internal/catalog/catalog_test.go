package catalog

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// sourceDB writes the stand-in jlcparts database.
func sourceDB(t *testing.T, format string) []byte {
	t.Helper()
	b, err := testutil.JLCDatabase(t.TempDir(), format, testutil.JLCRows)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// splitZip splits a database like the published archive.
func splitZip(t *testing.T, data []byte, n int) map[string][]byte {
	t.Helper()
	files, err := testutil.SplitZip(data, n)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// publisher serves files with a Last-Modified per file.
type publisher struct {
	files    map[string][]byte
	modified func(name string) string
	gets     atomic.Int32
}

// ServeHTTP serves HEAD and GET like GitHub Pages.
func (p *publisher) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/data/")
	b, ok := p.files[name]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Last-Modified", p.modified(name))
	w.Header().Set("Content-Length", fmt.Sprint(len(b)))
	if r.Method == http.MethodGet {
		p.gets.Add(1)
		w.Write(b)
	}
}

// serve starts a publisher and a catalogue reading from it.
func serve(t *testing.T, p *publisher) *Catalog {
	t.Helper()
	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)
	c, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	c.Source = srv.URL + "/data/"
	return c
}

const published = "Sun, 18 Oct 2026 22:15:48 GMT"

func TestDownloadAndSearch(t *testing.T) {
	ctx := context.Background()
	p := &publisher{files: splitZip(t, sourceDB(t, sourceFormat), 600), modified: func(string) string { return published }}
	if len(p.files) < 3 {
		t.Fatalf("want a split archive, got %d parts", len(p.files))
	}
	c := serve(t, p)
	if st := c.Status(ctx); st.Installed {
		t.Fatal("nothing installed yet")
	}
	if _, err := c.Search(ctx, Query{Text: "x"}); err != ErrNotInstalled {
		t.Errorf("search before download = %v", err)
	}
	var stages []string
	if err := c.Download(ctx, func(pr Progress) {
		if len(stages) == 0 || stages[len(stages)-1] != pr.Stage {
			stages = append(stages, pr.Stage)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(stages, ","); got != "downloading,extracting,checking,done" {
		t.Errorf("stages = %s", got)
	}
	st := c.Status(ctx)
	if !st.Installed || st.Parts != 5 || st.InStock != 4 || st.Published.Format(time.RFC1123) != strings.Replace(published, "GMT", "UTC", 1) || st.SizeBytes == 0 {
		t.Errorf("status = %+v", st)
	}
	if _, err := os.Stat(filepath.Join(c.dir, "download")); !os.IsNotExist(err) {
		t.Error("the download folder is cleaned up")
	}

	items, err := c.Search(ctx, Query{Text: "ao3400a"})
	if err != nil || len(items) != 2 || items[0].LCSC != "C20917" || items[0].Library != "basic" || items[0].Price != 0.0895 {
		t.Fatalf("search = %+v %v", items, err)
	}
	if items[0].Attributes["Drain to Source Voltage"] != "30V" {
		t.Errorf("attributes = %v", items[0].Attributes)
	}
	if items, _ := c.Search(ctx, Query{Text: "buck 3A", InStock: true}); len(items) != 1 || items[0].MPN != "TPS563200DDCR" || items[0].Price != 1.0261 {
		t.Errorf("in-stock buck = %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "3A buck"}); len(items) != 2 || items[0].Stock == 0 {
		t.Errorf("in-stock parts first: %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "SOT-23", Basic: true}); len(items) != 1 || items[0].Manufacturer != "Alpha Omega Semicon" {
		t.Errorf("basic only = %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "17V 5.7A"}); len(items) != 0 {
		t.Errorf("values match at the start of a value: %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "4.5V 17V"}); len(items) != 1 {
		t.Errorf("ranges split on ~: %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "3400"}); len(items) != 2 {
		t.Errorf("part numbers match anywhere: %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "8A"}); len(items) != 0 {
		t.Errorf("5.8A is not 8A: %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "100%"}); len(items) != 0 {
		t.Errorf("wildcards are literal and absent parts hidden: %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Text: "10kΩ"}); len(items) != 1 || items[0].Datasheet != "" {
		t.Errorf("only http(s) datasheet links are kept: %+v", items)
	}
	cats, err := c.Categories(ctx)
	if err != nil || len(cats) != 3 {
		t.Errorf("categories = %+v %v", cats, err)
	}
	for _, x := range cats {
		if x.Subcategory == "MOSFETs" && (x.Parts != 2 || x.InStock != 2) {
			t.Errorf("MOSFETs = %+v", x)
		}
	}
	if items, _ := c.Search(ctx, Query{Subcategory: "MOSFETs", Sort: "price"}); len(items) != 2 || items[0].Price != 0.0627 {
		t.Errorf("browse by price = %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Subcategory: "MOSFETs", Sort: "price", Desc: true, Limit: 1, Offset: 1}); len(items) != 1 || items[0].Price != 0.0627 {
		t.Errorf("page 2 of price desc = %+v", items)
	}
	if items, _ := c.Search(ctx, Query{Sort: "mpn"}); len(items) != 5 || items[0].MPN != "0402WGF1002TCE" {
		t.Errorf("sort by mpn = %+v", items)
	}
	if items, _ := c.ByMPN(ctx, "tps563200ddcr"); len(items) != 1 || items[0].LCSC != "C97253" {
		t.Errorf("exact mpn = %+v", items)
	}
	if items, _ := c.ByMPN(ctx, "TPS563200"); len(items) != 2 || items[0].Stock == 0 {
		t.Errorf("mpn prefix, in stock first = %+v", items)
	}
	if items, _ := c.Get(ctx, []string{"C25744", "nope", "20917"}); len(items) != 2 || items[1].MPN != "AO3400A" {
		t.Errorf("get = %+v", items)
	}
	if subs, _ := c.Subcategories(ctx); subs["MOSFETs"] != 2 || TopSubcategories(subs, 1)[0] != "Chip Resistor - Surface Mount" && TopSubcategories(subs, 1)[0] != "MOSFETs" {
		t.Errorf("subcategories = %v", subs)
	}

	// A refresh replaces the catalogue in place.
	before := p.gets.Load()
	if err := c.Download(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if p.gets.Load()-before != int32(len(p.files)) {
		t.Errorf("refresh downloads every part once")
	}
	if err := c.Remove(); err != nil || c.Status(ctx).Installed {
		t.Errorf("remove = %v", err)
	}
}

func TestDownloadRejectsMixedAndDamagedArchives(t *testing.T) {
	ctx := context.Background()
	files := splitZip(t, sourceDB(t, sourceFormat), 600)

	// Parts from two publications: retried, then refused.
	mixed := &publisher{files: files, modified: func(n string) string {
		if n == "cache.zip" {
			return "Mon, 19 Oct 2026 03:00:00 GMT"
		}
		return published
	}}
	if err := serve(t, mixed).Download(ctx, nil); err == nil || !strings.Contains(err.Error(), "republished") {
		t.Errorf("mixed = %v", err)
	}

	// A flipped byte fails the checksum (or the inflate).
	bad := map[string][]byte{}
	for k, v := range files {
		bad[k] = bytes.Clone(v)
	}
	bad["cache.z01"][len(bad["cache.z01"])/2] ^= 0xff
	if err := serve(t, &publisher{files: bad, modified: func(string) string { return published }}).Download(ctx, nil); err == nil {
		t.Error("a damaged archive is refused")
	}

	// A new database format is reported, not misread.
	other := splitZip(t, sourceDB(t, "source-db-v3"), 1<<20)
	err := serve(t, &publisher{files: other, modified: func(string) string { return published }}).Download(ctx, nil)
	if err == nil || !strings.Contains(err.Error(), "changed format") {
		t.Errorf("format = %v", err)
	}

	// Nothing published.
	if err := serve(t, &publisher{files: map[string][]byte{}, modified: func(string) string { return published }}).Download(ctx, nil); err == nil {
		t.Error("an empty site is an error")
	}
}
