package pricingalias

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"opencode-dashboard/internal/source"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "dashboard-settings.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func upsertTestAlias(t *testing.T, store *Store, alias Alias) Alias {
	t.Helper()
	stored, err := store.Upsert(context.Background(), alias)
	if err != nil {
		t.Fatalf("Upsert(%#v): %v", alias, err)
	}
	return stored
}

func TestCreateListAndResolve(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "dashboard-settings.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	if store.Path() != path {
		t.Fatalf("Path() = %q, want %q", store.Path(), path)
	}
	if revision := store.PricingAliasRevision(source.SourceCodex); revision != "" {
		t.Fatalf("empty revision = %q, want empty", revision)
	}

	withProvider := upsertTestAlias(t, store, Alias{
		SourceID:      source.SourceCodex,
		ProviderID:    "  openai  ",
		ModelID:       "  custom-codex  ",
		TargetModelID: "  gpt-5.5-codex  ",
	})
	withoutProvider := upsertTestAlias(t, store, Alias{
		SourceID:      source.SourceCodex,
		ModelID:       "local-name",
		TargetModelID: "gpt-5-codex",
	})
	if withProvider.ProviderID != "openai" || withProvider.ModelID != "custom-codex" || withProvider.TargetModelID != "gpt-5.5-codex" {
		t.Fatalf("normalized alias = %#v", withProvider)
	}
	if withProvider.CreatedMS <= 0 || withProvider.UpdatedMS != withProvider.CreatedMS {
		t.Fatalf("initial timestamps = created %d updated %d", withProvider.CreatedMS, withProvider.UpdatedMS)
	}
	if withoutProvider.ProviderID != "" {
		t.Fatalf("empty provider normalized to %q", withoutProvider.ProviderID)
	}

	aliases, err := store.List(ctx, source.SourceCodex)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(aliases) != 2 {
		t.Fatalf("List returned %d aliases, want 2: %#v", len(aliases), aliases)
	}
	if aliases[0].ProviderID != "" || aliases[0].ModelID != "local-name" || aliases[1].ProviderID != "openai" {
		t.Fatalf("List order/content = %#v", aliases)
	}

	if target, ok := store.ResolvePricingAlias(source.SourceCodex, " openai ", " custom-codex "); !ok || target.ModelID != "gpt-5.5-codex" {
		t.Fatalf("ResolvePricingAlias provider mapping = (%q, %v)", target.ModelID, ok)
	}
	if target, ok := store.ResolvePricingAlias(source.SourceCodex, "", "local-name"); !ok || target.ModelID != "gpt-5-codex" {
		t.Fatalf("ResolvePricingAlias empty-provider mapping = (%q, %v)", target.ModelID, ok)
	}
	if target, ok := store.ResolvePricingAlias(source.SourceCodex, "another-provider", "local-name"); ok || target.ModelID != "" {
		t.Fatalf("empty provider acted as wildcard: (%q, %v)", target, ok)
	}

	aliases[0].TargetModelID = "mutated-by-caller"
	fresh, err := store.List(ctx, source.SourceCodex)
	if err != nil {
		t.Fatalf("List after caller mutation: %v", err)
	}
	if fresh[0].TargetModelID != "gpt-5-codex" {
		t.Fatalf("List exposed mutable snapshot storage: %#v", fresh[0])
	}
}

func TestProviderAndSourceIsolation(t *testing.T) {
	store := openTestStore(t)
	for _, alias := range []Alias{
		{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "shared", TargetModelID: "codex-openai"},
		{SourceID: source.SourceCodex, ProviderID: "azure", ModelID: "shared", TargetModelID: "codex-azure"},
		{SourceID: source.SourceClaudeCode, ProviderID: "openai", ModelID: "shared", TargetModelID: "claude-openai"},
	} {
		upsertTestAlias(t, store, alias)
	}

	tests := []struct {
		sourceID source.SourceID
		provider string
		want     string
	}{
		{source.SourceCodex, "openai", "codex-openai"},
		{source.SourceCodex, "azure", "codex-azure"},
		{source.SourceClaudeCode, "openai", "claude-openai"},
	}
	for _, test := range tests {
		if got, ok := store.ResolvePricingAlias(test.sourceID, test.provider, "shared"); !ok || got.ModelID != test.want {
			t.Errorf("ResolvePricingAlias(%q, %q) = (%q, %v), want %q", test.sourceID, test.provider, got.ModelID, ok, test.want)
		}
	}
	if _, ok := store.ResolvePricingAlias(source.SourceClaudeCode, "azure", "shared"); ok {
		t.Fatal("provider mapping leaked across sources")
	}
}

