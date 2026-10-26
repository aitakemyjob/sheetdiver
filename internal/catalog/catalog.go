// Package catalog is an optional, local parts catalogue built from the
// open jlcparts dataset (the JLCPCB/LCSC assembly catalogue, republished
// several times a day at yaqwsx.github.io/jlcparts). The user downloads it
// on request and SheetDiver searches the jlcparts SQLite database as
// published, offline, reminding the user to refresh it when it gets old.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	_ "modernc.org/sqlite" // database/sql driver
)

// DefaultSource is where the jlcparts dataset is published.
const DefaultSource = "https://yaqwsx.github.io/jlcparts/data/"

// fileName is the downloaded jlcparts database in the catalogue folder.
const fileName = "jlc.sqlite3"

// ErrNotInstalled is returned when the catalogue hasn't been downloaded.
var ErrNotInstalled = errors.New("the JLCPCB parts catalogue isn't downloaded yet (Settings → Parts catalogue → Download)")

// Catalog is the local parts catalogue.
type Catalog struct {
	// Source is the base URL of the published dataset.
	Source string

	dir string

	// mu serializes a download or removal against queries: a query holds
	// it shared for its duration, so the file is never replaced under one.
	mu       sync.RWMutex
	updating bool
}

// Open opens the catalogue in dir (which needn't contain one yet). A
// catalogue already there is checked to open.
func Open(dir string) (*Catalog, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	c := &Catalog{Source: DefaultSource, dir: dir}
	if _, err := os.Stat(c.path()); err == nil {
		db, err := openDB(c.path())
		if err != nil {
			return nil, err
		}
		_ = db.Close()
	}
	return c, nil
}

// open opens the installed catalogue for one call; the caller closes it
// and holds c.mu (shared) meanwhile. Nothing keeps the database open
// between calls: every call sees the catalogue as it is now, including
// one the app downloaded, replaced or removed after this process started
// (the MCP server serving the library on its own said the catalogue
// wasn't downloaded until it was restarted, #70), and no process holds
// the file while another replaces it, which Windows refuses.
func (c *Catalog) open() (*sql.DB, error) {
	if _, err := os.Stat(c.path()); err != nil {
		return nil, ErrNotInstalled
	}
	return openDB(c.path())
}

// path is the catalogue database file.
func (c *Catalog) path() string { return filepath.Join(c.dir, fileName) }

// openDB opens a catalogue database read-mostly.
func openDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=query_only(1)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Close releases the catalogue. Nothing is held between calls, so it
// only waits for a query in progress.
func (c *Catalog) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil
}

// Status describes the installed catalogue.
type Status struct {
	Installed bool `json:"installed"`
	Updating  bool `json:"updating"`
	// Parts is how many parts the catalogue lists; InStock how many
	// JLCPCB has in stock.
	Parts   int `json:"parts"`
	InStock int `json:"inStock"`
	// Published is when jlcparts published the data; Downloaded when it
	// was fetched.
	Published  time.Time `json:"published"`
	Downloaded time.Time `json:"downloaded"`
	SizeBytes  int64     `json:"sizeBytes"`
	Source     string    `json:"source"`
}

// info is the sidecar file recording a download.
type info struct {
	Parts      int       `json:"parts"`
	InStock    int       `json:"inStock"`
	Published  time.Time `json:"published"`
	Downloaded time.Time `json:"downloaded"`
}

// infoPath is the sidecar file.
func (c *Catalog) infoPath() string { return filepath.Join(c.dir, "jlc.json") }

// Status reports what is installed.
func (c *Catalog) Status(ctx context.Context) Status {
	c.mu.RLock()
	defer c.mu.RUnlock()
	st := Status{Updating: c.updating, Source: c.Source}
	fi, err := os.Stat(c.path())
	if err != nil {
		return st
	}
	st.Installed, st.SizeBytes = true, fi.Size()
	var in info
	if b, err := os.ReadFile(c.infoPath()); err == nil {
		_ = json.Unmarshal(b, &in)
	}
	st.Parts, st.InStock, st.Published, st.Downloaded = in.Parts, in.InStock, in.Published, in.Downloaded
	return st
}

