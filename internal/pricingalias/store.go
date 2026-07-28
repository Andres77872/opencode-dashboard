// Package pricingalias owns durable user-managed mappings from detected model
// identifiers to bundled pricing catalog model identifiers.
package pricingalias

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"opencode-dashboard/internal/source"

	_ "modernc.org/sqlite"
)

const (
	busyTimeout = 5 * time.Second

	markerOwner = "opencode-dashboard-settings"

	MaxSourceIDLength       = 128
	MaxProviderIDLength     = 256
	MaxModelIDLength        = 512
	MaxTargetSourceIDLength = 128
	MaxTargetModelIDLength  = 512
)

var (
	ErrInvalidAlias        = errors.New("invalid pricing alias")
	ErrIdentifierRequired  = errors.New("pricing alias identifier is required")
	ErrIdentifierTooLong   = errors.New("pricing alias identifier is too long")
	ErrPathRequired        = errors.New("pricing alias store path is required")
	ErrForeignDatabase     = errors.New("foreign settings database")
	ErrFutureSchemaVersion = errors.New("settings database schema is newer than this binary")
	ErrMalformedMarker     = errors.New("malformed settings database ownership marker")
)

// ValidationError identifies a caller-controlled pricing alias field that was
// rejected. It matches ErrInvalidAlias as well as its more specific Cause.
type ValidationError struct {
	Field string
	Limit int
	Cause error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ErrInvalidAlias.Error()
	}
	switch {
	case errors.Is(e.Cause, ErrIdentifierRequired):
		return fmt.Sprintf("invalid pricing alias %s: value is required", e.Field)
	case errors.Is(e.Cause, ErrIdentifierTooLong):
		return fmt.Sprintf("invalid pricing alias %s: exceeds maximum length of %d characters", e.Field, e.Limit)
	default:
		return fmt.Sprintf("invalid pricing alias %s", e.Field)
	}
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return ErrInvalidAlias
	}
	return e.Cause
}

func (e *ValidationError) Is(target error) bool {
	return target == ErrInvalidAlias || (e != nil && target == e.Cause)
}

// Alias is one direct pricing model mapping keyed by the observing source. An
// empty ProviderID is a literal key and never acts as a wildcard.
//
// TargetSourceID names the catalog that supplies the rate and is normalized to
// SourceID when empty, which is how schema v1 rows and same-source API requests
// both read as "price from my own catalog". A different value is a cross-source
// alias: the observing CLI reports a model another vendor prices.
type Alias struct {
	SourceID       source.SourceID `json:"source_id"`
	ProviderID     string          `json:"provider_id"`
	ModelID        string          `json:"model_id"`
	TargetSourceID source.SourceID `json:"target_source_id"`
	TargetModelID  string          `json:"target_model_id"`
	CreatedMS      int64           `json:"created_ms"`
	UpdatedMS      int64           `json:"updated_ms"`
}

// Foreign reports whether the alias borrows another source's pricing catalog.
func (a Alias) Foreign() bool {
	return a.TargetSourceID != "" && a.TargetSourceID != a.SourceID
}

type aliasKey struct {
	sourceID   source.SourceID
	providerID string
	modelID    string
}

type aliasSnapshot struct {
	byKey     map[aliasKey]Alias
	bySource  map[source.SourceID][]Alias
	revisions map[source.SourceID]string
	foreign   map[source.SourceID]bool
}

// Store is a writable SQLite settings store with lock-free pricing alias reads.
type Store struct {
	db      *sql.DB
	path    string
	write   sync.Mutex
	aliases atomic.Pointer[aliasSnapshot]
}

var (
	_ source.PricingAliasResolver         = (*Store)(nil)
	_ source.PricingAliasSnapshotProvider = (*Store)(nil)
)

type capturedSnapshot struct {
	snapshot *aliasSnapshot
	sourceID source.SourceID
}

var _ source.PricingAliasForeignTargets = capturedSnapshot{}

func (s capturedSnapshot) ResolvePricingAlias(providerID, modelID string) (source.PricingAliasTarget, bool) {
	providerID, err := normalizeOptionalIdentifier("provider_id", providerID, MaxProviderIDLength)
	if err != nil {
		return source.PricingAliasTarget{}, false
	}
	modelID, err = normalizeRequiredIdentifier("model_id", modelID, MaxModelIDLength)
	if err != nil {
		return source.PricingAliasTarget{}, false
	}
	alias, ok := s.snapshot.byKey[aliasKey{sourceID: s.sourceID, providerID: providerID, modelID: modelID}]
	if !ok {
		return source.PricingAliasTarget{}, false
	}
	return aliasTarget(alias), true
}

