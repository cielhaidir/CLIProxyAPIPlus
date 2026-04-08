package manageddb

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/managedtypes"
)

type Store struct {
	mu sync.Mutex
	db *sql.DB
}

var (
	defaultStore *Store
	defaultMu    sync.Mutex
)

func Enabled() bool {
	path := strings.TrimSpace(os.Getenv("SQLITESTORE_PATH"))
	return path != ""
}

func Default() *Store {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	return defaultStore
}

func ResetDefault() {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultStore != nil && defaultStore.db != nil {
		_ = defaultStore.db.Close()
	}
	defaultStore = nil
}

func Configure(cfg *config.Config) error {
	path := strings.TrimSpace(os.Getenv("SQLITESTORE_PATH"))
	if path == "" {
		return nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", abs)
	if err != nil {
		return err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return err
	}
	s := &Store{db: db}
	if err := s.ensureSchema(); err != nil {
		_ = db.Close()
		return err
	}
	if cfg != nil {
		if err := s.bootstrapManagedData(cfg); err != nil {
			_ = db.Close()
			return err
		}
	}
	defaultMu.Lock()
	if defaultStore != nil && defaultStore.db != nil {
		_ = defaultStore.db.Close()
	}
	defaultStore = s
	defaultMu.Unlock()
	return nil
}

func (s *Store) ensureSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS client_api_keys (
			api_key TEXT PRIMARY KEY,
			name TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			currency TEXT NOT NULL DEFAULT 'USD',
			credit_balance INTEGER NOT NULL DEFAULT 0,
			total_topup INTEGER NOT NULL DEFAULT 0,
			total_spent INTEGER NOT NULL DEFAULT 0,
			notes TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS client_api_key_models (
			api_key TEXT NOT NULL,
			model TEXT NOT NULL,
			PRIMARY KEY (api_key, model)
		)`,
		`CREATE TABLE IF NOT EXISTS model_pricing (
			model TEXT PRIMARY KEY,
			currency TEXT NOT NULL DEFAULT 'USD',
			pricing_type TEXT NOT NULL DEFAULT '',
			input_price INTEGER NOT NULL DEFAULT 0,
			output_price INTEGER NOT NULL DEFAULT 0,
			reasoning_price INTEGER NOT NULL DEFAULT 0,
			cached_input_price INTEGER NOT NULL DEFAULT 0,
			request_price INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS billing_ledger (
			id TEXT PRIMARY KEY,
			api_key TEXT NOT NULL,
			entry_type TEXT NOT NULL,
			amount INTEGER NOT NULL,
			currency TEXT NOT NULL,
			model TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			reasoning_tokens INTEGER NOT NULL DEFAULT 0,
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			created_by TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS usage_statistics (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			snapshot_json TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL DEFAULT ''
		)`,
	}
	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) bootstrapManagedData(cfg *config.Config) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM client_api_keys`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	ctx := context.Background()
	for _, entry := range cfg.APIKeys {
		if err := s.UpsertClientAPIKey(ctx, entry); err != nil {
			return err
		}
	}
	for _, entry := range cfg.ModelPricing {
		if err := s.UpsertModelPricing(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func intToBool(v int) bool { return v != 0 }

func (s *Store) LoadManagedConfig(ctx context.Context) ([]config.ClientAPIKey, []config.ModelPricing, error) {
	keysRows, err := s.db.QueryContext(ctx, `SELECT api_key, name, enabled, currency, credit_balance, total_topup, total_spent, notes, created_at, updated_at FROM client_api_keys ORDER BY api_key`)
	if err != nil {
		return nil, nil, err
	}
	defer keysRows.Close()
	keys := []config.ClientAPIKey{}
	for keysRows.Next() {
		var entry config.ClientAPIKey
		var enabled int
		if err := keysRows.Scan(&entry.Key, &entry.Name, &enabled, &entry.Currency, &entry.CreditBalance, &entry.TotalTopup, &entry.TotalSpent, &entry.Notes, &entry.CreatedAt, &entry.UpdatedAt); err != nil {
			return nil, nil, err
		}
		e := intToBool(enabled)
		entry.Enabled = &e
		modelRows, err := s.db.QueryContext(ctx, `SELECT model FROM client_api_key_models WHERE api_key = ? ORDER BY model`, entry.Key)
		if err != nil {
			return nil, nil, err
		}
		models := []string{}
		for modelRows.Next() {
			var model string
			if err := modelRows.Scan(&model); err != nil {
				_ = modelRows.Close()
				return nil, nil, err
			}
			models = append(models, model)
		}
		_ = modelRows.Close()
		entry.AllowedModels = models
		keys = append(keys, entry)
	}
	pricingRows, err := s.db.QueryContext(ctx, `SELECT model, currency, pricing_type, input_price, output_price, reasoning_price, cached_input_price, request_price, enabled, updated_at FROM model_pricing ORDER BY model`)
	if err != nil {
		return nil, nil, err
	}
	defer pricingRows.Close()
	pricing := []config.ModelPricing{}
	for pricingRows.Next() {
		var entry config.ModelPricing
		var enabled int
		var updatedAt string
		if err := pricingRows.Scan(&entry.Model, &entry.Currency, &entry.PricingType, &entry.InputPrice, &entry.OutputPrice, &entry.ReasoningPrice, &entry.CachedInputPrice, &entry.RequestPrice, &enabled, &updatedAt); err != nil {
			return nil, nil, err
		}
		e := intToBool(enabled)
		entry.Enabled = &e
		pricing = append(pricing, entry)
	}
	return keys, pricing, nil
}

func (s *Store) UpsertClientAPIKey(ctx context.Context, entry config.ClientAPIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Enabled == nil {
		b := true
		entry.Enabled = &b
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `
		INSERT INTO client_api_keys (api_key, name, enabled, currency, credit_balance, total_topup, total_spent, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(api_key) DO UPDATE SET name=excluded.name, enabled=excluded.enabled, currency=excluded.currency, credit_balance=excluded.credit_balance, total_topup=excluded.total_topup, total_spent=excluded.total_spent, notes=excluded.notes, created_at=excluded.created_at, updated_at=excluded.updated_at
	`, entry.Key, entry.Name, boolToInt(*entry.Enabled), defaultCurrency(entry.Currency), entry.CreditBalance, entry.TotalTopup, entry.TotalSpent, entry.Notes, entry.CreatedAt, entry.UpdatedAt); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM client_api_key_models WHERE api_key = ?`, entry.Key); err != nil {
		return err
	}
	for _, model := range entry.AllowedModels {
		if _, err = tx.ExecContext(ctx, `INSERT INTO client_api_key_models (api_key, model) VALUES (?, ?)`, entry.Key, model); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) DeleteClientAPIKey(ctx context.Context, apiKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `DELETE FROM client_api_key_models WHERE api_key = ?`, apiKey); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM client_api_keys WHERE api_key = ?`, apiKey); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpsertModelPricing(ctx context.Context, entry config.ModelPricing) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.Enabled == nil {
		b := true
		entry.Enabled = &b
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_pricing (model, currency, pricing_type, input_price, output_price, reasoning_price, cached_input_price, request_price, enabled, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(model) DO UPDATE SET currency=excluded.currency, pricing_type=excluded.pricing_type, input_price=excluded.input_price, output_price=excluded.output_price, reasoning_price=excluded.reasoning_price, cached_input_price=excluded.cached_input_price, request_price=excluded.request_price, enabled=excluded.enabled, updated_at=excluded.updated_at
	`, entry.Model, defaultCurrency(entry.Currency), entry.PricingType, entry.InputPrice, entry.OutputPrice, entry.ReasoningPrice, entry.CachedInputPrice, entry.RequestPrice, boolToInt(*entry.Enabled), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) DeleteModelPricing(ctx context.Context, model string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_pricing WHERE model = ?`, model)
	return err
}

func (s *Store) AppendLedgerAndApplyBalance(ctx context.Context, entry managedtypes.LedgerEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `INSERT INTO billing_ledger (id, api_key, entry_type, amount, currency, model, request_id, input_tokens, output_tokens, reasoning_tokens, description, created_at, created_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, entry.ID, entry.APIKey, entry.Type, entry.Amount, defaultCurrency(entry.Currency), entry.Model, entry.RequestID, entry.InputTokens, entry.OutputTokens, entry.ReasoningTokens, entry.Description, entry.CreatedAt, entry.CreatedBy); err != nil {
		return err
	}
	if entry.Type == "topup" {
		_, err = tx.ExecContext(ctx, `UPDATE client_api_keys SET credit_balance = credit_balance + ?, total_topup = total_topup + ?, updated_at = ? WHERE api_key = ?`, entry.Amount, entry.Amount, entry.CreatedAt, entry.APIKey)
	} else if entry.Type == "adjustment" {
		_, err = tx.ExecContext(ctx, `UPDATE client_api_keys SET credit_balance = credit_balance + ?, updated_at = ? WHERE api_key = ?`, entry.Amount, entry.CreatedAt, entry.APIKey)
	} else {
		spent := -entry.Amount
		_, err = tx.ExecContext(ctx, `UPDATE client_api_keys SET credit_balance = credit_balance + ?, total_spent = total_spent + ?, updated_at = ? WHERE api_key = ?`, entry.Amount, spent, entry.CreatedAt, entry.APIKey)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Ledger(ctx context.Context, apiKey string) ([]managedtypes.LedgerEntry, error) {
	query := `SELECT id, api_key, entry_type, amount, currency, model, request_id, input_tokens, output_tokens, reasoning_tokens, description, created_at, created_by FROM billing_ledger`
	args := []any{}
	if strings.TrimSpace(apiKey) != "" {
		query += ` WHERE api_key = ?`
		args = append(args, apiKey)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []managedtypes.LedgerEntry{}
	for rows.Next() {
		var entry managedtypes.LedgerEntry
		if err := rows.Scan(&entry.ID, &entry.APIKey, &entry.Type, &entry.Amount, &entry.Currency, &entry.Model, &entry.RequestID, &entry.InputTokens, &entry.OutputTokens, &entry.ReasoningTokens, &entry.Description, &entry.CreatedAt, &entry.CreatedBy); err != nil {
			return nil, err
		}
		out = append(out, entry)
	}
	return out, nil
}

func (s *Store) SaveUsageStatisticsJSON(ctx context.Context, payload []byte) error {
	if s == nil || s.db == nil {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO usage_statistics (id, snapshot_json, updated_at)
		VALUES (1, ?, ?)
		ON CONFLICT(id) DO UPDATE SET snapshot_json=excluded.snapshot_json, updated_at=excluded.updated_at
	`, string(payload), time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) LoadUsageStatisticsJSON(ctx context.Context) ([]byte, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var payload string
	err := s.db.QueryRowContext(ctx, `SELECT snapshot_json FROM usage_statistics WHERE id = 1`).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(payload)
	if trimmed == "" {
		return nil, nil
	}
	return []byte(trimmed), nil
}

func defaultCurrency(value string) string {
	if strings.TrimSpace(value) == "" {
		return "USD"
	}
	return value
}