// Item is one catalogue part.
type Item struct {
	// LCSC is the LCSC/JLCPCB part code, such as C20917.
	LCSC         string `json:"lcsc"`
	MPN          string `json:"mpn"`
	Manufacturer string `json:"manufacturer"`
	Package      string `json:"package"`
	Category     string `json:"category"`
	Subcategory  string `json:"subcategory"`
	Description  string `json:"description"`
	Datasheet    string `json:"datasheet"`
	Stock        int    `json:"stock"`
	// Library is JLCPCB's assembly library: basic (no setup fee),
	// preferred or extended.
	Library string `json:"library"`
	// Price is the single-unit price in USD (0 when unknown).
	Price      float64           `json:"price"`
	Attributes map[string]string `json:"attributes,omitempty"`
}

// Query is a catalogue search.
type Query struct {
	// Text matches part numbers, manufacturers, packages, categories and
	// descriptions (which carry the main specs: "3A 4.5V~17V Buck").
	Text string `json:"text"`
	// InStock keeps parts JLCPCB has in stock; Basic keeps its basic and
	// preferred libraries (no extended-part fee).
	InStock     bool   `json:"inStock,omitempty"`
	Basic       bool   `json:"basic,omitempty"`
	Subcategory string `json:"subcategory,omitempty"`
	// Attrs filter on the parts' attributes (see Params for which a
	// category has); they need a Subcategory or search words.
	Attrs []AttrFilter `json:"attrs,omitempty"`
	// Sort is stock (default), price, mpn or attr:<key>; Desc reverses it.
	Sort   string `json:"sort,omitempty"`
	Desc   bool   `json:"desc,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// itemCols are the jlcparts columns scanItem reads.
const itemCols = `lcsc, mfr, manufacturer, package, category, subcategory, description, datasheet, stock, library_type, preferred, price, attributes`

// scanItem reads one jlcparts row.
func scanItem(sc interface{ Scan(...any) error }) (Item, error) {
	var it Item
	var lcsc, preferred int64
	var lib, price, attrs string
	err := sc.Scan(&lcsc, &it.MPN, &it.Manufacturer, &it.Package, &it.Category, &it.Subcategory, &it.Description, &it.Datasheet, &it.Stock, &lib, &preferred, &price, &attrs)
	it.LCSC = fmt.Sprintf("C%d", lcsc)
	it.Library = libraryName(lib, preferred)
	it.Price = unitPrice(price)
	it.Datasheet = safeURL(it.Datasheet)
	it.Attributes = parseAttrs(attrs)
	return it, err
}

// libraryName names JLCPCB's assembly library for a part.
func libraryName(t string, preferred int64) string {
	switch {
	case t == "base":
		return "basic"
	case preferred != 0:
		return "preferred"
	}
	return "extended"
}

// unitPrice is the first price break of "1-9:1.0261,10-29:0.9097,…".
func unitPrice(s string) float64 {
	first, _, _ := strings.Cut(s, ",")
	_, p, ok := strings.Cut(first, ":")
	if !ok {
		return 0
	}
	v, _ := strconv.ParseFloat(strings.TrimSpace(p), 64)
	return v
}

// parseAttrs reads the attribute object, keeping string values.
func parseAttrs(s string) map[string]string {
	var m map[string]any
	if json.Unmarshal([]byte(s), &m) != nil || len(m) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range m {
		if str, ok := v.(string); ok && str != "" && str != "-" {
			out[k] = str
		}
	}
	return out
}

// safeURL keeps only http(s) datasheet links.
func safeURL(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "http://") {
		return s
	}
	return ""
}

// quantityRe matches a value with a unit or multiplier: 3A, 4.5V, 10k,
// 100nF, 650kHz, 1%.
var quantityRe = regexp.MustCompile(`^\d+(?:\.\d+)?\s*(?:[a-zA-ZµΩ%]{1,4})$`)

// words splits search text into terms.
func words(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == '"'
	})
}

// likeEscape escapes LIKE wildcards (with \ as the escape character).
func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

// Search finds parts whose part number, manufacturer, package, category
// or description contain every word, in-stock basic parts first. It reads
// the jlcparts database directly (a scan: a few hundred milliseconds).
func (c *Catalog) Search(ctx context.Context, q Query) ([]Item, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	db, err := c.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	limit := q.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	if err := checkFilters(q.Attrs); err != nil {
		return nil, err
	}
	// Package filters are plain SQL; other attributes are checked in Go.
	if attrSort(q.Sort) != "" || slices.ContainsFunc(q.Attrs, func(f AttrFilter) bool { return f.Key != PackageKey }) {
		return c.searchAttrs(ctx, db, q, limit)
	}
	where, args := q.where()
	args = append(args, limit, max(0, q.Offset))
	rows, err := db.QueryContext(ctx, `SELECT `+itemCols+` FROM jlc_components WHERE `+where+ //nolint:gosec // constant SQL fragments; values are placeholders
		` ORDER BY `+q.order()+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// where builds the SQL condition of a query's words and flags.
func (q Query) where() (string, []any) {
	where := []string{"present = 1"}
	var args []any
	for _, w := range words(q.Text) {
		if quantityRe.MatchString(w) {
			// A value ("3A", "17V", "10k") matches at the start of a value,
			// so 3A doesn't find 1.3A; ranges and conditions split on ~ and @.
			where = append(where, `(' ' || REPLACE(REPLACE(description, '~', ' '), '@', ' ')) LIKE ? ESCAPE '\'`)
			args = append(args, "% "+likeEscape(w)+"%")
			continue
		}
		where = append(where, `(mfr || ' ' || manufacturer || ' ' || package || ' ' || subcategory || ' ' || description) LIKE ? ESCAPE '\'`)
		args = append(args, "%"+likeEscape(w)+"%")
	}
	if q.InStock {
		where = append(where, "stock > 0")
	}
	if q.Basic {
		where = append(where, "(library_type = 'base' OR preferred <> 0)")
	}
	if q.Subcategory != "" {
		where = append(where, "subcategory = ?")
		args = append(args, q.Subcategory)
	}
	for _, f := range q.Attrs {
		if f.Key != PackageKey || f.Op != "=" {
			continue
		}
		vals := f.wanted()
		if len(vals) == 0 {
			continue
		}
		where = append(where, "package COLLATE NOCASE IN ("+strings.TrimSuffix(strings.Repeat("?,", len(vals)), ",")+")")
		for _, v := range vals {
			args = append(args, v)
		}
	}
	return strings.Join(where, " AND "), args
}

