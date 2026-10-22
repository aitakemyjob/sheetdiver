package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Net kinds.
const (
	NetSignal = "signal"
	NetPower  = "power"
	NetGround = "ground"
)

// Net is a named connection in a project's connection table. Kind is
// signal, power or ground; Bus groups nets (SDA and SCL in I2C1) on the
// block diagram; Voltage is a signal net's I/O level in volts.
type Net struct {
	ID        int64    `json:"id"`
	ProjectID int64    `json:"projectId"`
	Name      string   `json:"name"`
	Kind      string   `json:"kind"`
	Bus       string   `json:"bus,omitempty"`
	Voltage   *float64 `json:"voltage,omitempty"`
	Notes     string   `json:"notes,omitempty"`
	Source    string   `json:"source"`
}

// Block is something off the BOM that the board connects to (a battery,
// a USB-C port, a sensor still to be chosen), or a passive not worth a
// datasheet ("R4", 36.5k). Kind is one of BlockKinds; Value is what's
// printed with it ("10µF", "2.2µH").
type Block struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	Name      string `json:"name"`
	Kind      string `json:"kind,omitempty"`
	Value     string `json:"value,omitempty"`
	Notes     string `json:"notes,omitempty"`
	Source    string `json:"source"`
}

// BlockKinds are the kinds a block can be ("" is a generic block).
var BlockKinds = []string{"ic", "connector", "module", "resistor", "capacitor", "inductor", "diode"}

// TwoTerminal reports whether a block kind or part category is drawn as
// a passive on the block diagram, hanging off a net rather than as a box
// with pins.
func TwoTerminal(kind string) bool {
	switch kind {
	case "resistor", "capacitor", "inductor", "diode", "tvs", "ferrite", "fuse":
		return true
	}
	return false
}

// Connection puts a pin of a BOM entry (EntryID), or a block (BlockID),
// on a net. Pin is the pin number when the pin table has it, else as
// given; a block's connections usually have none. Ref is which of an
// entry's designators it is, when the entry has several ("R1" of
// "R1,R2"); empty for the entry as a whole.
type Connection struct {
	ID      int64  `json:"id"`
	NetID   int64  `json:"netId"`
	Net     string `json:"net"`
	EntryID int64  `json:"entryId,omitempty"`
	BlockID int64  `json:"blockId,omitempty"`
	Ref     string `json:"ref,omitempty"`
	Pin     string `json:"pin,omitempty"`
	Notes   string `json:"notes,omitempty"`
	Source  string `json:"source"`
}

// Position is where a block-diagram node was placed: "part:<entry id>"
// or "block:<block id>".
type Position struct {
	Node string  `json:"node"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// Nets lists a project's nets by name.
func (s *Store) Nets(ctx context.Context, projectID int64) ([]Net, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, bus, voltage, notes, source FROM project_nets WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Net{}
	for rows.Next() {
		n := Net{ProjectID: projectID}
		var v sql.NullFloat64
		if err := rows.Scan(&n.ID, &n.Name, &n.Kind, &n.Bus, &v, &n.Notes, &n.Source); err != nil {
			return nil, err
		}
		n.Voltage = floatPtr(v)
		out = append(out, n)
	}
	return out, rows.Err()
}

// SetNet adds a net or updates the one with the same name (ignoring case).
func (s *Store) SetNet(ctx context.Context, n Net) (Net, error) {
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		return n, errors.New("a net needs a name, e.g. SDA or EN")
	}
	switch n.Kind {
	case NetSignal, NetPower, NetGround:
	case "":
		n.Kind = NetSignal
	default:
		return n, errors.New("a net's kind is signal, power or ground")
	}
	if n.Source == "" {
		n.Source = SourceUser
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO project_nets (project_id, name, kind, bus, voltage, notes, source) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(project_id, name) DO UPDATE SET kind = excluded.kind, bus = excluded.bus, voltage = excluded.voltage, notes = excluded.notes, source = excluded.source
		RETURNING id`, n.ProjectID, n.Name, n.Kind, strings.TrimSpace(n.Bus), nullFloat(n.Voltage), n.Notes, n.Source).Scan(&n.ID)
	if err != nil {
		return n, err
	}
	s.touch(ctx, n.ProjectID)
	return n, nil
}

// netID returns a net's id, creating a signal net when there's none.
func (s *Store) netID(ctx context.Context, projectID int64, name, source string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM project_nets WHERE project_id = ? AND name = ?`, projectID, strings.TrimSpace(name)).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	n, err := s.SetNet(ctx, Net{ProjectID: projectID, Name: name, Source: source})
	return n.ID, err
}

// DeleteNet removes a net and its connections.
func (s *Store) DeleteNet(ctx context.Context, projectID int64, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_nets WHERE project_id = ? AND name = ?`, projectID, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.touch(ctx, projectID)
	return nil
}

