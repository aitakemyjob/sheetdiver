package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// partCols is the column list scanPart reads.
const partCols = `id, mpn, manufacturer, description, category, package, COALESCE(datasheet_id, 0), tags, favorite, status, created_at, updated_at`

// scanPart reads a part row.
func scanPart(sc interface{ Scan(...any) error }) (Part, error) {
	var p Part
	var tags, c, u string
	err := sc.Scan(&p.ID, &p.MPN, &p.Manufacturer, &p.Description, &p.Category, &p.Package, &p.DatasheetID, &tags, &p.Favorite, &p.Status, &c, &u)
	if err != nil {
		return p, err
	}
	_ = json.Unmarshal([]byte(tags), &p.Tags)
	if p.Tags == nil {
		p.Tags = []string{}
	}
	p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
	return p, nil
}

// PartByID returns a part.
func (s *Store) PartByID(ctx context.Context, id int64) (Part, error) {
	p, err := scanPart(s.db.QueryRowContext(ctx, `SELECT `+partCols+` FROM parts WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("part %d: %w", id, ErrNotFound)
	}
	return p, err
}

// Resolution says how a reference matched a part.
type Resolution struct {
	Part Part `json:"part"`
	// Via is "mpn", "id", "variant" (an orderable code), "family" (a
	// family pattern such as STM32U545xx) or "prefix" (an unlisted
	// ordering code that starts with the part number).
	Via     string `json:"via"`
	Variant string `json:"variant,omitempty"`
}

// Resolve finds the part a reference names: an ID ("12" or "#12"), a
// base MPN, a known orderable code, a part matching a datasheet's family
// pattern, or an unlisted ordering code that extends a known part number
// (STM32U545VET6Q → STM32U545VE). Matching ignores case.
func (s *Store) Resolve(ctx context.Context, ref string) (Resolution, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return Resolution{}, errors.New("no part given")
	}
	if id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64); err == nil {
		if p, err := s.PartByID(ctx, id); err == nil {
			return Resolution{Part: p, Via: "id"}, nil
		}
	}
	p, err := scanPart(s.db.QueryRowContext(ctx, `SELECT `+partCols+` FROM parts WHERE mpn = ?`, ref))
	if err == nil {
		return Resolution{Part: p, Via: "mpn"}, nil
	}
	var pid int64
	var vmpn string
	if err := s.db.QueryRowContext(ctx, `SELECT part_id, mpn FROM variants WHERE mpn = ?`, ref).Scan(&pid, &vmpn); err == nil {
		if p, err := s.PartByID(ctx, pid); err == nil {
			return Resolution{Part: p, Via: "variant", Variant: vmpn}, nil
		}
	}
	// Longest part number that the reference extends.
	up := strings.ToUpper(ref)
	rows, err := s.db.QueryContext(ctx, `SELECT `+partCols+` FROM parts WHERE ? LIKE mpn || '%' ORDER BY length(mpn) DESC LIMIT 1`, up)
	if err == nil {
		defer rows.Close()
		if rows.Next() {
			if p, err := scanPart(rows); err == nil && len(p.MPN) >= 4 {
				return Resolution{Part: p, Via: "prefix", Variant: ref}, nil
			}
		}
		rows.Close()
	}
	// One of the numbers a part was stored under, joined by a slash, by an
	// import before they were split ("MAX17048/MAX17049").
	if r, ok := s.resolveJoined(ctx, ref); ok {
		return r, nil
	}
	// A family pattern: the part from that datasheet whose number the
	// reference matches best.
	if r, ok := s.resolveFamily(ctx, ref); ok {
		return r, nil
	}
	// An orderable number: what follows a + or # (lead-free, RoHS, tape
	// and reel: MAX17048G+T10) says nothing about the part.
	if base := orderingBase(ref); base != ref {
		if r, err := s.Resolve(ctx, base); err == nil {
			if r.Variant == "" || r.Variant == base {
				r.Variant = ref
			}
			return r, nil
		}
	}
	return Resolution{}, s.notFound(ctx, ref)
}

// orderingBase is a part number without its ordering suffix: everything
// from the first + or # (MAX17048G+T10 → MAX17048G).
func orderingBase(ref string) string {
	if i := strings.IndexAny(ref, "+#"); i > 0 {
		return ref[:i]
	}
	return ref
}

// resolveJoined matches ref against parts stored under several numbers
// joined by slashes, the longest that ref starts with.
func (s *Store) resolveJoined(ctx context.Context, ref string) (Resolution, bool) {
	up := strings.ToUpper(ref)
	rows, err := s.db.QueryContext(ctx, `SELECT `+partCols+` FROM parts WHERE mpn LIKE '%/%'`)
	if err != nil {
		return Resolution{}, false
	}
	defer rows.Close()
	var best Resolution
	n := 0
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			continue
		}
		for _, alt := range joined(p.MPN) {
			if len(alt) >= 4 && len(alt) > n && strings.HasPrefix(up, strings.ToUpper(alt)) {
				best, n = Resolution{Part: p, Via: "prefix", Variant: ref}, len(alt)
			}
		}
	}
	return best, n > 0
}

// joined splits numbers joined by slashes, completing a short second one
// from the first ("MAX17048/9" is MAX17048 and MAX17049).
func joined(mpn string) []string {
	pieces := strings.Split(mpn, "/")
	out := []string{pieces[0]}
	for _, p := range pieces[1:] {
		if p == "" {
			continue
		}
		if first := pieces[0]; len(p) < len(first) && strings.Trim(p, "0123456789") == "" && strings.Trim(first[len(first)-len(p):], "0123456789") == "" {
			p = first[:len(first)-len(p)] + p
		}
		out = append(out, p)
	}
	return out
}

// resolveFamily matches ref against datasheet family patterns.
func (s *Store) resolveFamily(ctx context.Context, ref string) (Resolution, bool) {
	ds, err := s.Datasheets(ctx)
	if err != nil {
		return Resolution{}, false
	}
	for _, d := range ds {
		for _, f := range d.Families {
			if !familyMatch(f, ref) && !strings.EqualFold(f, ref) {
				continue
			}
			parts, err := s.PartsOfDatasheet(ctx, d.ID)
			if err != nil || len(parts) == 0 {
				continue
			}
			best := parts[0]
			for _, p := range parts {
				if strings.HasPrefix(strings.ToUpper(ref), strings.ToUpper(p.MPN)) {
					best = p
				}
			}
			return Resolution{Part: best, Via: "family", Variant: f}, true
		}
	}
	return Resolution{}, false
}

// familyMatch reports whether ref fits a family pattern, where each x is
// any letter or digit and anything may follow (ordering suffixes).
func familyMatch(family, ref string) bool {
	var b strings.Builder
	b.WriteString("^(?i:")
	for _, r := range family {
		if r == 'x' {
			b.WriteString("[A-Z0-9]")
		} else {
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(")")
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(ref)
}

// notFound builds a helpful error listing similar part numbers: those
// sharing the longest possible prefix with the reference.
func (s *Store) notFound(ctx context.Context, ref string) error {
	up := strings.ToUpper(ref)
	var near []string
	for n := len(up); n >= 3 && len(near) == 0; n-- {
		like := up[:n] + "%"
		rows, err := s.db.QueryContext(ctx, `SELECT mpn FROM parts WHERE upper(mpn) LIKE ? UNION SELECT mpn FROM variants WHERE upper(mpn) LIKE ? LIMIT 8`, like, like)
		if err != nil {
			break
		}
		for rows.Next() {
			var m string
			if rows.Scan(&m) == nil {
				near = append(near, m)
			}
		}
		rows.Close()
	}
	if len(near) > 0 {
		sort.Strings(near)
		return fmt.Errorf("part %q: %w; similar: %s", ref, ErrNotFound, strings.Join(near, ", "))
	}
	return fmt.Errorf("part %q: %w (import its datasheet first)", ref, ErrNotFound)
}

// PartFilter narrows Parts.
type PartFilter struct {
	Query        string `json:"query,omitempty"`
	Category     string `json:"category,omitempty"`
	Manufacturer string `json:"manufacturer,omitempty"`
	// Favorite keeps only starred parts; Status only parts with that status.
	Favorite bool   `json:"favorite,omitempty"`
	Status   string `json:"status,omitempty"`
	Limit    int    `json:"limit,omitempty"`
}

// Parts lists parts, filtered by MPN/variant/description substring,
// category and manufacturer, ordered by MPN.
func (s *Store) Parts(ctx context.Context, f PartFilter) ([]Part, error) {
	q := `SELECT ` + partCols + ` FROM parts WHERE 1=1`
	var args []any
	if f.Query != "" {
		like := "%" + strings.ToUpper(f.Query) + "%"
		q += ` AND (upper(mpn) LIKE ? OR upper(description) LIKE ? OR id IN (SELECT part_id FROM variants WHERE upper(mpn) LIKE ?))`
		args = append(args, like, like, like)
	}
	if f.Category != "" {
		q += ` AND category = ?`
		args = append(args, f.Category)
	}
	if f.Manufacturer != "" {
		q += ` AND manufacturer LIKE ?`
		args = append(args, "%"+f.Manufacturer+"%")
	}
	if f.Favorite {
		q += ` AND favorite = 1`
	}
	if f.Status != "" {
		q += ` AND status = ?`
		args = append(args, f.Status)
	}
	q += ` ORDER BY favorite DESC, mpn`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Part{}
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PartsOfDatasheet lists the parts a datasheet covers.
func (s *Store) PartsOfDatasheet(ctx context.Context, dsID int64) ([]Part, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+partCols+` FROM parts WHERE datasheet_id = ? ORDER BY mpn`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Part{}
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PartUpdate is a partial update; nil fields are left alone.
type PartUpdate struct {
	MPN          *string   `json:"mpn,omitempty"`
	Manufacturer *string   `json:"manufacturer,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Category     *string   `json:"category,omitempty"`
	Package      *string   `json:"package,omitempty"`
	Tags         *[]string `json:"tags,omitempty"`
	Favorite     *bool     `json:"favorite,omitempty"`
	Status       *string   `json:"status,omitempty"`
}

// UpdatePart applies a partial update to a part.
func (s *Store) UpdatePart(ctx context.Context, id int64, u PartUpdate) (Part, error) {
	p, err := s.PartByID(ctx, id)
	if err != nil {
		return p, err
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	set(&p.MPN, u.MPN)
	set(&p.Manufacturer, u.Manufacturer)
	set(&p.Description, u.Description)
	set(&p.Category, u.Category)
	set(&p.Package, u.Package)
	if u.Tags != nil {
		p.Tags = *u.Tags
	}
	if u.Favorite != nil {
		p.Favorite = *u.Favorite
	}
	if u.Status != nil {
		st := strings.ToLower(strings.TrimSpace(*u.Status))
		if _, ok := Statuses[st]; !ok && st != "" {
			return p, fmt.Errorf("status must be one of preferred, approved, avoid, nrnd, eol (or empty), got %q", *u.Status)
		}
		p.Status = st
	}
	if p.MPN == "" {
		return p, errors.New("a part needs a part number")
	}
	tags, _ := json.Marshal(nonNil(p.Tags))
	_, err = s.db.ExecContext(ctx, `UPDATE parts SET mpn = ?, manufacturer = ?, description = ?, category = ?, package = ?, tags = ?, favorite = ?, status = ?, updated_at = ? WHERE id = ?`,
		p.MPN, p.Manufacturer, p.Description, p.Category, p.Package, string(tags), p.Favorite, p.Status, now(), id)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return p, fmt.Errorf("another part is already %s", p.MPN)
		}
		return p, err
	}
	if p.DatasheetID != 0 {
		err = s.Reindex(ctx, p.DatasheetID)
	}
	return p, err
}

// AddPart creates a part, optionally on a datasheet (a datasheet that
// covers more parts than the extractor found).
func (s *Store) AddPart(ctx context.Context, p Part) (Part, error) {
	p.MPN = strings.TrimSpace(p.MPN)
	if p.MPN == "" {
		return p, errors.New("a part needs a part number")
	}
	if r, err := s.Resolve(ctx, p.MPN); err == nil && strings.EqualFold(r.Part.MPN, p.MPN) {
		return r.Part, nil
	}
	var ds any
	if p.DatasheetID != 0 {
		ds = p.DatasheetID
	}
	tags, _ := json.Marshal(nonNil(p.Tags))
	res, err := s.db.ExecContext(ctx, `INSERT INTO parts (mpn, manufacturer, description, category, package, datasheet_id, tags, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		p.MPN, p.Manufacturer, p.Description, p.Category, p.Package, ds, string(tags), now(), now())
	if err != nil {
		return p, err
	}
	id, _ := res.LastInsertId()
	if p.DatasheetID != 0 {
		if err := s.Reindex(ctx, p.DatasheetID); err != nil {
			return p, err
		}
	}
	return s.PartByID(ctx, id)
}

// DeletePart removes a part (and its variants and project entries).
func (s *Store) DeletePart(ctx context.Context, id int64) error {
	p, err := s.PartByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM parts WHERE id = ?`, id); err != nil {
		return err
	}
	if p.DatasheetID != 0 {
		return s.Reindex(ctx, p.DatasheetID)
	}
	return nil
}

// Variants lists a part's orderable codes.
func (s *Store) Variants(ctx context.Context, partID int64) ([]Variant, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mpn, part_id, package, notes, source FROM variants WHERE part_id = ? ORDER BY mpn`, partID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Variant{}
	for rows.Next() {
		var v Variant
		if err := rows.Scan(&v.MPN, &v.PartID, &v.Package, &v.Notes, &v.Source); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// SetVariants adds or updates orderable codes for a part.
func (s *Store) SetVariants(ctx context.Context, partID int64, vs []Variant, source string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, v := range vs {
			v.MPN = strings.TrimSpace(v.MPN)
			if v.MPN == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO variants (mpn, part_id, package, notes, source) VALUES (?,?,?,?,?)
				ON CONFLICT(mpn) DO UPDATE SET part_id = excluded.part_id, package = excluded.package, notes = excluded.notes, source = excluded.source`,
				v.MPN, partID, v.Package, v.Notes, source); err != nil {
				return err
			}
		}
		return nil
	})
}

// Categories lists the categories in use with their part counts.
func (s *Store) Categories(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT category, count(*) FROM parts GROUP BY category`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var c string
		var n int
		if err := rows.Scan(&c, &n); err != nil {
			return nil, err
		}
		out[c] = n
	}
	return out, rows.Err()
}

// appliesTo reports whether a row limited to the given MPNs applies to a
// part (an empty list applies to every part on the datasheet).
func appliesTo(list []string, mpn string) bool {
	if len(list) == 0 || mpn == "" {
		return true
	}
	for _, m := range list {
		if strings.EqualFold(m, mpn) {
			return true
		}
	}
	return false
}
