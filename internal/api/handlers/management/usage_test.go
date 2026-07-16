package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestGetUsageStatisticsWithFilteredDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usage.NewRequestStatistics()
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := usage.StatisticsSnapshot{
		APIs: map[string]usage.APISnapshot{
			"global-key": {
				Models: map[string]usage.ModelSnapshot{
					"model-a": {
						Details: []usage.RequestDetail{
							{Timestamp: now.Add(-2 * time.Hour), Tokens: usage.TokenStats{InputTokens: 1, OutputTokens: 2, TotalTokens: 3}},
							{Timestamp: now.Add(-1 * time.Hour), Failed: true, Tokens: usage.TokenStats{InputTokens: 2, OutputTokens: 3, TotalTokens: 5}},
						},
					},
				},
			},
			"client-key": {
				Models: map[string]usage.ModelSnapshot{
					"model-b": {
						Details: []usage.RequestDetail{{Timestamp: now.Add(-30 * time.Minute), Tokens: usage.TokenStats{InputTokens: 4, OutputTokens: 5, TotalTokens: 9}}},
					},
				},
			},
		},
	}
	stats.MergeSnapshot(snapshot)

	enabled := true
	h := &Handler{usageStats: stats, cfg: &config.Config{SDKConfig: config.SDKConfig{APIKeys: []config.ClientAPIKey{{Key: "client-key", Name: "Team A", Enabled: &enabled, Currency: "USD"}}}}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("management_handler", h)
	from := now.Add(-90 * time.Minute).Format(time.RFC3339)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage?api_key=global-key&from="+from+"&detail_limit=1", nil)

	h.GetUsageStatistics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		Usage struct {
			TotalRequests int64                        `json:"total_requests"`
			FailureCount  int64                        `json:"failure_count"`
			APIs          map[string]usage.APISnapshot `json:"apis"`
		} `json:"usage"`
		DetailsPagination map[string]int `json:"details-pagination"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}

	if body.Usage.TotalRequests != 1 {
		t.Fatalf("total requests = %d, want 1", body.Usage.TotalRequests)
	}
	if body.Usage.FailureCount != 1 {
		t.Fatalf("failure count = %d, want 1", body.Usage.FailureCount)
	}
	if len(body.Usage.APIs) != 1 {
		t.Fatalf("apis len = %d, want 1", len(body.Usage.APIs))
	}
	apiSnapshot, ok := body.Usage.APIs["global-key"]
	if !ok {
		t.Fatalf("filtered response missing api key")
	}
	modelSnapshot := apiSnapshot.Models["model-a"]
	if len(modelSnapshot.Details) != 1 {
		t.Fatalf("details len = %d, want 1", len(modelSnapshot.Details))
	}
	if !modelSnapshot.Details[0].Failed {
		t.Fatal("expected most recent filtered detail to be failed")
	}
	if body.DetailsPagination["matched"] != 1 {
		t.Fatalf("matched = %d, want 1", body.DetailsPagination["matched"])
	}
	if body.DetailsPagination["returned"] != 1 {
		t.Fatalf("returned = %d, want 1", body.DetailsPagination["returned"])
	}
}

func TestGetUsageStatisticsFiltersByClientAPIKeyName(t *testing.T) {
	gin.SetMode(gin.TestMode)

	stats := usage.NewRequestStatistics()
	now := time.Now().UTC().Truncate(time.Second)
	snapshot := usage.StatisticsSnapshot{
		APIs: map[string]usage.APISnapshot{
			"client-key": {
				Models: map[string]usage.ModelSnapshot{
					"model-a": {Details: []usage.RequestDetail{{Timestamp: now, Tokens: usage.TokenStats{TotalTokens: 3}}}},
				},
			},
			"other-key": {
				Models: map[string]usage.ModelSnapshot{
					"model-b": {Details: []usage.RequestDetail{{Timestamp: now, Tokens: usage.TokenStats{TotalTokens: 5}}}},
				},
			},
		},
	}
	stats.MergeSnapshot(snapshot)
	enabled := true
	h := &Handler{usageStats: stats, cfg: &config.Config{SDKConfig: config.SDKConfig{APIKeys: []config.ClientAPIKey{{Key: "client-key", Name: "Team A", Enabled: &enabled, Currency: "USD"}}}}}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set("management_handler", h)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage?client_api_key_name=Team%20A", nil)

	h.GetUsageStatistics(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var body struct {
		Usage struct {
			APIs map[string]usage.APISnapshot `json:"apis"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal error = %v", err)
	}
	if len(body.Usage.APIs) != 1 {
		t.Fatalf("apis len = %d, want 1", len(body.Usage.APIs))
	}
	if _, ok := body.Usage.APIs["client-key"]; !ok {
		t.Fatalf("filtered response missing matched client key: %#v", body.Usage.APIs)
	}
}

func TestGetUsageStatisticsRejectsInvalidDetailLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handler{usageStats: usage.NewRequestStatistics()}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/usage?detail_limit=-1", nil)

	h.GetUsageStatistics(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}
