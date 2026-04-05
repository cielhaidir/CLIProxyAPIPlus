package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfigOptional_ClientAPIKeysLegacyMigration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("api-keys:\n  - legacy-key-1\n  - legacy-key-2\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	cfg, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("LoadConfigOptional() error = %v", err)
	}
	if len(cfg.APIKeys) != 2 {
		t.Fatalf("expected 2 api keys, got %d", len(cfg.APIKeys))
	}
	if cfg.APIKeys[0].Key != "legacy-key-1" {
		t.Fatalf("expected first key to be migrated, got %#v", cfg.APIKeys[0])
	}
	if cfg.APIKeys[0].Enabled == nil || !*cfg.APIKeys[0].Enabled {
		t.Fatalf("expected migrated key to be enabled, got %#v", cfg.APIKeys[0].Enabled)
	}
	if cfg.APIKeys[0].Currency != "USD" {
		t.Fatalf("expected migrated key currency USD, got %q", cfg.APIKeys[0].Currency)
	}
	if len(cfg.APIKeys[0].AllowedModels) != 0 {
		t.Fatalf("expected empty allowed models, got %#v", cfg.APIKeys[0].AllowedModels)
	}
}

func TestSaveConfigPreserveComments_ClientAPIKeyObjects(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := []byte("# config\napi-keys:\n  - old-key\n")
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		t.Fatalf("failed to write config: %v", err)
	}

	enabled := true
	cfg := &Config{}
	cfg.APIKeys = []ClientAPIKey{{
		Key:           "client-key-1",
		Name:          "Team A",
		Enabled:       &enabled,
		Currency:      "USD",
		CreditBalance: 2500,
		AllowedModels: []string{"gpt-4.1", "claude-sonnet-4-5"},
	}}

	if err := SaveConfigPreserveComments(configPath, cfg); err != nil {
		t.Fatalf("SaveConfigPreserveComments() error = %v", err)
	}

	reloaded, err := LoadConfigOptional(configPath, false)
	if err != nil {
		t.Fatalf("reloading config failed: %v", err)
	}
	if len(reloaded.APIKeys) != 1 {
		t.Fatalf("expected 1 api key, got %d", len(reloaded.APIKeys))
	}
	if reloaded.APIKeys[0].Name != "Team A" {
		t.Fatalf("expected object schema to persist, got %#v", reloaded.APIKeys[0])
	}
	if len(reloaded.ModelPricing) != 0 {
		t.Fatalf("expected no model pricing entries, got %#v", reloaded.ModelPricing)
	}
}
