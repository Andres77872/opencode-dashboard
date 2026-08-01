package pricingalias

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxAssistantProviderIDLength   = 96
	MaxAssistantProviderNameLength = 120
	MaxAssistantBaseURLLength      = 2048
	MaxAssistantAPIKeyLength       = 16 << 10
	MaxAssistantModelIDLength      = 512
	MaxAssistantContextLimit       = 16_000_000
)

var (
	ErrProviderNotFound = errors.New("assistant provider not found")
	ErrModelNotFound    = errors.New("assistant model not found")
	ErrInvalidProvider  = errors.New("invalid assistant provider")
	ErrInvalidSelection = errors.New("invalid assistant selection")
)

// AssistantProvider is the safe settings representation returned by public
// APIs. APIKey is intentionally excluded from JSON and must never be logged.
type AssistantProvider struct {
	ID                   string           `json:"id"`
	Name                 string           `json:"name"`
	BaseURL              string           `json:"base_url"`
	APIKeyConfigured     bool             `json:"api_key_configured"`
	InsecureTransportAck bool             `json:"insecure_transport_ack"`
	Models               []AssistantModel `json:"models"`
	Catalog              CatalogState     `json:"catalog"`
	CreatedMS            int64            `json:"created_ms"`
	UpdatedMS            int64            `json:"updated_ms"`
	APIKey               string           `json:"-"`
}

type AssistantModel struct {
	ID           string `json:"id"`
	ContextLimit int    `json:"context_limit"`
	Verified     bool   `json:"verified"`
	Discovered   bool   `json:"discovered"`
	UpdatedMS    int64  `json:"updated_ms"`
}

type CatalogState struct {
	Status        string `json:"status"`
	LastAttemptMS int64  `json:"last_attempt_ms,omitempty"`
	LastSuccessMS int64  `json:"last_success_ms,omitempty"`
	Error         string `json:"error,omitempty"`
}

type AssistantSelection struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id"`
	Revision   int64  `json:"revision"`
	UpdatedMS  int64  `json:"updated_ms"`
}

type CreateAssistantProviderInput struct {
	Name                 string
	BaseURL              string
	APIKey               string
	InsecureTransportAck bool
}

// UpdateAssistantProviderInput distinguishes an omitted key from replacement
// and explicit clearing. ClearAPIKey wins when both fields are supplied.
type UpdateAssistantProviderInput struct {
	Name                 *string
	BaseURL              *string
	APIKey               *string
	ClearAPIKey          bool
	InsecureTransportAck *bool
}

func (s *Store) ListAssistantProviders(ctx context.Context) ([]AssistantProvider, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("settings store is closed")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_id, name, base_url, api_key, insecure_transport_ack, created_ms, updated_ms
		FROM assistant_providers ORDER BY lower(name), provider_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list assistant providers: %w", err)
	}
	defer rows.Close()
	providers := make([]AssistantProvider, 0)
	for rows.Next() {
		var provider AssistantProvider
		var insecure int
		if err := rows.Scan(&provider.ID, &provider.Name, &provider.BaseURL, &provider.APIKey, &insecure, &provider.CreatedMS, &provider.UpdatedMS); err != nil {
			return nil, fmt.Errorf("scan assistant provider: %w", err)
		}
		provider.APIKeyConfigured = provider.APIKey != ""
		provider.InsecureTransportAck = insecure != 0
		if err := s.loadAssistantProviderDetails(ctx, &provider); err != nil {
			return nil, err
		}
		providers = append(providers, provider)
	}
	return providers, rows.Err()
}

func (s *Store) GetAssistantProvider(ctx context.Context, id string) (AssistantProvider, error) {
	id, err := assistantIdentifier("provider_id", id, MaxAssistantProviderIDLength)
	if err != nil {
		return AssistantProvider{}, err
	}
	var provider AssistantProvider
	var insecure int
	err = s.db.QueryRowContext(ctx, `
		SELECT provider_id, name, base_url, api_key, insecure_transport_ack, created_ms, updated_ms
		FROM assistant_providers WHERE provider_id = ?
	`, id).Scan(&provider.ID, &provider.Name, &provider.BaseURL, &provider.APIKey, &insecure, &provider.CreatedMS, &provider.UpdatedMS)
	if errors.Is(err, sql.ErrNoRows) {
		return AssistantProvider{}, ErrProviderNotFound
	}
	if err != nil {
		return AssistantProvider{}, fmt.Errorf("get assistant provider: %w", err)
	}
	provider.APIKeyConfigured = provider.APIKey != ""
	provider.InsecureTransportAck = insecure != 0
	if err := s.loadAssistantProviderDetails(ctx, &provider); err != nil {
		return AssistantProvider{}, err
	}
	return provider, nil
}

