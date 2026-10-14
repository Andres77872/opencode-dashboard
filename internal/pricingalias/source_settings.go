package pricingalias

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"opencode-dashboard/internal/source"
)

func migrateV4(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE source_settings (
	 source_id TEXT PRIMARY KEY, enabled INTEGER NOT NULL CHECK(enabled IN (0,1)), updated_ms INTEGER NOT NULL);
	 CREATE TABLE source_settings_revision (singleton INTEGER PRIMARY KEY CHECK(singleton=1), revision INTEGER NOT NULL);
	 INSERT INTO source_settings_revision VALUES(1,0);`)
	return err
}

func validateSourceSettingsSchema(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `SELECT source_id,enabled,updated_ms FROM source_settings WHERE 0`)
	if err != nil {
		return fmt.Errorf("validate source settings schema: %w", err)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	var revision int64
	if err := tx.QueryRowContext(ctx, `SELECT revision FROM source_settings_revision WHERE singleton=1`).Scan(&revision); err != nil {
		return fmt.Errorf("validate source settings revision: %w", err)
	}
	if revision < 0 {
		return fmt.Errorf("invalid source settings revision")
	}
	return nil
}

// One SELECT pins preferences and their revision to the same SQLite snapshot.
func (s *Store) SourceSettings(ctx context.Context) (map[source.SourceID]bool, int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT s.source_id,s.enabled,r.revision
	 FROM source_settings_revision r LEFT JOIN source_settings s ON 1=1 WHERE r.singleton=1`)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	settings := make(map[source.SourceID]bool)
	var revision int64
	for rows.Next() {
		var id sql.NullString
		var enabled sql.NullBool
		if err := rows.Scan(&id, &enabled, &revision); err != nil {
			return nil, 0, err
		}
		if id.Valid && source.IsKnown(source.SourceID(id.String)) {
			settings[source.SourceID(id.String)] = enabled.Bool
		}
	}
	return settings, revision, rows.Err()
}

func (s *Store) SetSourceEnabled(ctx context.Context, id source.SourceID, enabled bool) error {
	if !source.IsKnown(id) {
		return source.InvalidSourceError{ID: string(id)}
	}
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	previous := true
	err = tx.QueryRowContext(ctx, `SELECT enabled FROM source_settings WHERE source_id=?`, id).Scan(&previous)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if previous != enabled {
		if _, err := tx.ExecContext(ctx, `INSERT INTO source_settings(source_id,enabled,updated_ms) VALUES(?,?,?)
		 ON CONFLICT(source_id) DO UPDATE SET enabled=excluded.enabled,updated_ms=excluded.updated_ms`, id, enabled, time.Now().UnixMilli()); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE source_settings_revision SET revision=revision+1 WHERE singleton=1`); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("save source preference: %w", err)
	}
	return nil
}
