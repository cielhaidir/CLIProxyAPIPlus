package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteStoreBootstrapSeedsDatabaseFromConfig(t *testing.T) {
	tmpDir := t.TempDir()
	bootstrapPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("port: 8317\napi-keys:\n  - bootstrap-key\n")
	if err := os.WriteFile(bootstrapPath, content, 0o644); err != nil {
		t.Fatalf("write bootstrap config: %v", err)
	}
	store, err := NewSQLiteStore(context.Background(), SQLiteStoreConfig{
		DBPath:   filepath.Join(tmpDir, "runtime.db"),
		SpoolDir: filepath.Join(tmpDir, "spool"),
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Bootstrap(context.Background(), bootstrapPath, ""); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	spooled, err := os.ReadFile(store.ConfigPath())
	if err != nil {
		t.Fatalf("read spooled config: %v", err)
	}
	if string(spooled) != string(content) {
		t.Fatalf("spooled config mismatch\n got: %q\nwant: %q", string(spooled), string(content))
	}
	var stored string
	if err := store.db.QueryRowContext(context.Background(), `SELECT content FROM config_store WHERE id = ?`, defaultConfigKey).Scan(&stored); err != nil {
		t.Fatalf("query config_store: %v", err)
	}
	if stored != string(content) {
		t.Fatalf("stored config mismatch\n got: %q\nwant: %q", stored, string(content))
	}
}

func TestSQLiteStoreBootstrapPrefersDatabaseAfterSeed(t *testing.T) {
	tmpDir := t.TempDir()
	bootstrapPath := filepath.Join(tmpDir, "config.yaml")
	initial := []byte("port: 8317\napi-keys:\n  - first-key\n")
	if err := os.WriteFile(bootstrapPath, initial, 0o644); err != nil {
		t.Fatalf("write bootstrap config: %v", err)
	}
	store, err := NewSQLiteStore(context.Background(), SQLiteStoreConfig{
		DBPath:   filepath.Join(tmpDir, "runtime.db"),
		SpoolDir: filepath.Join(tmpDir, "spool"),
	})
	if err != nil {
		t.Fatalf("NewSQLiteStore() error = %v", err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Bootstrap(context.Background(), bootstrapPath, ""); err != nil {
		t.Fatalf("first Bootstrap() error = %v", err)
	}
	changed := []byte("port: 9999\napi-keys:\n  - changed-key\n")
	if err := os.WriteFile(bootstrapPath, changed, 0o644); err != nil {
		t.Fatalf("overwrite bootstrap config: %v", err)
	}
	if err := store.Bootstrap(context.Background(), bootstrapPath, ""); err != nil {
		t.Fatalf("second Bootstrap() error = %v", err)
	}
	spooled, err := os.ReadFile(store.ConfigPath())
	if err != nil {
		t.Fatalf("read spooled config: %v", err)
	}
	if strings.Contains(string(spooled), "changed-key") {
		t.Fatalf("expected DB-backed config to win over modified YAML, got %q", string(spooled))
	}
	if !strings.Contains(string(spooled), "first-key") {
		t.Fatalf("expected original bootstrapped config, got %q", string(spooled))
	}
}