func (s *Store) loadAssistantProviderDetails(ctx context.Context, provider *AssistantProvider) error {
	rows, err := s.db.QueryContext(ctx, `
		SELECT model_id, context_limit, verified, discovered, updated_ms
		FROM assistant_models WHERE provider_id = ? ORDER BY lower(model_id), model_id
	`, provider.ID)
	if err != nil {
		return fmt.Errorf("list assistant models: %w", err)
	}
	defer rows.Close()
	provider.Models = make([]AssistantModel, 0)
	for rows.Next() {
		var model AssistantModel
		var verified, discovered int
		if err := rows.Scan(&model.ID, &model.ContextLimit, &verified, &discovered, &model.UpdatedMS); err != nil {
			return err
		}
		model.Verified = verified != 0
		model.Discovered = discovered != 0
		provider.Models = append(provider.Models, model)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	err = s.db.QueryRowContext(ctx, `
		SELECT status, last_attempt_ms, last_success_ms, error
		FROM assistant_catalog_state WHERE provider_id = ?
	`, provider.ID).Scan(&provider.Catalog.Status, &provider.Catalog.LastAttemptMS, &provider.Catalog.LastSuccessMS, &provider.Catalog.Error)
	if errors.Is(err, sql.ErrNoRows) {
		provider.Catalog.Status = "never"
		return nil
	}
	return err
}

func (s *Store) CreateAssistantProvider(ctx context.Context, input CreateAssistantProviderInput) (AssistantProvider, error) {
	name, baseURL, apiKey, err := normalizeAssistantProviderInput(input.Name, input.BaseURL, input.APIKey)
	if err != nil {
		return AssistantProvider{}, err
	}
	id, err := newAssistantProviderID()
	if err != nil {
		return AssistantProvider{}, err
	}
	now := time.Now().UnixMilli()
	s.write.Lock()
	defer s.write.Unlock()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO assistant_providers(provider_id, name, base_url, api_key, insecure_transport_ack, created_ms, updated_ms)
		VALUES(?, ?, ?, ?, ?, ?, ?)
	`, id, name, baseURL, apiKey, boolInt(input.InsecureTransportAck), now, now)
	if err != nil {
		return AssistantProvider{}, fmt.Errorf("create assistant provider: %w", err)
	}
	return s.GetAssistantProvider(ctx, id)
}

func (s *Store) UpdateAssistantProvider(ctx context.Context, id string, input UpdateAssistantProviderInput) (AssistantProvider, error) {
	current, err := s.GetAssistantProvider(ctx, id)
	if err != nil {
		return AssistantProvider{}, err
	}
	name, baseURL, apiKey := current.Name, current.BaseURL, current.APIKey
	insecure := current.InsecureTransportAck
	if input.Name != nil {
		name = *input.Name
	}
	if input.BaseURL != nil {
		baseURL = *input.BaseURL
	}
	if input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "" {
		apiKey = *input.APIKey
	}
	if input.ClearAPIKey {
		apiKey = ""
	}
	if input.InsecureTransportAck != nil {
		insecure = *input.InsecureTransportAck
	}
	name, baseURL, apiKey, err = normalizeAssistantProviderInput(name, baseURL, apiKey)
	if err != nil {
		return AssistantProvider{}, err
	}
	keyChanged := apiKey != current.APIKey
	endpointChanged := baseURL != current.BaseURL || keyChanged
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AssistantProvider{}, err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `
		UPDATE assistant_providers SET name = ?, base_url = ?, api_key = ?, insecure_transport_ack = ?, updated_ms = ?
		WHERE provider_id = ?
	`, name, baseURL, apiKey, boolInt(insecure), now, id)
	if err != nil {
		return AssistantProvider{}, fmt.Errorf("update assistant provider: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return AssistantProvider{}, ErrProviderNotFound
	}
	if endpointChanged {
		_, err = tx.ExecContext(ctx, `
			UPDATE assistant_catalog_state SET status = 'stale', error = '' WHERE provider_id = ?
		`, id)
		if err != nil {
			return AssistantProvider{}, err
		}
		if err := bumpSelectionRevisionTx(ctx, tx, id, now); err != nil {
			return AssistantProvider{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return AssistantProvider{}, err
	}
	if keyChanged {
		s.checkpointSecrets(ctx)
	}
	return s.GetAssistantProvider(ctx, id)
}

func (s *Store) DeleteAssistantProvider(ctx context.Context, id string) error {
	id, err := assistantIdentifier("provider_id", id, MaxAssistantProviderIDLength)
	if err != nil {
		return err
	}
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `DELETE FROM assistant_providers WHERE provider_id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete assistant provider: %w", err)
	}
	if affected, _ := result.RowsAffected(); affected == 0 {
		return ErrProviderNotFound
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE assistant_selection
		SET provider_id = '', model_id = '', revision = revision + 1, updated_ms = ?
		WHERE singleton = 1 AND provider_id = ?
	`, now, id); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.checkpointSecrets(ctx)
	return nil
}

// ReplaceAssistantCatalog records one successful discovery atomically. A
// failed discovery uses RecordAssistantCatalogFailure and never erases this
// last-good model list.
func (s *Store) ReplaceAssistantCatalog(ctx context.Context, providerID string, models []AssistantModel) error {
	providerID, err := assistantIdentifier("provider_id", providerID, MaxAssistantProviderIDLength)
	if err != nil {
		return err
	}
	if len(models) > 2000 {
		return fmt.Errorf("%w: model catalog exceeds 2000 entries", ErrInvalidProvider)
	}
	normalized := make([]AssistantModel, 0, len(models))
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		id, err := assistantIdentifier("model_id", model.ID, MaxAssistantModelIDLength)
		if err != nil {
			return err
		}
		if model.ContextLimit < 0 || model.ContextLimit > MaxAssistantContextLimit {
			return fmt.Errorf("%w: invalid context limit", ErrInvalidProvider)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		model.ID, model.Verified, model.Discovered = id, true, true
		normalized = append(normalized, model)
	}
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assistant_providers WHERE provider_id = ?`, providerID).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrProviderNotFound
	}
	now := time.Now().UnixMilli()
	// Manual-only rows survive refresh. A discovered row with a configured
	// context limit is demoted to an unverified manual fallback first, so a
	// user's context override is not lost if a later catalog omits that model.
	if _, err := tx.ExecContext(ctx, `
		UPDATE assistant_models SET discovered = 0, verified = 0, updated_ms = ?
		WHERE provider_id = ? AND discovered = 1 AND context_limit > 0
	`, now, providerID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM assistant_models WHERE provider_id = ? AND discovered = 1`, providerID); err != nil {
		return err
	}
	for _, model := range normalized {
		_, err := tx.ExecContext(ctx, `
			INSERT INTO assistant_models(provider_id, model_id, context_limit, verified, discovered, updated_ms)
			VALUES(?, ?, ?, 1, 1, ?)
			ON CONFLICT(provider_id, model_id) DO UPDATE SET
				context_limit = CASE WHEN excluded.context_limit > 0 THEN excluded.context_limit ELSE assistant_models.context_limit END,
				verified = 1, discovered = 1, updated_ms = excluded.updated_ms
		`, providerID, model.ID, model.ContextLimit, now)
		if err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO assistant_catalog_state(provider_id, status, last_attempt_ms, last_success_ms, error)
		VALUES(?, 'ready', ?, ?, '')
		ON CONFLICT(provider_id) DO UPDATE SET status = 'ready', last_attempt_ms = excluded.last_attempt_ms,
			last_success_ms = excluded.last_success_ms, error = ''
	`, providerID, now, now)
	if err != nil {
		return err
	}
	if err := bumpSelectionRevisionTx(ctx, tx, providerID, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) RecordAssistantCatalogFailure(ctx context.Context, providerID, safeError string) error {
	providerID, err := assistantIdentifier("provider_id", providerID, MaxAssistantProviderIDLength)
	if err != nil {
		return err
	}
	safeError = strings.TrimSpace(safeError)
	if utf8.RuneCountInString(safeError) > 300 {
		safeError = string([]rune(safeError)[:300])
	}
	now := time.Now().UnixMilli()
	s.write.Lock()
	defer s.write.Unlock()
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO assistant_catalog_state(provider_id, status, last_attempt_ms, last_success_ms, error)
		VALUES(?, 'error', ?, 0, ?)
		ON CONFLICT(provider_id) DO UPDATE SET status = 'error', last_attempt_ms = excluded.last_attempt_ms, error = excluded.error
	`, providerID, now, safeError)
	return err
}

func (s *Store) PutAssistantModel(ctx context.Context, providerID string, model AssistantModel) (AssistantModel, error) {
	providerID, err := assistantIdentifier("provider_id", providerID, MaxAssistantProviderIDLength)
	if err != nil {
		return AssistantModel{}, err
	}
	model.ID, err = assistantIdentifier("model_id", model.ID, MaxAssistantModelIDLength)
	if err != nil {
		return AssistantModel{}, err
	}
	if model.ContextLimit < 1024 || model.ContextLimit > MaxAssistantContextLimit {
		return AssistantModel{}, fmt.Errorf("%w: context_limit must be between 1024 and %d", ErrInvalidProvider, MaxAssistantContextLimit)
	}
	s.write.Lock()
	defer s.write.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return AssistantModel{}, err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM assistant_providers WHERE provider_id = ?`, providerID).Scan(&exists); err != nil {
		return AssistantModel{}, err
	}
	if exists == 0 {
		return AssistantModel{}, ErrProviderNotFound
	}
	now := time.Now().UnixMilli()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO assistant_models(provider_id, model_id, context_limit, verified, discovered, updated_ms)
		VALUES(?, ?, ?, ?, 0, ?)
		ON CONFLICT(provider_id, model_id) DO UPDATE SET context_limit = excluded.context_limit,
			verified = CASE WHEN assistant_models.discovered = 1 THEN assistant_models.verified ELSE excluded.verified END,
			updated_ms = excluded.updated_ms
	`, providerID, model.ID, model.ContextLimit, boolInt(model.Verified), now)
	if err != nil {
		return AssistantModel{}, err
	}
	if err := bumpSelectionRevisionTx(ctx, tx, providerID, now); err != nil {
		return AssistantModel{}, err
	}
	if err := tx.Commit(); err != nil {
		return AssistantModel{}, err
	}
	model.Discovered = false
	model.UpdatedMS = now
	return model, nil
}

func (s *Store) AssistantSelection(ctx context.Context) (AssistantSelection, error) {
	var selection AssistantSelection
	err := s.db.QueryRowContext(ctx, `
		SELECT provider_id, model_id, revision, updated_ms FROM assistant_selection WHERE singleton = 1
	`).Scan(&selection.ProviderID, &selection.ModelID, &selection.Revision, &selection.UpdatedMS)
	return selection, err
}

func (s *Store) SetAssistantSelection(ctx context.Context, providerID, modelID string) (AssistantSelection, error) {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	if (providerID == "") != (modelID == "") {
		return AssistantSelection{}, ErrInvalidSelection
	}
	if providerID != "" {
		var err error
		providerID, err = assistantIdentifier("provider_id", providerID, MaxAssistantProviderIDLength)
		if err != nil {
			return AssistantSelection{}, err
		}
		modelID, err = assistantIdentifier("model_id", modelID, MaxAssistantModelIDLength)
		if err != nil {
			return AssistantSelection{}, err
		}
	}
	s.write.Lock()
	defer s.write.Unlock()
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		UPDATE assistant_selection SET provider_id = ?, model_id = ?, revision = revision + 1, updated_ms = ?
		WHERE singleton = 1
	`, providerID, modelID, now)
	if err != nil {
		return AssistantSelection{}, err
	}
	return s.AssistantSelection(ctx)
}

