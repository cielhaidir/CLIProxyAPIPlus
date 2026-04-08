package billing

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/manageddb"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestManagerTopUpAndAdjust(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []config.ClientAPIKey{{Key: "k1", Enabled: &enabled, Currency: "USD"}},
		},
	}
	mgr := NewManager(cfg, filepath.Join(t.TempDir(), "config.yaml"))
	if _, err := mgr.TopUp("k1", 1500, "admin", "seed"); err != nil {
		t.Fatalf("TopUp() error = %v", err)
	}
	if _, err := mgr.Adjust("k1", -200, "admin", "correction"); err != nil {
		t.Fatalf("Adjust() error = %v", err)
	}
	if cfg.APIKeys[0].CreditBalance != 1300 {
		t.Fatalf("credit balance = %d, want 1300", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.APIKeys[0].TotalTopup != 1500 {
		t.Fatalf("total topup = %d, want 1500", cfg.APIKeys[0].TotalTopup)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}
	if entries[0].Type != "adjustment" || entries[1].Type != "topup" {
		t.Fatalf("unexpected ledger order/types: %#v", entries)
	}
}

func TestManagerUsageDebit(t *testing.T) {
	enabled := true
	pricingEnabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []config.ClientAPIKey{{Key: "k1", Enabled: &enabled, Currency: "USD", CreditBalance: 5000}},
			ModelPricing: []config.ModelPricing{{
				Model:       "gpt-4.1",
				Currency:    "USD",
				InputPrice:  200,
				OutputPrice: 800,
				Enabled:     &pricingEnabled,
			}},
		},
	}
	mgr := NewManager(cfg, filepath.Join(t.TempDir(), "config.yaml"))
	mgr.HandleUsageRecord(context.Background(), coreusage.Record{
		APIKey: "k1",
		Model:  "gpt-4.1",
		Detail: coreusage.Detail{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000},
	})
	if cfg.APIKeys[0].CreditBalance != 4000 {
		t.Fatalf("credit balance = %d, want 4000", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.APIKeys[0].TotalSpent != 1000 {
		t.Fatalf("total spent = %d, want 1000", cfg.APIKeys[0].TotalSpent)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(entries))
	}
	if entries[0].Type != "debit" || entries[0].Amount != -1000 {
		t.Fatalf("unexpected debit entry: %#v", entries[0])
	}
}

func TestManagerUsageDebitUsesResolvedBillingModel(t *testing.T) {
	enabled := true
	pricingEnabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []config.ClientAPIKey{{Key: "k1", Enabled: &enabled, Currency: "USD", CreditBalance: 5000}},
			ModelPricing: []config.ModelPricing{{
				Model:       "gpt-5-4",
				Currency:    "USD",
				InputPrice:  250,
				OutputPrice: 1500,
				Enabled:     &pricingEnabled,
			}},
		},
	}
	mgr := NewManager(cfg, filepath.Join(t.TempDir(), "config.yaml"))
	ginCtx := &gin.Context{}
	ginCtx.Set("billingModel", "gpt-5-4")
	ctx := context.WithValue(context.Background(), "gin", ginCtx)
	mgr.HandleUsageRecord(ctx, coreusage.Record{
		APIKey: "k1",
		Model:  "gpt-5.4",
		Detail: coreusage.Detail{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000},
	})
	if cfg.APIKeys[0].CreditBalance != 3250 {
		t.Fatalf("credit balance = %d, want 3250", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.APIKeys[0].TotalSpent != 1750 {
		t.Fatalf("total spent = %d, want 1750", cfg.APIKeys[0].TotalSpent)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(entries))
	}
	if entries[0].Model != "gpt-5-4" {
		t.Fatalf("ledger model = %q, want billed model", entries[0].Model)
	}
}

func TestManagerTopUpAndDebitWithSQLiteStore(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "runtime.db")
	if err := os.Setenv("SQLITESTORE_PATH", dbPath); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
	defer func() {
		os.Unsetenv("SQLITESTORE_PATH")
		manageddb.ResetDefault()
	}()
	enabled := true
	pricingEnabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys:      []config.ClientAPIKey{{Key: "k1", Enabled: &enabled, Currency: "USD"}},
			ModelPricing: []config.ModelPricing{{Model: "gpt-4.1", Currency: "USD", InputPrice: 200, OutputPrice: 800, Enabled: &pricingEnabled}},
		},
	}
	if err := manageddb.Configure(cfg); err != nil {
		t.Fatalf("manageddb.Configure() error = %v", err)
	}
	mgr := NewManager(cfg, filepath.Join(tmpDir, "config.yaml"))
	if _, err := mgr.TopUp("k1", 1500, "admin", "seed"); err != nil {
		t.Fatalf("TopUp() error = %v", err)
	}
	mgr.HandleUsageRecord(context.Background(), coreusage.Record{
		APIKey: "k1",
		Model:  "gpt-4.1",
		Detail: coreusage.Detail{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000},
	})
	if cfg.APIKeys[0].CreditBalance != 500 {
		t.Fatalf("credit balance = %d, want 500", cfg.APIKeys[0].CreditBalance)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}
	if entries[0].Type != "topup" || entries[1].Type != "debit" {
		t.Fatalf("unexpected sqlite ledger order/types: %#v", entries)
	}
}