func (s capturedSnapshot) Revision() string {
	return s.snapshot.revisions[s.sourceID]
}

// HasForeignTargets reports whether this source's mappings depend on another
// source's bundled catalog, which the caller folds into the cache identity.
func (s capturedSnapshot) HasForeignTargets() bool {
	return s.snapshot.foreign[s.sourceID]
}

func aliasTarget(alias Alias) source.PricingAliasTarget {
	targetSource := alias.TargetSourceID
	if targetSource == "" {
		targetSource = alias.SourceID
	}
	return source.PricingAliasTarget{SourceID: targetSource, ModelID: alias.TargetModelID}
}

type migration func(context.Context, *sql.Tx) error

var (
	migrations    = []migration{migrateV1, migrateV2}
	schemaVersion = len(migrations)
)

// Open opens or creates the settings database at path, applies forward-only
// migrations, and loads the initial in-memory alias snapshot.
func Open(ctx context.Context, path string) (*Store, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, ErrPathRequired
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create pricing alias store directory: %w", err)
		}
	}

	db, err := openDB(ctx, path)
	if err != nil {
		return nil, err
	}
	if err := ensureSchema(ctx, db, path); err != nil {
		_ = db.Close()
		return nil, err
	}
	snapshot, err := loadSnapshot(ctx, db)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("load pricing aliases: %w", err)
	}

	store := &Store{db: db, path: path}
	store.aliases.Store(snapshot)
	return store, nil
}

func openDB(ctx context.Context, path string) (*sql.DB, error) {
	params := []string{
		"_txlock=immediate",
		fmt.Sprintf("_pragma=busy_timeout(%d)", busyTimeout.Milliseconds()),
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
	}
	db, err := sql.Open("sqlite", path+"?"+strings.Join(params, "&"))
	if err != nil {
		return nil, fmt.Errorf("open pricing alias store: %w", err)
	}
	db.SetMaxOpenConns(2)
	db.SetMaxIdleConns(1)
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("connect pricing alias store: %w", err)
	}
	return db, nil
}

func ensureSchema(ctx context.Context, db *sql.DB, path string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin settings schema migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var version int
	if err := tx.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read settings schema version: %w", err)
	}
	if version > schemaVersion {
		return fmt.Errorf("%w: database has version %d, binary supports %d", ErrFutureSchemaVersion, version, schemaVersion)
	}

	var tables int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
	`).Scan(&tables); err != nil {
		return fmt.Errorf("inspect settings database: %w", err)
	}
	var markerTables int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'table' AND name = 'settings_marker'
	`).Scan(&markerTables); err != nil {
		return fmt.Errorf("inspect settings database marker: %w", err)
	}

	if tables > 0 {
		if markerTables != 1 {
			return fmt.Errorf("%w: %s has no settings_marker table; refusing to modify it", ErrForeignDatabase, path)
		}
		if err := verifyMarker(ctx, tx); err != nil {
			return err
		}
	} else if version != 0 {
		return fmt.Errorf("%w: schema version %d has no ownership marker", ErrMalformedMarker, version)
	}

	for next := version + 1; next <= schemaVersion; next++ {
		if err := migrations[next-1](ctx, tx); err != nil {
			return fmt.Errorf("apply settings migration %d: %w", next, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`PRAGMA user_version = %d`, next)); err != nil {
			return fmt.Errorf("record settings schema version %d: %w", next, err)
		}
	}
	if err := verifyMarker(ctx, tx); err != nil {
		return err
	}
	if err := validateAliasSchema(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit settings schema migration: %w", err)
	}
	return nil
}

