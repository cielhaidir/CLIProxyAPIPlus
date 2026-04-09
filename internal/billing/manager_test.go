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
	if _, err := mgr.TopUp("k1", 150000, "admin", "seed"); err != nil {
		t.Fatalf("TopUp() error = %v", err)
	}
	if _, err := mgr.Adjust("k1", -20000, "admin", "correction"); err != nil {
		t.Fatalf("Adjust() error = %v", err)
	}
	if cfg.APIKeys[0].CreditBalance != 130000 {
		t.Fatalf("credit balance = %d, want 130000", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.APIKeys[0].TotalTopup != 150000 {
		t.Fatalf("total topup = %d, want 150000", cfg.APIKeys[0].TotalTopup)
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
			APIKeys: []config.ClientAPIKey{{Key: "k1", Enabled: &enabled, Currency: "USD", CreditBalance: 500000}},
			ModelPricing: []config.ModelPricing{{
				Model:       "gpt-4.1",
				Currency:    "USD",
				InputPrice:  20000,
				OutputPrice: 80000,
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
	if cfg.APIKeys[0].CreditBalance != 400000 {
		t.Fatalf("credit balance = %d, want 400000", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.APIKeys[0].TotalSpent != 100000 {
		t.Fatalf("total spent = %d, want 100000", cfg.APIKeys[0].TotalSpent)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(entries))
	}
	if entries[0].Type != "debit" || entries[0].Amount != -100000 {
		t.Fatalf("unexpected debit entry: %#v", entries[0])
	}
}

func TestManagerUsageDebitUsesResolvedBillingModel(t *testing.T) {
	enabled := true
	pricingEnabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys: []config.ClientAPIKey{{Key: "k1", Enabled: &enabled, Currency: "USD", CreditBalance: 500000}},
			ModelPricing: []config.ModelPricing{{
				Model:       "gpt-5-4",
				Currency:    "USD",
				InputPrice:  25000,
				OutputPrice: 150000,
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
	if cfg.APIKeys[0].CreditBalance != 325000 {
		t.Fatalf("credit balance = %d, want 325000", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.APIKeys[0].TotalSpent != 175000 {
		t.Fatalf("total spent = %d, want 175000", cfg.APIKeys[0].TotalSpent)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 1 {
		t.Fatalf("ledger entries = %d, want 1", len(entries))
	}
	if entries[0].Model != "gpt-5-4" {
		t.Fatalf("ledger model = %q, want billed model", entries[0].Model)
	}
}

func TestCalculateUsageCostRoundsAfterSummingComponents(t *testing.T) {
	pricingEnabled := true
	pricing := &config.ModelPricing{
		Model:       "gpt-5-4",
		Currency:    "USD",
		InputPrice:  10000,
		OutputPrice: 75000,
		Enabled:     &pricingEnabled,
	}

	amount := calculateUsageCost(coreusage.Detail{
		InputTokens:  93834,
		OutputTokens: 192,
	}, pricing)

	if amount != 953 {
		t.Fatalf("amount = %d, want 953", amount)
	}
}

func TestCalculateUsageCostKeepsFourDecimalPrecision(t *testing.T) {
	pricingEnabled := true
	pricing := &config.ModelPricing{
		Model:      "gpt-5-4",
		Currency:   "USD",
		InputPrice: 10000,
		Enabled:    &pricingEnabled,
	}

	amount := calculateUsageCost(coreusage.Detail{
		InputTokens: 4999,
	}, pricing)

	if amount != 50 {
		t.Fatalf("amount = %d, want 50", amount)
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
			ModelPricing: []config.ModelPricing{{Model: "gpt-4.1", Currency: "USD", InputPrice: 20000, OutputPrice: 80000, Enabled: &pricingEnabled}},
		},
	}
	if err := manageddb.Configure(cfg); err != nil {
		t.Fatalf("manageddb.Configure() error = %v", err)
	}
	mgr := NewManager(cfg, filepath.Join(tmpDir, "config.yaml"))
	if _, err := mgr.TopUp("k1", 150000, "admin", "seed"); err != nil {
		t.Fatalf("TopUp() error = %v", err)
	}
	mgr.HandleUsageRecord(context.Background(), coreusage.Record{
		APIKey: "k1",
		Model:  "gpt-4.1",
		Detail: coreusage.Detail{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000},
	})
	if cfg.APIKeys[0].CreditBalance != 50000 {
		t.Fatalf("credit balance = %d, want 50000", cfg.APIKeys[0].CreditBalance)
	}
	entries := mgr.Ledger("k1")
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}
	if entries[0].Type != "topup" || entries[1].Type != "debit" {
		t.Fatalf("unexpected sqlite ledger order/types: %#v", entries)
	}
}

func TestManagerSetConfigMigratesLegacyBillingScale(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			BillingScaleVersion: 1,
			APIKeys: []config.ClientAPIKey{{
				Key:           "k1",
				Enabled:       &enabled,
				Currency:      "USD",
				CreditBalance: 936,
				TotalTopup:    1000,
				TotalSpent:    64,
			}},
			ModelPricing: []config.ModelPricing{{
				Model:       "gpt-5-4",
				Currency:    "USD",
				InputPrice:  100,
				OutputPrice: 750,
				Enabled:     &enabled,
			}},
		},
	}

	mgr := NewManager(cfg, filepath.Join(t.TempDir(), "config.yaml"))

	if cfg.BillingScaleVersion != 2 {
		t.Fatalf("billing scale version = %d, want 2", cfg.BillingScaleVersion)
	}
	if cfg.APIKeys[0].CreditBalance != 93600 {
		t.Fatalf("credit balance = %d, want 93600", cfg.APIKeys[0].CreditBalance)
	}
	if cfg.ModelPricing[0].InputPrice != 10000 {
		t.Fatalf("input price = %d, want 10000", cfg.ModelPricing[0].InputPrice)
	}
	if mgr == nil {
		t.Fatal("expected manager")
	}
}
