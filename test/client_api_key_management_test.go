package test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/billing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func newClientKeyTestHandler(t *testing.T) (*management.Handler, string) {
	t.Helper()
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}
	enabled := true
	cfg := &config.Config{
		SDKConfig: config.SDKConfig{
			APIKeys:      []config.ClientAPIKey{{Key: "seed-key", Enabled: &enabled, Currency: "USD"}},
			ModelPricing: []config.ModelPricing{{Model: "gpt-4.1", Currency: "USD", InputPrice: 200}},
		},
	}
	return management.NewHandler(cfg, configPath, nil), configPath
}

func setupClientKeyRouter(h *management.Handler) *gin.Engine {
	r := gin.New()
	mgmt := r.Group("/v0/management")
	{
		mgmt.GET("/api-keys", h.GetAPIKeys)
		mgmt.PATCH("/api-keys", h.PatchAPIKeys)
		mgmt.GET("/client-api-keys", h.GetClientAPIKeys)
		mgmt.POST("/client-api-keys", h.PutClientAPIKeys)
		mgmt.PATCH("/client-api-keys", h.PatchClientAPIKeys)
		mgmt.DELETE("/client-api-keys", h.DeleteClientAPIKeys)
		mgmt.GET("/model-pricing", h.GetModelPricing)
		mgmt.POST("/model-pricing", h.PutModelPricing)
		mgmt.PATCH("/model-pricing", h.PatchModelPricing)
		mgmt.DELETE("/model-pricing", h.DeleteModelPricing)
		mgmt.GET("/client-api-keys/:id/ledger", h.GetClientAPIKeyLedger)
		mgmt.GET("/client-api-keys/:id/activity", h.GetClientAPIKeyActivity)
		mgmt.POST("/client-api-keys/:id/topups", h.PostClientAPIKeyTopup)
		mgmt.POST("/client-api-keys/:id/adjustments", h.PostClientAPIKeyAdjustment)
		mgmt.GET("/client-api-keys/:id/usage", h.GetClientAPIKeyUsage)
		mgmt.GET("/billing/overview", h.GetBillingOverview)
	}
	return r
}

func TestGetAPIKeysLegacyShape(t *testing.T) {
	h, _ := newClientKeyTestHandler(t)
	r := setupClientKeyRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/api-keys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	var resp map[string][]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if len(resp["api-keys"]) != 1 || resp["api-keys"][0] != "seed-key" {
		t.Fatalf("expected legacy string response, got %#v", resp)
	}
}