// Blocks lists a project's off-BOM blocks by name.
func (s *Store) Blocks(ctx context.Context, projectID int64) ([]Block, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, value, notes, source FROM project_blocks WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Block{}
	for rows.Next() {
		b := Block{ProjectID: projectID}
		if err := rows.Scan(&b.ID, &b.Name, &b.Kind, &b.Value, &b.Notes, &b.Source); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// SetBlock adds a block or updates the one with the same name.
func (s *Store) SetBlock(ctx context.Context, b Block) (Block, error) {
	b.Name = strings.TrimSpace(b.Name)
	if b.Name == "" {
		return b, errors.New("a block needs a name, e.g. Battery or USB-C")
	}
	if b.Kind != "" && !slices.Contains(BlockKinds, b.Kind) {
		return b, fmt.Errorf("a block's kind is one of %s", strings.Join(BlockKinds, ", "))
	}
	if b.Source == "" {
		b.Source = SourceUser
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO project_blocks (project_id, name, kind, value, notes, source) VALUES (?,?,?,?,?,?)
		ON CONFLICT(project_id, name) DO UPDATE SET kind = excluded.kind, value = excluded.value, notes = excluded.notes, source = excluded.source RETURNING id`,
		b.ProjectID, b.Name, b.Kind, strings.TrimSpace(b.Value), b.Notes, b.Source).Scan(&b.ID)
	if err != nil {
		return b, err
	}
	s.touch(ctx, b.ProjectID)
	return b, nil
}

// DeleteBlock removes a block and its connections.
func (s *Store) DeleteBlock(ctx context.Context, projectID int64, name string) error {
	var id int64
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM project_blocks WHERE project_id = ? AND name = ?`, projectID, strings.TrimSpace(name)).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM project_blocks WHERE id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_layout WHERE project_id = ? AND node = ?`, projectID, BlockNode(id))
	s.touch(ctx, projectID)
	return err
}

// Connections lists a project's connections by net, then pin.
func (s *Store) Connections(ctx context.Context, projectID int64) ([]Connection, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.id, n.id, n.name, IFNULL(c.entry_id, 0), IFNULL(c.block_id, 0), c.ref, c.pin, c.notes, c.source
		FROM project_connections c JOIN project_nets n ON n.id = c.net_id WHERE n.project_id = ?
		ORDER BY n.name, c.entry_id IS NULL, c.entry_id, c.ref, c.block_id, CAST(c.pin AS INTEGER), c.pin`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Connection{}
	for rows.Next() {
		var c Connection
		if err := rows.Scan(&c.ID, &c.NetID, &c.Net, &c.EntryID, &c.BlockID, &c.Ref, &c.Pin, &c.Notes, &c.Source); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Connect puts a pin (or a block) on a net, creating the net if needed.
// A pin is on one net: connecting it again moves it.
func (s *Store) Connect(ctx context.Context, projectID int64, c Connection) (Connection, error) {
	if (c.EntryID == 0) == (c.BlockID == 0) {
		return c, errors.New("a connection is for a BOM entry or a block")
	}
	if c.Source == "" {
		c.Source = SourceUser
	}
	c.Pin = strings.TrimSpace(c.Pin)
	netID, err := s.netID(ctx, projectID, c.Net, c.Source)
	if err != nil {
		return c, err
	}
	c.NetID = netID
	err = s.tx(ctx, func(tx *sql.Tx) error {
		// The same pin elsewhere moves here; a pinless connection to the
		// same net is replaced.
		q := `DELETE FROM project_connections WHERE IFNULL(entry_id, 0) = ? AND IFNULL(block_id, 0) = ? AND ref = ? AND pin = ?`
		args := []any{c.EntryID, c.BlockID, c.Ref, c.Pin}
		if c.Pin == "" {
			q += ` AND net_id = ?`
			args = append(args, netID)
		}
		if _, err := tx.ExecContext(ctx, q, args...); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO project_connections (net_id, entry_id, block_id, ref, pin, notes, source) VALUES (?,?,?,?,?,?,?)`,
			netID, nullID(c.EntryID), nullID(c.BlockID), c.Ref, c.Pin, c.Notes, c.Source)
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return c, err
	}
	s.touch(ctx, projectID)
	return c, nil
}

// Disconnect removes a pin (or a block) from a net.
func (s *Store) Disconnect(ctx context.Context, projectID int64, c Connection) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_connections WHERE net_id IN (SELECT id FROM project_nets WHERE project_id = ? AND name = ?)
		AND IFNULL(entry_id, 0) = ? AND IFNULL(block_id, 0) = ? AND ref = ? AND pin = ?`, projectID, strings.TrimSpace(c.Net), c.EntryID, c.BlockID, c.Ref, strings.TrimSpace(c.Pin))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	s.touch(ctx, projectID)
	return nil
}

// PartNode names a BOM entry's block-diagram node, for Position.
func PartNode(entryID int64) string { return "part:" + strconv.FormatInt(entryID, 10) }

// RefNode names one designator's node, of an entry with several
// ("part:3/R1"); the entry's own node when ref is empty.
func RefNode(entryID int64, ref string) string {
	if ref == "" {
		return PartNode(entryID)
	}
	return PartNode(entryID) + "/" + ref
}

// BlockNode names a block's block-diagram node.
func BlockNode(blockID int64) string { return "block:" + strconv.FormatInt(blockID, 10) }

// Layout lists the saved block-diagram positions.
func (s *Store) Layout(ctx context.Context, projectID int64) ([]Position, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node, x, y FROM project_layout WHERE project_id = ? ORDER BY node`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Position{}
	for rows.Next() {
		var p Position
		if err := rows.Scan(&p.Node, &p.X, &p.Y); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetPosition saves where a node was placed.
func (s *Store) SetPosition(ctx context.Context, projectID int64, p Position) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_layout (project_id, node, x, y) VALUES (?,?,?,?)
		ON CONFLICT(project_id, node) DO UPDATE SET x = excluded.x, y = excluded.y`, projectID, p.Node, p.X, p.Y)
	return err
}

// ClearLayout forgets the saved positions, back to the automatic layout.
func (s *Store) ClearLayout(ctx context.Context, projectID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_layout WHERE project_id = ?`, projectID)
	return err
}

// nullID stores 0 as NULL.
func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