func bumpSelectionRevisionTx(ctx context.Context, tx *sql.Tx, providerID string, now int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE assistant_selection SET revision = revision + 1, updated_ms = ?
		WHERE singleton = 1 AND provider_id = ?
	`, now, providerID)
	return err
}

func (s *Store) checkpointSecrets(ctx context.Context) {
	checkpointCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	_, _ = s.db.ExecContext(checkpointCtx, `PRAGMA wal_checkpoint(TRUNCATE)`)
}

func normalizeAssistantProviderInput(name, baseURL, apiKey string) (string, string, string, error) {
	var err error
	name, err = assistantIdentifier("name", name, MaxAssistantProviderNameLength)
	if err != nil {
		return "", "", "", err
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" || utf8.RuneCountInString(baseURL) > MaxAssistantBaseURLLength {
		return "", "", "", fmt.Errorf("%w: invalid base_url", ErrInvalidProvider)
	}
	apiKey = strings.TrimSpace(apiKey)
	if len(apiKey) > MaxAssistantAPIKeyLength {
		return "", "", "", fmt.Errorf("%w: api_key is too long", ErrInvalidProvider)
	}
	return name, baseURL, apiKey, nil
}

func assistantIdentifier(field, value string, limit int) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || utf8.RuneCountInString(value) > limit || strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("%w: invalid %s", ErrInvalidProvider, field)
	}
	return value, nil
}

func newAssistantProviderID() (string, error) {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "custom_" + hex.EncodeToString(raw[:]), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
