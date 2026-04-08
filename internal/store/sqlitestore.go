package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/misc"
	_ "modernc.org/sqlite"
)

type SQLiteStoreConfig struct {
	DBPath   string
	SpoolDir string
}

type SQLiteStore struct {
	db         *sql.DB
	cfg        SQLiteStoreConfig
	spoolRoot  string
	configPath string
	mu         sync.Mutex
}

func NewSQLiteStore(ctx context.Context, cfg SQLiteStoreConfig) (*SQLiteStore, error) {
	dbPath := strings.TrimSpace(cfg.DBPath)
	if dbPath == "" {
		return nil, fmt.Errorf("sqlite store: DB path is required")
	}
	absDBPath, err := filepath.Abs(dbPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite store: resolve DB path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDBPath), 0o700); err != nil {
		return nil, fmt.Errorf("sqlite store: create DB directory: %w", err)
	}

	spoolRoot := strings.TrimSpace(cfg.SpoolDir)
	if spoolRoot == "" {
		spoolRoot = filepath.Join(filepath.Dir(absDBPath), "sqlitestore")
	}
	absSpool, err := filepath.Abs(spoolRoot)
	if err != nil {
		return nil, fmt.Errorf("sqlite store: resolve spool dir: %w", err)
	}
	configDir := filepath.Join(absSpool, "config")
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		return nil, fmt.Errorf("sqlite store: create config spool dir: %w", err)
	}

	db, err := sql.Open("sqlite", absDBPath)
	if err != nil {
		return nil, fmt.Errorf("sqlite store: open DB: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("sqlite store: ping DB: %w", err)
	}

	return &SQLiteStore{
		db:         db,
		cfg:        SQLiteStoreConfig{DBPath: absDBPath, SpoolDir: absSpool},
		spoolRoot:  absSpool,
		configPath: filepath.Join(configDir, "config.yaml"),
	}, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) ConfigPath() string {
	if s == nil {
		return ""
	}
	return s.configPath
}

func (s *SQLiteStore) WorkDir() string {
	if s == nil {
		return ""
	}
	return s.spoolRoot
}

func (s *SQLiteStore) EnsureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("sqlite store: not initialized")
	}
	queries := []string{
		`CREATE TABLE IF NOT EXISTS config_store (
			id TEXT PRIMARY KEY,
			content TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS bootstrap_meta (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			bootstrapped INTEGER NOT NULL,
			source_path TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
	}
	for _, query := range queries {
		if _, err := s.db.ExecContext(ctx, query); err != nil {
			return fmt.Errorf("sqlite store: ensure schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) Bootstrap(ctx context.Context, bootstrapConfigPath, exampleConfigPath string) error {
	if err := s.EnsureSchema(ctx); err != nil {
		return err
	}
	return s.syncConfigFromDatabase(ctx, bootstrapConfigPath, exampleConfigPath)
}

func (s *SQLiteStore) PersistConfig(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("sqlite store: not initialized")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return s.deleteConfigRecord(ctx)
		}
		return fmt.Errorf("sqlite store: read config file: %w", err)
	}
	return s.persistConfig(ctx, data)
}

func (s *SQLiteStore) syncConfigFromDatabase(ctx context.Context, bootstrapConfigPath, exampleConfigPath string) error {
	var content string
	err := s.db.QueryRowContext(ctx, `SELECT content FROM config_store WHERE id = ?`, defaultConfigKey).Scan(&content)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		sourcePath := strings.TrimSpace(bootstrapConfigPath)
		if sourcePath == "" {
			sourcePath = s.configPath
		}
		if _, errStat := os.Stat(sourcePath); errors.Is(errStat, fs.ErrNotExist) {
			if exampleConfigPath != "" {
				if errCopy := misc.CopyConfigTemplate(exampleConfigPath, sourcePath); errCopy != nil {
					return fmt.Errorf("sqlite store: copy example config: %w", errCopy)
				}
			} else {
				if errCreate := os.MkdirAll(filepath.Dir(sourcePath), 0o700); errCreate != nil {
					return fmt.Errorf("sqlite store: prepare config directory: %w", errCreate)
				}
				if errWrite := os.WriteFile(sourcePath, []byte{}, 0o600); errWrite != nil {
					return fmt.Errorf("sqlite store: create empty config: %w", errWrite)
				}
			}
		} else if errStat != nil {
			return fmt.Errorf("sqlite store: stat bootstrap config: %w", errStat)
		}
		data, errRead := os.ReadFile(sourcePath)
		if errRead != nil {
			return fmt.Errorf("sqlite store: read bootstrap config: %w", errRead)
		}
		if errPersist := s.persistConfig(ctx, data); errPersist != nil {
			return errPersist
		}
		if errMeta := s.upsertBootstrapMeta(ctx, sourcePath); errMeta != nil {
			return errMeta
		}
		content = normalizeLineEndings(string(data))
	case err != nil:
		return fmt.Errorf("sqlite store: load config from database: %w", err)
	default:
		content = normalizeLineEndings(content)
	}
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0o700); err != nil {
		return fmt.Errorf("sqlite store: prepare spool config dir: %w", err)
	}
	if err := os.WriteFile(s.configPath, []byte(content), 0o600); err != nil {
		return fmt.Errorf("sqlite store: write spool config: %w", err)
	}
	return nil
}

func (s *SQLiteStore) upsertBootstrapMeta(ctx context.Context, sourcePath string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO bootstrap_meta (id, bootstrapped, source_path, created_at, updated_at)
		VALUES (1, 1, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET bootstrapped = 1, source_path = excluded.source_path, updated_at = excluded.updated_at
	`, sourcePath, now, now)
	if err != nil {
		return fmt.Errorf("sqlite store: upsert bootstrap meta: %w", err)
	}
	return nil
}

func (s *SQLiteStore) persistConfig(ctx context.Context, data []byte) error {
	normalized := normalizeLineEndings(string(data))
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO config_store (id, content, created_at, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET content = excluded.content, updated_at = excluded.updated_at
	`, defaultConfigKey, normalized, now, now)
	if err != nil {
		return fmt.Errorf("sqlite store: upsert config: %w", err)
	}
	return nil
}

func (s *SQLiteStore) deleteConfigRecord(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM config_store WHERE id = ?`, defaultConfigKey)
	if err != nil {
		return fmt.Errorf("sqlite store: delete config record: %w", err)
	}
	return nil
}
