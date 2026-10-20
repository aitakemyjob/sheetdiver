package library

import (
	"context"
	"database/sql"
	"strings"
)

// Translation is a text translated into English on this machine (the
// offline translation engine runs in the UI and saves what it
// translated, so it's done once and search can find it).
type Translation struct {
	// Lang is the source language ("zh").
	Lang   string `json:"lang"`
	Source string `json:"source"`
	Text   string `json:"text"`
}

// Translations returns the saved English for the given texts, keyed by
// source text; texts without a translation are left out.
func (s *Store) Translations(ctx context.Context, lang string, sources []string) (map[string]string, error) {
	out := map[string]string{}
	for len(sources) > 0 {
		batch := sources[:min(len(sources), 400)]
		sources = sources[len(batch):]
		args := []any{lang}
		for _, src := range batch {
			args = append(args, src)
		}
		rows, err := s.db.QueryContext(ctx, `SELECT source, text FROM translations WHERE lang = ? AND source IN (`+strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")+`)`, args...) //nolint:gosec // placeholders only
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var src, text string
			if err := rows.Scan(&src, &text); err != nil {
				rows.Close()
				return nil, err
			}
			out[src] = text
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// SaveTranslations stores translations (replacing earlier ones).
func (s *Store) SaveTranslations(ctx context.Context, ts []Translation) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, t := range ts {
			t.Source, t.Text = strings.TrimSpace(t.Source), strings.TrimSpace(t.Text)
			if t.Lang == "" || t.Source == "" || t.Text == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO translations (lang, source, text, created_at) VALUES (?,?,?,?)`, t.Lang, t.Source, t.Text, now()); err != nil {
				return err
			}
		}
		return nil
	})
}