func migrateV1(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS settings_marker (
			singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
			owner TEXT NOT NULL
		);
		CREATE TABLE IF NOT EXISTS pricing_aliases (
			source_id TEXT NOT NULL,
			provider_id TEXT NOT NULL DEFAULT '',
			model_id TEXT NOT NULL,
			target_model_id TEXT NOT NULL,
			created_ms INTEGER NOT NULL,
			updated_ms INTEGER NOT NULL,
			PRIMARY KEY (source_id, provider_id, model_id)
		);
		CREATE INDEX IF NOT EXISTS idx_pricing_aliases_target
			ON pricing_aliases(source_id, target_model_id);
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO settings_marker(singleton, owner) VALUES(1, ?)
		ON CONFLICT(singleton) DO NOTHING
	`, markerOwner); err != nil {
		return err
	}
	return nil
}

// migrateV2 adds the alias target's source. Existing rows targeted the source
// that observed the model, so they are backfilled with their own source id
// rather than left at the "" default, which keeps the stored revision inputs
// identical to what the resolver now reads back.
func migrateV2(ctx context.Context, tx *sql.Tx) error {
	var exists int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM pragma_table_info('pricing_aliases') WHERE name = 'target_source_id'
	`).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if _, err := tx.ExecContext(ctx, `
			ALTER TABLE pricing_aliases ADD COLUMN target_source_id TEXT NOT NULL DEFAULT ''
		`); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE pricing_aliases SET target_source_id = source_id WHERE target_source_id = ''
	`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DROP INDEX IF EXISTS idx_pricing_aliases_target;
		CREATE INDEX IF NOT EXISTS idx_pricing_aliases_target
			ON pricing_aliases(target_source_id, target_model_id);
	`); err != nil {
		return err
	}
	return nil
}

func verifyMarker(ctx context.Context, tx *sql.Tx) error {
	var count int
	var owner sql.NullString
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*), MIN(owner) FROM settings_marker WHERE singleton = 1
	`).Scan(&count, &owner); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedMarker, err)
	}
	if count != 1 || !owner.Valid || owner.String != markerOwner {
		return fmt.Errorf("%w: owner must be %q", ErrMalformedMarker, markerOwner)
	}

	var total int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM settings_marker`).Scan(&total); err != nil {
		return fmt.Errorf("%w: %v", ErrMalformedMarker, err)
	}
	if total != 1 {
		return fmt.Errorf("%w: expected exactly one ownership row", ErrMalformedMarker)
	}
	return nil
}

func validateAliasSchema(ctx context.Context, tx *sql.Tx) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT source_id, provider_id, model_id, target_source_id, target_model_id, created_ms, updated_ms
		FROM pricing_aliases WHERE 0
	`)
	if err != nil {
		return fmt.Errorf("validate pricing alias schema: %w", err)
	}
	return rows.Close()
}

// Close closes the underlying SQLite database. Resolver snapshots remain safe
// to read, but no further writes should be attempted.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	s.write.Lock()
	defer s.write.Unlock()
	return s.db.Close()
}

// Path returns the SQLite path supplied to Open.
func (s *Store) Path() string {
	if s == nil {
		return ""
	}
	return s.path
}

// List returns a stable copy of aliases for sourceID from the in-memory
// snapshot, ordered by provider id and model id.
func (s *Store) List(ctx context.Context, sourceID source.SourceID) ([]Alias, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	normalized, err := normalizeSourceID(sourceID)
	if err != nil {
		return nil, err
	}
	snapshot := s.currentSnapshot()
	aliases := snapshot.bySource[normalized]
	return append([]Alias(nil), aliases...), nil
}

// Upsert creates or replaces one mapping. The original creation timestamp is
// retained, while updated_ms advances on every successful upsert, including an
// idempotent write of the same target.
func (s *Store) Upsert(ctx context.Context, alias Alias) (Alias, error) {
	normalized, err := normalizeAlias(alias)
	if err != nil {
		return Alias{}, err
	}
	if s == nil || s.db == nil {
		return Alias{}, errors.New("pricing alias store is closed")
	}

	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Alias{}, fmt.Errorf("begin pricing alias upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UnixMilli()
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO pricing_aliases(
			source_id, provider_id, model_id, target_source_id, target_model_id, created_ms, updated_ms
		) VALUES(?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(source_id, provider_id, model_id) DO UPDATE SET
			target_source_id = excluded.target_source_id,
			target_model_id = excluded.target_model_id,
			updated_ms = CASE
				WHEN excluded.updated_ms > pricing_aliases.updated_ms THEN excluded.updated_ms
				ELSE pricing_aliases.updated_ms + 1
			END
		RETURNING created_ms, updated_ms
	`, normalized.SourceID, normalized.ProviderID, normalized.ModelID, normalized.TargetSourceID, normalized.TargetModelID, now, now).Scan(&normalized.CreatedMS, &normalized.UpdatedMS); err != nil {
		return Alias{}, fmt.Errorf("upsert pricing alias: %w", err)
	}
	next, err := loadSnapshot(ctx, tx)
	if err != nil {
		return Alias{}, fmt.Errorf("refresh pricing alias snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Alias{}, fmt.Errorf("commit pricing alias upsert: %w", err)
	}
	s.aliases.Store(next)
	return normalized, nil
}

