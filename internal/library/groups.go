package library

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// Group is a functional group of a project (Power, MCU, Sensors): the
// parts and blocks assigned to it are drawn together on the block
// diagram, framed, with its description and notes.
type Group struct {
	ID          int64  `json:"id"`
	ProjectID   int64  `json:"projectId"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Notes       string `json:"notes,omitempty"`
	// Below names the group it's drawn under, in that group's column;
	// "" starts a column of its own, in power-flow order.
	Below  string `json:"below,omitempty"`
	Source string `json:"source"`
}

// Assignment says where a block-diagram node belongs: the group it's
// drawn in, the node it supports (a decoupling capacitor for U1), beside
// which it's drawn, and the subgroup within its group it's wired up in
// with the other parts of that name (the VDD decoupling, a button and
// its pull-up). Node and Supports are nodes ("part:3", "part:3/R1",
// "block:2"); Group is the group's name and Cluster the subgroup's.
type Assignment struct {
	Node     string `json:"node"`
	Group    string `json:"group,omitempty"`
	Supports string `json:"supports,omitempty"`
	Cluster  string `json:"cluster,omitempty"`
	// Column and Row are where in its group it's asked to be drawn: the
	// column from the left and the place in it from the top, from 0.
	Column *int   `json:"column,omitempty"`
	Row    *int   `json:"row,omitempty"`
	Source string `json:"source"`
}

// Groups lists a project's groups by name.
func (s *Store) Groups(ctx context.Context, projectID int64) ([]Group, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, notes, below, source FROM project_groups WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Group{}
	for rows.Next() {
		g := Group{ProjectID: projectID}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Notes, &g.Below, &g.Source); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// SetGroup adds a group or updates the one with the same name (ignoring
// case).
func (s *Store) SetGroup(ctx context.Context, g Group) (Group, error) {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return g, errors.New("a group needs a name, e.g. Power or MCU")
	}
	if g.Source == "" {
		g.Source = SourceUser
	}
	g.Below = strings.TrimSpace(g.Below)
	if strings.EqualFold(g.Below, g.Name) {
		return g, errors.New("a group can't be drawn below itself")
	}
	err := s.db.QueryRowContext(ctx, `INSERT INTO project_groups (project_id, name, description, notes, below, source) VALUES (?,?,?,?,?,?)
		ON CONFLICT(project_id, name) DO UPDATE SET description = excluded.description, notes = excluded.notes, below = excluded.below, source = excluded.source
		RETURNING id`, g.ProjectID, g.Name, strings.TrimSpace(g.Description), g.Notes, g.Below, g.Source).Scan(&g.ID)
	if err != nil {
		return g, err
	}
	s.touch(ctx, g.ProjectID)
	return g, nil
}

// DeleteGroup removes a group; what was in it is drawn ungrouped, and
// the groups drawn below it start columns of their own.
func (s *Store) DeleteGroup(ctx context.Context, projectID int64, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_groups WHERE project_id = ? AND name = ?`, projectID, strings.TrimSpace(name))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE project_groups SET below = '' WHERE project_id = ? AND below = ? COLLATE NOCASE`, projectID, strings.TrimSpace(name)); err != nil {
		return err
	}
	if err := s.pruneAssignments(ctx, projectID); err != nil {
		return err
	}
	s.touch(ctx, projectID)
	return nil
}

// Assignments lists where a project's nodes belong, by node.
func (s *Store) Assignments(ctx context.Context, projectID int64) ([]Assignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.node, IFNULL(g.name, ''), a.supports, a.cluster, a.col, a.row, a.source
		FROM project_assignments a LEFT JOIN project_groups g ON g.id = a.group_id WHERE a.project_id = ? ORDER BY a.node`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Assignment{}
	for rows.Next() {
		var a Assignment
		var col, row sql.NullInt64
		if err := rows.Scan(&a.Node, &a.Group, &a.Supports, &a.Cluster, &col, &row, &a.Source); err != nil {
			return nil, err
		}
		a.Column, a.Row = intPtr(col), intPtr(row)
		out = append(out, a)
	}
	return out, rows.Err()
}

// Assign sets a node's group, what it supports and its subgroup,
// replacing all three; a group that doesn't exist yet is created. None
// set forgets the node.
func (s *Store) Assign(ctx context.Context, projectID int64, a Assignment) error {
	a.Node, a.Group, a.Supports, a.Cluster = strings.TrimSpace(a.Node), strings.TrimSpace(a.Group), strings.TrimSpace(a.Supports), strings.TrimSpace(a.Cluster)
	if a.Node == "" {
		return errors.New("an assignment is for a part or block")
	}
	if a.Supports == a.Node {
		return errors.New("a part can't support itself")
	}
	if a.Source == "" {
		a.Source = SourceUser
	}
	if a.Column != nil && *a.Column < 0 {
		a.Column = nil
	}
	if a.Row != nil && *a.Row < 0 {
		a.Row = nil
	}
	if a.Group == "" && a.Supports == "" && a.Cluster == "" && a.Column == nil && a.Row == nil {
		_, err := s.db.ExecContext(ctx, `DELETE FROM project_assignments WHERE project_id = ? AND node = ?`, projectID, a.Node)
		s.touch(ctx, projectID)
		return err
	}
	var group sql.NullInt64
	if a.Group != "" {
		id, err := s.groupID(ctx, projectID, a.Group, a.Source)
		if err != nil {
			return err
		}
		group = sql.NullInt64{Int64: id, Valid: true}
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_assignments (project_id, node, group_id, supports, cluster, col, row, source) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id, node) DO UPDATE SET group_id = excluded.group_id, supports = excluded.supports, cluster = excluded.cluster, col = excluded.col, row = excluded.row, source = excluded.source`,
		projectID, a.Node, group, a.Supports, a.Cluster, nullInt(a.Column), nullInt(a.Row), a.Source)
	if err != nil {
		return err
	}
	s.touch(ctx, projectID)
	return nil
}

// groupID returns a group's id, creating the group when there's none.
func (s *Store) groupID(ctx context.Context, projectID int64, name, source string) (int64, error) {
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM project_groups WHERE project_id = ? AND name = ?`, projectID, name).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	g, err := s.SetGroup(ctx, Group{ProjectID: projectID, Name: name, Source: source})
	return g.ID, err
}

// forgetNode drops a removed node's assignment, its designators' (for a
// BOM entry, "part:3/R1"), and anything saying it supports them.
func (s *Store) forgetNode(ctx context.Context, projectID int64, node string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM project_assignments WHERE project_id = ? AND (node = ? OR node LIKE ?)`, projectID, node, node+"/%"); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE project_assignments SET supports = '' WHERE project_id = ? AND (supports = ? OR supports LIKE ?)`, projectID, node, node+"/%"); err != nil {
		return err
	}
	return s.pruneAssignments(ctx, projectID)
}

// pruneAssignments drops assignments left with neither a group nor
// anything they support.
func (s *Store) pruneAssignments(ctx context.Context, projectID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_assignments WHERE project_id = ? AND group_id IS NULL AND supports = '' AND cluster = '' AND col IS NULL AND row IS NULL`, projectID)
	return err
}

// nullInt stores an optional number.
func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// intPtr reads an optional number.
func intPtr(n sql.NullInt64) *int {
	if !n.Valid {
		return nil
	}
	v := int(n.Int64)
	return &v
}