// order is a query's SQL ordering (attribute sorts are applied later).
func (q Query) order() string {
	dir := map[bool]string{false: "ASC", true: "DESC"}
	switch q.Sort {
	case "price":
		// Unpriced parts last; the first price break as a number.
		return `price = '', CAST(SUBSTR(price, INSTR(price, ':') + 1) AS REAL) ` + dir[q.Desc] + `, stock DESC`
	case "mpn":
		return `mfr ` + dir[q.Desc]
	case "stock":
		return `stock ` + dir[!q.Desc]
	}
	return `stock > 0 DESC, library_type <> 'base' AND preferred = 0, stock DESC`
}

// ByMPN finds parts by manufacturer part number: exact (ignoring case)
// first, else ones that start with it (orderable codes with a suffix).
// In-stock parts come first.
func (c *Catalog) ByMPN(ctx context.Context, mpn string) ([]Item, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	db, err := c.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	key := strings.ToUpper(strings.TrimSpace(mpn))
	if len(key) < 3 {
		return nil, nil
	}
	query := func(cond string, arg string) ([]Item, error) {
		rows, err := db.QueryContext(ctx, `SELECT `+itemCols+` FROM jlc_components WHERE present = 1 AND `+cond+` ORDER BY stock > 0 DESC, datasheet = '', stock DESC LIMIT 20`, arg) //nolint:gosec // constant SQL fragments; values are placeholders
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []Item
		for rows.Next() {
			it, err := scanItem(rows)
			if err != nil {
				return nil, err
			}
			out = append(out, it)
		}
		return out, rows.Err()
	}
	out, err := query(`UPPER(mfr) = ?`, key)
	if err != nil || len(out) > 0 {
		return out, err
	}
	return query(`mfr LIKE ? ESCAPE '\'`, likeEscape(key)+"%")
}

