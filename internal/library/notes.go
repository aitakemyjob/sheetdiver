package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// AddNote attaches a note to a target. datasheetID, when known, lets the
// note be found by searches scoped to that datasheet.
func (s *Store) AddNote(ctx context.Context, n Note, datasheetID int64) (Note, error) {
	n.Body = strings.TrimSpace(n.Body)
	if n.Body == "" {
		return n, errors.New("a note needs text")
	}
	if n.Author == "" {
		n.Author = SourceUser
	}
	t := now()
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO notes (target_kind, target_id, body, author, created_at, updated_at) VALUES (?,?,?,?,?,?)`,
			n.TargetKind, n.TargetID, n.Body, n.Author, t, t)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		ds := ""
		if datasheetID != 0 {
			ds = strconv.FormatInt(datasheetID, 10)
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO fts (kind, ref, datasheet, page, title, body) VALUES ('note', ?, ?, 0, ?, ?)`,
			fmt.Sprint(id), ds, "Note on "+n.TargetKind+" "+n.TargetID, n.Body)
		return err
	})
	if err != nil {
		return n, err
	}
	return s.Note(ctx, id)
}

// Note returns one note.
func (s *Store) Note(ctx context.Context, id int64) (Note, error) {
	var n Note
	var c, u string
	err := s.db.QueryRowContext(ctx, `SELECT id, target_kind, target_id, body, author, created_at, updated_at FROM notes WHERE id = ?`, id).
		Scan(&n.ID, &n.TargetKind, &n.TargetID, &n.Body, &n.Author, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		return n, fmt.Errorf("note %d: %w", id, ErrNotFound)
	}
	n.CreatedAt, n.UpdatedAt = parseTime(c), parseTime(u)
	return n, err
}

// Notes lists the notes on a target, oldest first. An empty kind lists
// every note.
func (s *Store) Notes(ctx context.Context, kind, id string) ([]Note, error) {
	q := `SELECT id, target_kind, target_id, body, author, created_at, updated_at FROM notes`
	var args []any
	if kind != "" {
		q += ` WHERE target_kind = ? AND target_id = ?`
		args = append(args, kind, id)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Note{}
	for rows.Next() {
		var n Note
		var c, u string
		if err := rows.Scan(&n.ID, &n.TargetKind, &n.TargetID, &n.Body, &n.Author, &c, &u); err != nil {
			return nil, err
		}
		n.CreatedAt, n.UpdatedAt = parseTime(c), parseTime(u)
		out = append(out, n)
	}
	return out, rows.Err()
}

// UpdateNote replaces a note's text.
func (s *Store) UpdateNote(ctx context.Context, id int64, body string) (Note, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Note{}, errors.New("a note needs text")
	}
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE notes SET body = ?, updated_at = ? WHERE id = ?`, body, now(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("note %d: %w", id, ErrNotFound)
		}
		_, err = tx.ExecContext(ctx, `UPDATE fts SET body = ? WHERE kind = 'note' AND ref = ?`, body, fmt.Sprint(id))
		return err
	})
	if err != nil {
		return Note{}, err
	}
	return s.Note(ctx, id)
}

// DeleteNote removes a note.
func (s *Store) DeleteNote(ctx context.Context, id int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM notes WHERE id = ?`, id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM fts WHERE kind = 'note' AND ref = ?`, fmt.Sprint(id))
		return err
	})
}