// Delete removes one exact source/provider/model mapping. Deleting a missing
// mapping succeeds without changing the snapshot revision.
func (s *Store) Delete(ctx context.Context, sourceID source.SourceID, providerID, modelID string) error {
	normalizedSource, err := normalizeSourceID(sourceID)
	if err != nil {
		return err
	}
	normalizedProvider, err := normalizeOptionalIdentifier("provider_id", providerID, MaxProviderIDLength)
	if err != nil {
		return err
	}
	normalizedModel, err := normalizeRequiredIdentifier("model_id", modelID, MaxModelIDLength)
	if err != nil {
		return err
	}
	if s == nil || s.db == nil {
		return errors.New("pricing alias store is closed")
	}

	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin pricing alias delete: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM pricing_aliases
		WHERE source_id = ? AND provider_id = ? AND model_id = ?
	`, normalizedSource, normalizedProvider, normalizedModel); err != nil {
		return fmt.Errorf("delete pricing alias: %w", err)
	}
	next, err := loadSnapshot(ctx, tx)
	if err != nil {
		return fmt.Errorf("refresh pricing alias snapshot: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit pricing alias delete: %w", err)
	}
	s.aliases.Store(next)
	return nil
}

// CapturePricingAliases atomically binds one source's mappings and revision to
// the same immutable in-memory snapshot.
func (s *Store) CapturePricingAliases(sourceID source.SourceID) source.PricingAliasSnapshot {
	normalized, err := normalizeSourceID(sourceID)
	if err != nil {
		return nil
	}
	return capturedSnapshot{snapshot: s.currentSnapshot(), sourceID: normalized}
}

// ResolvePricingAlias resolves one exact source/provider/model key entirely
// from the current in-memory snapshot.
func (s *Store) ResolvePricingAlias(sourceID source.SourceID, providerID, modelID string) (source.PricingAliasTarget, bool) {
	normalizedSource, err := normalizeSourceID(sourceID)
	if err != nil {
		return source.PricingAliasTarget{}, false
	}
	normalizedProvider, err := normalizeOptionalIdentifier("provider_id", providerID, MaxProviderIDLength)
	if err != nil {
		return source.PricingAliasTarget{}, false
	}
	normalizedModel, err := normalizeRequiredIdentifier("model_id", modelID, MaxModelIDLength)
	if err != nil {
		return source.PricingAliasTarget{}, false
	}
	alias, ok := s.currentSnapshot().byKey[aliasKey{
		sourceID: normalizedSource, providerID: normalizedProvider, modelID: normalizedModel,
	}]
	if !ok {
		return source.PricingAliasTarget{}, false
	}
	return aliasTarget(alias), true
}

// PricingAliasRevision returns the full lowercase SHA-256 revision for one
// source's sorted mapping tuples, or an empty string when the source has none.
func (s *Store) PricingAliasRevision(sourceID source.SourceID) string {
	normalized, err := normalizeSourceID(sourceID)
	if err != nil {
		return ""
	}
	return s.currentSnapshot().revisions[normalized]
}

func (s *Store) currentSnapshot() *aliasSnapshot {
	if s == nil {
		return emptySnapshot()
	}
	if snapshot := s.aliases.Load(); snapshot != nil {
		return snapshot
	}
	return emptySnapshot()
}

type rowQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func loadSnapshot(ctx context.Context, queryer rowQueryer) (*aliasSnapshot, error) {
	rows, err := queryer.QueryContext(ctx, `
		SELECT source_id, provider_id, model_id, target_source_id, target_model_id, created_ms, updated_ms
		FROM pricing_aliases
		ORDER BY source_id, provider_id, model_id, target_source_id, target_model_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var aliases []Alias
	for rows.Next() {
		var rawSource, rawTargetSource string
		var alias Alias
		if err := rows.Scan(
			&rawSource, &alias.ProviderID, &alias.ModelID, &rawTargetSource, &alias.TargetModelID,
			&alias.CreatedMS, &alias.UpdatedMS,
		); err != nil {
			return nil, err
		}
		alias.SourceID = source.SourceID(rawSource)
		alias.TargetSourceID = source.SourceID(rawTargetSource)
		normalized, err := normalizeAlias(alias)
		if err != nil {
			return nil, fmt.Errorf("invalid stored pricing alias: %w", err)
		}
		normalized.CreatedMS = alias.CreatedMS
		normalized.UpdatedMS = alias.UpdatedMS
		aliases = append(aliases, normalized)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return buildSnapshot(aliases), nil
}

func buildSnapshot(aliases []Alias) *aliasSnapshot {
	snapshot := &aliasSnapshot{
		byKey:     make(map[aliasKey]Alias, len(aliases)),
		bySource:  make(map[source.SourceID][]Alias),
		revisions: make(map[source.SourceID]string),
		foreign:   make(map[source.SourceID]bool),
	}
	for _, alias := range aliases {
		key := aliasKey{sourceID: alias.SourceID, providerID: alias.ProviderID, modelID: alias.ModelID}
		snapshot.byKey[key] = alias
		snapshot.bySource[alias.SourceID] = append(snapshot.bySource[alias.SourceID], alias)
		if alias.Foreign() {
			snapshot.foreign[alias.SourceID] = true
		}
	}
	for sourceID, sourceAliases := range snapshot.bySource {
		sort.Slice(sourceAliases, func(i, j int) bool {
			if sourceAliases[i].ProviderID != sourceAliases[j].ProviderID {
				return sourceAliases[i].ProviderID < sourceAliases[j].ProviderID
			}
			if sourceAliases[i].ModelID != sourceAliases[j].ModelID {
				return sourceAliases[i].ModelID < sourceAliases[j].ModelID
			}
			if sourceAliases[i].TargetSourceID != sourceAliases[j].TargetSourceID {
				return sourceAliases[i].TargetSourceID < sourceAliases[j].TargetSourceID
			}
			return sourceAliases[i].TargetModelID < sourceAliases[j].TargetModelID
		})
		snapshot.bySource[sourceID] = sourceAliases
		snapshot.revisions[sourceID] = revisionFor(sourceAliases)
	}
	return snapshot
}

func emptySnapshot() *aliasSnapshot {
	return &aliasSnapshot{
		byKey:     map[aliasKey]Alias{},
		bySource:  map[source.SourceID][]Alias{},
		revisions: map[source.SourceID]string{},
		foreign:   map[source.SourceID]bool{},
	}
}

func revisionFor(aliases []Alias) string {
	if len(aliases) == 0 {
		return ""
	}
	digest := sha256.New()
	for _, alias := range aliases {
		writeHashField(digest, string(alias.SourceID))
		writeHashField(digest, alias.ProviderID)
		writeHashField(digest, alias.ModelID)
		writeHashField(digest, string(alias.TargetSourceID))
		writeHashField(digest, alias.TargetModelID)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func writeHashField(digest hash.Hash, value string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(value)))
	_, _ = digest.Write(length[:])
	_, _ = digest.Write([]byte(value))
}

func normalizeAlias(alias Alias) (Alias, error) {
	var err error
	alias.SourceID, err = normalizeSourceID(alias.SourceID)
	if err != nil {
		return Alias{}, err
	}
	alias.ProviderID, err = normalizeOptionalIdentifier("provider_id", alias.ProviderID, MaxProviderIDLength)
	if err != nil {
		return Alias{}, err
	}
	alias.ModelID, err = normalizeRequiredIdentifier("model_id", alias.ModelID, MaxModelIDLength)
	if err != nil {
		return Alias{}, err
	}
	// An omitted target source means "my own catalog": schema v1 rows carry the
	// column default and same-source API requests omit the field.
	if strings.TrimSpace(string(alias.TargetSourceID)) == "" {
		alias.TargetSourceID = alias.SourceID
	}
	targetSource, err := normalizeRequiredIdentifier("target_source_id", string(alias.TargetSourceID), MaxTargetSourceIDLength)
	if err != nil {
		return Alias{}, err
	}
	alias.TargetSourceID = source.SourceID(targetSource)
	alias.TargetModelID, err = normalizeRequiredIdentifier("target_model_id", alias.TargetModelID, MaxTargetModelIDLength)
	if err != nil {
		return Alias{}, err
	}
	alias.CreatedMS = 0
	alias.UpdatedMS = 0
	return alias, nil
}

func normalizeSourceID(value source.SourceID) (source.SourceID, error) {
	normalized, err := normalizeRequiredIdentifier("source_id", string(value), MaxSourceIDLength)
	return source.SourceID(normalized), err
}

func normalizeRequiredIdentifier(field, value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", &ValidationError{Field: field, Cause: ErrIdentifierRequired}
	}
	if utf8.RuneCountInString(value) > limit {
		return "", &ValidationError{Field: field, Limit: limit, Cause: ErrIdentifierTooLong}
	}
	return value, nil
}

func normalizeOptionalIdentifier(field, value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) > limit {
		return "", &ValidationError{Field: field, Limit: limit, Cause: ErrIdentifierTooLong}
	}
	return value, nil
}