func TestUpsertPreservesCreationAndAdvancesUpdateTimestamp(t *testing.T) {
	store := openTestStore(t)
	alias := Alias{SourceID: source.SourceQwenCode, ProviderID: "dashscope", ModelID: "custom", TargetModelID: "qwen3-coder-plus"}

	first := upsertTestAlias(t, store, alias)
	firstRevision := store.PricingAliasRevision(alias.SourceID)
	second := upsertTestAlias(t, store, alias)
	secondRevision := store.PricingAliasRevision(alias.SourceID)
	if second.CreatedMS != first.CreatedMS {
		t.Fatalf("idempotent upsert changed created_ms from %d to %d", first.CreatedMS, second.CreatedMS)
	}
	if second.UpdatedMS <= first.UpdatedMS {
		t.Fatalf("idempotent upsert did not advance updated_ms: first %d second %d", first.UpdatedMS, second.UpdatedMS)
	}
	if secondRevision != firstRevision {
		t.Fatalf("timestamp-only upsert changed revision from %q to %q", firstRevision, secondRevision)
	}

	alias.TargetModelID = "qwen3-coder-next"
	third := upsertTestAlias(t, store, alias)
	if third.CreatedMS != first.CreatedMS || third.UpdatedMS <= second.UpdatedMS {
		t.Fatalf("updated alias timestamps = %#v, first %#v second %#v", third, first, second)
	}
	if thirdRevision := store.PricingAliasRevision(alias.SourceID); thirdRevision == firstRevision {
		t.Fatal("changing target_model_id did not change revision")
	}
	if target, ok := store.ResolvePricingAlias(alias.SourceID, alias.ProviderID, alias.ModelID); !ok || target.ModelID != alias.TargetModelID {
		t.Fatalf("updated mapping = (%q, %v), want %q", target, ok, alias.TargetModelID)
	}
}

func TestDeleteIsExactAndIdempotent(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)
	for _, alias := range []Alias{
		{SourceID: source.SourceKimiCode, ProviderID: "", ModelID: "moonshot", TargetModelID: "kimi-k2"},
		{SourceID: source.SourceKimiCode, ProviderID: "custom", ModelID: "moonshot", TargetModelID: "kimi-k2-thinking"},
	} {
		upsertTestAlias(t, store, alias)
	}

	if err := store.Delete(ctx, source.SourceKimiCode, "", "moonshot"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := store.ResolvePricingAlias(source.SourceKimiCode, "", "moonshot"); ok {
		t.Fatal("deleted alias still resolves")
	}
	if target, ok := store.ResolvePricingAlias(source.SourceKimiCode, "custom", "moonshot"); !ok || target.ModelID != "kimi-k2-thinking" {
		t.Fatalf("delete removed another provider mapping: (%q, %v)", target, ok)
	}
	revision := store.PricingAliasRevision(source.SourceKimiCode)
	if err := store.Delete(ctx, source.SourceKimiCode, "", "moonshot"); err != nil {
		t.Fatalf("idempotent Delete: %v", err)
	}
	if got := store.PricingAliasRevision(source.SourceKimiCode); got != revision {
		t.Fatalf("deleting missing alias changed revision from %q to %q", revision, got)
	}

	if err := store.Delete(ctx, source.SourceKimiCode, "custom", "moonshot"); err != nil {
		t.Fatalf("Delete final alias: %v", err)
	}
	if got := store.PricingAliasRevision(source.SourceKimiCode); got != "" {
		t.Fatalf("revision after deleting all aliases = %q, want empty", got)
	}
}

func TestAliasesPersistAcrossReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dashboard-settings.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	stored, err := store.Upsert(ctx, Alias{
		SourceID: source.SourceClaudeCode, ProviderID: "anthropic", ModelID: "custom-sonnet", TargetModelID: "claude-sonnet-4-5",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	revision := store.PricingAliasRevision(source.SourceClaudeCode)
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Resolver reads are snapshot-backed and do not depend on a live SQL handle.
	if target, ok := store.ResolvePricingAlias(source.SourceClaudeCode, "anthropic", "custom-sonnet"); !ok || target.ModelID != "claude-sonnet-4-5" {
		t.Fatalf("ResolvePricingAlias after Close = (%q, %v)", target.ModelID, ok)
	}

	reopened, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	aliases, err := reopened.List(ctx, source.SourceClaudeCode)
	if err != nil {
		t.Fatalf("List after reopen: %v", err)
	}
	if len(aliases) != 1 || aliases[0] != stored {
		t.Fatalf("persisted aliases = %#v, want %#v", aliases, stored)
	}
	if got := reopened.PricingAliasRevision(source.SourceClaudeCode); got != revision {
		t.Fatalf("revision after reopen = %q, want %q", got, revision)
	}
}

func TestRevisionIsDeterministicPerSource(t *testing.T) {
	aliases := []Alias{
		{SourceID: source.SourceCodex, ProviderID: "b", ModelID: "model-2", TargetModelID: "target-2"},
		{SourceID: source.SourceCodex, ProviderID: "a", ModelID: "model-1", TargetModelID: "target-1"},
	}
	first := openTestStore(t)
	for _, alias := range aliases {
		upsertTestAlias(t, first, alias)
	}
	firstRevision := first.PricingAliasRevision(source.SourceCodex)
	if len(firstRevision) != sha256HexLength {
		t.Fatalf("revision length = %d, want %d: %q", len(firstRevision), sha256HexLength, firstRevision)
	}
	if _, err := hex.DecodeString(firstRevision); err != nil {
		t.Fatalf("revision is not lowercase hexadecimal SHA-256: %q: %v", firstRevision, err)
	}

	second := openTestStore(t)
	for i := len(aliases) - 1; i >= 0; i-- {
		upsertTestAlias(t, second, aliases[i])
	}
	if secondRevision := second.PricingAliasRevision(source.SourceCodex); secondRevision != firstRevision {
		t.Fatalf("insertion order changed revision: first %q second %q", firstRevision, secondRevision)
	}

	upsertTestAlias(t, first, Alias{SourceID: source.SourceClaudeCode, ModelID: "other", TargetModelID: "claude-opus-4-1"})
	if got := first.PricingAliasRevision(source.SourceCodex); got != firstRevision {
		t.Fatalf("another source changed Codex revision from %q to %q", firstRevision, got)
	}
	if got := first.PricingAliasRevision(source.SourceClaudeCode); got == "" || got == firstRevision {
		t.Fatalf("per-source revision was not isolated: %q", got)
	}
}

const sha256HexLength = 64

func TestOpenMigratesEmptyDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dashboard-settings.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	var version int
	if err := store.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("user_version = %d, want %d", version, schemaVersion)
	}
	var owner string
	if err := store.db.QueryRowContext(ctx, `SELECT owner FROM settings_marker WHERE singleton = 1`).Scan(&owner); err != nil {
		t.Fatalf("read settings marker: %v", err)
	}
	if owner != markerOwner {
		t.Fatalf("settings marker owner = %q, want %q", owner, markerOwner)
	}
	var indexes int
	if err := store.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master
		WHERE type = 'index' AND name = 'idx_pricing_aliases_target'
	`).Scan(&indexes); err != nil {
		t.Fatalf("inspect pricing alias index: %v", err)
	}
	if indexes != 1 {
		t.Fatalf("pricing alias index count = %d, want 1", indexes)
	}
}

func TestOpenExistingV1PreservesAliases(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "dashboard-settings.sqlite")
	db := openRawDB(t, path)
	if _, err := db.ExecContext(ctx, v1SchemaSQL+`
		INSERT INTO settings_marker(singleton, owner) VALUES(1, '`+markerOwner+`');
		INSERT INTO pricing_aliases(
			source_id, provider_id, model_id, target_model_id, created_ms, updated_ms
		) VALUES('codex', '', 'legacy-name', 'gpt-5-codex', 100, 200);
		PRAGMA user_version = 1;
	`); err != nil {
		t.Fatalf("create v1 database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close v1 database: %v", err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open existing v1: %v", err)
	}
	defer store.Close()
	// A v1 row predates cross-source targets, so it must read back as pointing at
	// the observing source's own catalog rather than at a blank source.
	if target, ok := store.ResolvePricingAlias(source.SourceCodex, "", "legacy-name"); !ok || target.ModelID != "gpt-5-codex" || target.SourceID != source.SourceCodex {
		t.Fatalf("v1 alias = (%#v, %v)", target, ok)
	}
	aliases, err := store.List(ctx, source.SourceCodex)
	if err != nil {
		t.Fatalf("List v1 aliases: %v", err)
	}
	if len(aliases) != 1 || aliases[0].CreatedMS != 100 || aliases[0].UpdatedMS != 200 {
		t.Fatalf("existing v1 aliases changed: %#v", aliases)
	}
	if aliases[0].TargetSourceID != source.SourceCodex || aliases[0].Foreign() {
		t.Fatalf("v1 alias target source = %#v, want a same-source backfill", aliases[0])
	}

	var stored string
	if err := store.db.QueryRowContext(ctx, `
		SELECT target_source_id FROM pricing_aliases WHERE model_id = 'legacy-name'
	`).Scan(&stored); err != nil {
		t.Fatalf("read backfilled target source: %v", err)
	}
	if stored != string(source.SourceCodex) {
		t.Fatalf("stored target_source_id = %q, want the row's own source", stored)
	}
}

// An alias may borrow another source's catalog, which has to round-trip through
// the store and change the revision independently of the target model id.
func TestCrossSourceTargetsRoundTripAndAffectRevision(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	local := Alias{SourceID: source.SourceClaudeCode, ProviderID: "anthropic", ModelID: "gpt-5.6-sol", TargetModelID: "claude-opus-5"}
	if _, err := store.Upsert(ctx, local); err != nil {
		t.Fatalf("Upsert same-source alias: %v", err)
	}
	sameSourceRevision := store.PricingAliasRevision(source.SourceClaudeCode)

	foreign := local
	foreign.TargetSourceID = source.SourceCodex
	foreign.TargetModelID = "gpt-5.6-sol"
	if _, err := store.Upsert(ctx, foreign); err != nil {
		t.Fatalf("Upsert cross-source alias: %v", err)
	}

	target, ok := store.ResolvePricingAlias(source.SourceClaudeCode, "anthropic", "gpt-5.6-sol")
	if !ok || target.SourceID != source.SourceCodex || target.ModelID != "gpt-5.6-sol" {
		t.Fatalf("cross-source target = (%#v, %v)", target, ok)
	}
	if store.PricingAliasRevision(source.SourceClaudeCode) == sameSourceRevision {
		t.Fatal("retargeting to another source did not change the revision")
	}

	// The target source alone must move the revision: the same model id in a
	// different catalog is a different price.
	retarget := foreign
	retarget.TargetSourceID = source.SourceKimiCode
	retarget.TargetModelID = "gpt-5.6-sol"
	if _, err := store.Upsert(ctx, retarget); err != nil {
		t.Fatalf("Upsert retargeted alias: %v", err)
	}
	if got := store.PricingAliasRevision(source.SourceClaudeCode); got == store.PricingAliasRevision(source.SourceCodex) {
		t.Fatal("revisions collided across sources")
	}

	snapshot := store.CapturePricingAliases(source.SourceClaudeCode)
	foreignAware, ok := snapshot.(source.PricingAliasForeignTargets)
	if !ok || !foreignAware.HasForeignTargets() {
		t.Fatalf("captured snapshot = %#v, want a snapshot reporting foreign targets", snapshot)
	}
	if plain := store.CapturePricingAliases(source.SourceCodex); plain.(source.PricingAliasForeignTargets).HasForeignTargets() {
		t.Fatal("a source with no aliases reported foreign targets")
	}
}

// An omitted target source means "my own catalog", which keeps same-source API
// requests and v1 rows expressible without a redundant field.
func TestUpsertDefaultsTargetSourceToTheObservingSource(t *testing.T) {
	ctx := context.Background()
	store := openTestStore(t)

	saved, err := store.Upsert(ctx, Alias{
		SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "custom", TargetModelID: "gpt-5.6",
	})
	if err != nil {
		t.Fatalf("Upsert without a target source: %v", err)
	}
	if saved.TargetSourceID != source.SourceCodex {
		t.Fatalf("target source = %q, want the observing source", saved.TargetSourceID)
	}
	if saved.Foreign() {
		t.Fatal("a defaulted target source must not read as cross-source")
	}
}

func TestOpenRefusesForeignDatabaseWithoutModifyingIt(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "foreign.sqlite")
	db := openRawDB(t, path)
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE valuable_data (id INTEGER PRIMARY KEY, value TEXT NOT NULL);
		INSERT INTO valuable_data(value) VALUES('keep me');
	`); err != nil {
		t.Fatalf("create foreign database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close foreign database: %v", err)
	}

	if _, err := Open(ctx, path); !errors.Is(err, ErrForeignDatabase) {
		t.Fatalf("Open foreign database error = %v, want ErrForeignDatabase", err)
	}
	db = openRawDB(t, path)
	defer db.Close()
	var value string
	if err := db.QueryRowContext(ctx, `SELECT value FROM valuable_data`).Scan(&value); err != nil {
		t.Fatalf("foreign data was modified: %v", err)
	}
	if value != "keep me" {
		t.Fatalf("foreign value = %q, want preserved", value)
	}
	var aliasesTable int
	if err := db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'pricing_aliases'
	`).Scan(&aliasesTable); err != nil {
		t.Fatalf("inspect foreign tables: %v", err)
	}
	if aliasesTable != 0 {
		t.Fatal("Open created pricing_aliases in a foreign database")
	}
}

func TestOpenRefusesFutureSchemaWithoutDroppingAliases(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "future.sqlite")
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	upsertTestAlias(t, store, Alias{SourceID: source.SourceCodex, ModelID: "future-model", TargetModelID: "gpt-5-codex"})
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	db := openRawDB(t, path)
	if _, err := db.ExecContext(ctx, `PRAGMA user_version = 99`); err != nil {
		t.Fatalf("set future user_version: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close future database: %v", err)
	}
	if _, err := Open(ctx, path); !errors.Is(err, ErrFutureSchemaVersion) {
		t.Fatalf("Open future database error = %v, want ErrFutureSchemaVersion", err)
	}

	db = openRawDB(t, path)
	defer db.Close()
	var target string
	if err := db.QueryRowContext(ctx, `SELECT target_model_id FROM pricing_aliases WHERE model_id = 'future-model'`).Scan(&target); err != nil {
		t.Fatalf("future alias was dropped: %v", err)
	}
	if target != "gpt-5-codex" {
		t.Fatalf("future alias target = %q", target)
	}
}

func TestOpenRefusesMalformedMarkerOwnership(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name      string
		markerSQL string
	}{
		{name: "wrong owner", markerSQL: `INSERT INTO settings_marker(singleton, owner) VALUES(1, 'another-application')`},
		{name: "missing owner row", markerSQL: ``},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "malformed.sqlite")
			db := openRawDB(t, path)
			if _, err := db.ExecContext(ctx, v1SchemaSQL+test.markerSQL+`; PRAGMA user_version = 1;`); err != nil {
				t.Fatalf("create malformed database: %v", err)
			}
			if err := db.Close(); err != nil {
				t.Fatalf("close malformed database: %v", err)
			}
			if _, err := Open(ctx, path); !errors.Is(err, ErrMalformedMarker) {
				t.Fatalf("Open malformed marker error = %v, want ErrMalformedMarker", err)
			}
		})
	}
}

func TestValidation(t *testing.T) {
	ctx := context.Background()
	if _, err := Open(ctx, "  "); !errors.Is(err, ErrPathRequired) {
		t.Fatalf("Open empty path error = %v, want ErrPathRequired", err)
	}
	store := openTestStore(t)

	tests := []struct {
		name  string
		alias Alias
		cause error
		field string
	}{
		{name: "empty source", alias: Alias{ModelID: "model", TargetModelID: "target"}, cause: ErrIdentifierRequired, field: "source_id"},
		{name: "empty model", alias: Alias{SourceID: source.SourceCodex, ModelID: "  ", TargetModelID: "target"}, cause: ErrIdentifierRequired, field: "model_id"},
		{name: "empty target", alias: Alias{SourceID: source.SourceCodex, ModelID: "model", TargetModelID: "  "}, cause: ErrIdentifierRequired, field: "target_model_id"},
		{name: "long source", alias: Alias{SourceID: source.SourceID(strings.Repeat("s", MaxSourceIDLength+1)), ModelID: "model", TargetModelID: "target"}, cause: ErrIdentifierTooLong, field: "source_id"},
		{name: "long provider", alias: Alias{SourceID: source.SourceCodex, ProviderID: strings.Repeat("p", MaxProviderIDLength+1), ModelID: "model", TargetModelID: "target"}, cause: ErrIdentifierTooLong, field: "provider_id"},
		{name: "long model", alias: Alias{SourceID: source.SourceCodex, ModelID: strings.Repeat("m", MaxModelIDLength+1), TargetModelID: "target"}, cause: ErrIdentifierTooLong, field: "model_id"},
		{name: "long target", alias: Alias{SourceID: source.SourceCodex, ModelID: "model", TargetModelID: strings.Repeat("t", MaxTargetModelIDLength+1)}, cause: ErrIdentifierTooLong, field: "target_model_id"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := store.Upsert(ctx, test.alias)
			assertValidationError(t, err, test.cause, test.field)
		})
	}

	if _, err := store.List(ctx, " "); err == nil {
		t.Fatal("List accepted empty source_id")
	} else {
		assertValidationError(t, err, ErrIdentifierRequired, "source_id")
	}
	if err := store.Delete(ctx, source.SourceCodex, "", " "); err == nil {
		t.Fatal("Delete accepted empty model_id")
	} else {
		assertValidationError(t, err, ErrIdentifierRequired, "model_id")
	}
	if err := store.Delete(ctx, source.SourceCodex, strings.Repeat("p", MaxProviderIDLength+1), "model"); err == nil {
		t.Fatal("Delete accepted long provider_id")
	} else {
		assertValidationError(t, err, ErrIdentifierTooLong, "provider_id")
	}
}

func assertValidationError(t *testing.T, err, cause error, field string) {
	t.Helper()
	if !errors.Is(err, ErrInvalidAlias) || !errors.Is(err, cause) {
		t.Fatalf("validation error = %v, want ErrInvalidAlias and %v", err, cause)
	}
	var validation *ValidationError
	if !errors.As(err, &validation) {
		t.Fatalf("validation error type = %T, want *ValidationError", err)
	}
	if validation.Field != field {
		t.Fatalf("validation field = %q, want %q", validation.Field, field)
	}
}

func openRawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		t.Fatalf("db.Ping: %v", err)
	}
	return db
}

const v1SchemaSQL = `
CREATE TABLE settings_marker (
	singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
	owner TEXT NOT NULL
);
CREATE TABLE pricing_aliases (
	source_id TEXT NOT NULL,
	provider_id TEXT NOT NULL DEFAULT '',
	model_id TEXT NOT NULL,
	target_model_id TEXT NOT NULL,
	created_ms INTEGER NOT NULL,
	updated_ms INTEGER NOT NULL,
	PRIMARY KEY (source_id, provider_id, model_id)
);
`