func TestClientAPIKeysCRUDAndModelPricing(t *testing.T) {
	h, _ := newClientKeyTestHandler(t)
	r := setupClientKeyRouter(h)

	postBody := `{"items":[{"key":"client-key-1","name":"Team A","enabled":true,"currency":"usd","allowed-models":["gpt-4.1","gpt-4.1"],"credit-balance":2500}]}`
	req := httptest.NewRequest(http.MethodPost, "/v0/management/client-api-keys", bytes.NewBufferString(postBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/client-api-keys", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var keysResp map[string][]config.ClientAPIKey
	if err := json.Unmarshal(w.Body.Bytes(), &keysResp); err != nil {
		t.Fatalf("failed to unmarshal client keys response: %v", err)
	}
	if len(keysResp["client-api-keys"]) != 1 {
		t.Fatalf("expected 1 client key, got %#v", keysResp)
	}
	if keysResp["client-api-keys"][0].Currency != "USD" {
		t.Fatalf("expected USD currency normalization, got %#v", keysResp["client-api-keys"][0])
	}
	if len(keysResp["client-api-keys"][0].AllowedModels) != 1 {
		t.Fatalf("expected allowed models deduplication, got %#v", keysResp["client-api-keys"][0].AllowedModels)
	}

	patchBody := `{"match":"client-key-1","value":{"enabled":false,"notes":"  note  "}}`
	req = httptest.NewRequest(http.MethodPatch, "/v0/management/client-api-keys", bytes.NewBufferString(patchBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected patch status 200, got %d body=%s", w.Code, w.Body.String())
	}

	pricingBody := `[{"model":"gpt-4.1","currency":"usd","input-price":200,"output-price":800,"enabled":true}]`
	req = httptest.NewRequest(http.MethodPost, "/v0/management/model-pricing", bytes.NewBufferString(pricingBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected pricing post status 200, got %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/model-pricing", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var pricingResp map[string][]config.ModelPricing
	if err := json.Unmarshal(w.Body.Bytes(), &pricingResp); err != nil {
		t.Fatalf("failed to unmarshal pricing response: %v", err)
	}
	if len(pricingResp["model-pricing"]) != 1 {
		t.Fatalf("expected 1 model pricing entry, got %#v", pricingResp)
	}
	if pricingResp["model-pricing"][0].Currency != "USD" {
		t.Fatalf("expected USD currency normalization, got %#v", pricingResp["model-pricing"][0])
	}
}

func TestClientAPIKeyBillingEndpoints(t *testing.T) {
	h, _ := newClientKeyTestHandler(t)
	r := setupClientKeyRouter(h)

	topupBody := `{"amount":1500,"created-by":"admin","description":"seed"}`
	req := httptest.NewRequest(http.MethodPost, "/v0/management/client-api-keys/seed-key/topups", bytes.NewBufferString(topupBody))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected topup 200, got %d body=%s", w.Code, w.Body.String())
	}

	adjustBody := `{"amount":-200,"created-by":"admin","description":"correction"}`
	req = httptest.NewRequest(http.MethodPost, "/v0/management/client-api-keys/seed-key/adjustments", bytes.NewBufferString(adjustBody))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected adjustment 200, got %d body=%s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/client-api-keys/seed-key/ledger", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var ledgerResp map[string][]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &ledgerResp); err != nil {
		t.Fatalf("failed to unmarshal ledger response: %v", err)
	}
	if len(ledgerResp["ledger"]) != 2 {
		t.Fatalf("expected 2 ledger entries, got %#v", ledgerResp)
	}

	req = httptest.NewRequest(http.MethodGet, "/v0/management/billing/overview", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var overviewResp map[string]map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &overviewResp); err != nil {
		t.Fatalf("failed to unmarshal overview response: %v", err)
	}
	billing := overviewResp["billing"]
	if int64(billing["total-balance"].(float64)) != 1300 {
		t.Fatalf("expected total balance 1300, got %#v", billing)
	}
}

func TestClientAPIKeyActivityEndpoint(t *testing.T) {
	h, configPath := newClientKeyTestHandler(t)
	enabled := true
	cfg := &config.Config{SDKConfig: config.SDKConfig{
		APIKeys:      []config.ClientAPIKey{{Key: "seed-key", Enabled: &enabled, Currency: "USD", CreditBalance: 5000}},
		ModelPricing: []config.ModelPricing{{Model: "gpt-4.1", Currency: "USD", InputPrice: 200, OutputPrice: 800, Enabled: &enabled}},
	}}
	h.SetConfig(cfg)
	bm := billing.NewManager(cfg, configPath)
	h.SetBillingManager(bm)
	bm.HandleUsageRecord(context.Background(), coreusage.Record{
		APIKey: "seed-key",
		Model:  "gpt-4.1",
		Detail: coreusage.Detail{InputTokens: 1000000, OutputTokens: 1000000, TotalTokens: 2000000},
	})
	r := setupClientKeyRouter(h)
	req := httptest.NewRequest(http.MethodGet, "/v0/management/client-api-keys/seed-key/activity?page=1&page_size=10", nil)
	w := httptest.NewRecorder()
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected activity 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Items      []map[string]any `json:"items"`
		Pagination map[string]any   `json:"pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal activity response: %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 activity row, got %#v", resp)
	}
	if int64(resp.Items[0]["amount"].(float64)) >= 0 {
		t.Fatalf("expected debit amount, got %#v", resp.Items[0])
	}
	if int(resp.Pagination["total"].(float64)) != 1 {
		t.Fatalf("expected total 1, got %#v", resp.Pagination)
	}
}