// Get returns parts by LCSC code ("C20917" or "20917"), in the given
// order; unknown codes are skipped.
func (c *Catalog) Get(ctx context.Context, codes []string) ([]Item, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	db, err := c.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var out []Item
	for _, code := range codes {
		n, err := strconv.ParseInt(strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(code)), "C"), 10, 64)
		if err != nil {
			continue
		}
		it, err := scanItem(db.QueryRowContext(ctx, `SELECT `+itemCols+` FROM jlc_components WHERE lcsc = ?`, n))
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, nil
}

// Category is a JLCPCB category and subcategory with its part counts.
type Category struct {
	Category    string `json:"category"`
	Subcategory string `json:"subcategory"`
	Parts       int    `json:"parts"`
	InStock     int    `json:"inStock"`
}

// Categories lists every subcategory with its parts and in-stock counts,
// grouped by category (most in-stock parts first). It scans the
// catalogue once (about a second); callers cache it.
func (c *Catalog) Categories(ctx context.Context) ([]Category, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	db, err := c.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT category, subcategory, COUNT(*), SUM(stock > 0) FROM jlc_components
		WHERE present = 1 AND subcategory <> '' GROUP BY category, subcategory`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	total := map[string]int{}
	for rows.Next() {
		var x Category
		if err := rows.Scan(&x.Category, &x.Subcategory, &x.Parts, &x.InStock); err != nil {
			return nil, err
		}
		out = append(out, x)
		total[x.Category] += x.InStock
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Category != b.Category {
			if total[a.Category] != total[b.Category] {
				return total[a.Category] > total[b.Category]
			}
			return a.Category < b.Category
		}
		if a.InStock != b.InStock {
			return a.InStock > b.InStock
		}
		return a.Subcategory < b.Subcategory
	})
	return out, rows.Err()
}

// Subcategories lists the categories of in-stock parts with their counts,
// largest first.
func (c *Catalog) Subcategories(ctx context.Context) (map[string]int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	db, err := c.open()
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT subcategory, COUNT(*) FROM jlc_components WHERE present = 1 AND stock > 0 AND subcategory <> '' GROUP BY subcategory`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var s string
		var n int
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// TopSubcategories returns the n largest categories.
func TopSubcategories(m map[string]int, n int) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if m[out[i]] != m[out[j]] {
			return m[out[i]] > m[out[j]]
		}
		return out[i] < out[j]
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Remove deletes the downloaded catalogue.
func (c *Catalog) Remove() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.updating {
		return errors.New("the catalogue is being downloaded")
	}
	_ = os.Remove(c.infoPath())
	return retryBusy(func() error {
		err := os.Remove(c.path())
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	})
}

// retryBusy runs a rename or removal of the catalogue file, retrying for
// a few seconds on Windows, which refuses while another process has the
// file open: queries open it only for their own duration.
func retryBusy(op func() error) error {
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := op()
		if err == nil || runtime.GOOS != "windows" || time.Now().After(deadline) {
			return err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lcscCodeRe matches an LCSC/JLCPCB part code.
var lcscCodeRe = regexp.MustCompile(`^C\d{1,10}$`)

// Shops are the stores a catalogue part can be opened at.
const (
	ShopLCSC   = "lcsc"
	ShopJLCPCB = "jlcpcb"
)

// ShopURL is a part's page at LCSC (buy) or JLCPCB (assembly), from its
// LCSC code ("C25744").
func ShopURL(code, shop string) (string, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if !lcscCodeRe.MatchString(code) {
		return "", fmt.Errorf("%q is not an LCSC part code (like C25744)", code)
	}
	switch shop {
	case ShopLCSC, "":
		return "https://www.lcsc.com/product-detail/" + code + ".html", nil
	case ShopJLCPCB:
		return "https://jlcpcb.com/partdetail/" + code, nil
	}
	return "", fmt.Errorf("unknown shop %q (lcsc or jlcpcb)", shop)
}
