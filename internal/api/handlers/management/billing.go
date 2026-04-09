package management

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/managedtypes"
)

type activityRow struct {
	ID           string `json:"id"`
	Time         string `json:"time"`
	Model        string `json:"model"`
	Amount       int64  `json:"amount"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	TotalTokens  int64  `json:"total_tokens"`
	SortUnix     int64  `json:"-"`
}

func (h *Handler) GetClientAPIKeyLedger(c *gin.Context) {
	if h == nil || h.billingManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing unavailable"})
		return
	}
	apiKey := strings.TrimSpace(c.Param("id"))
	c.JSON(http.StatusOK, gin.H{"ledger": h.billingManager.Ledger(apiKey)})
}

func (h *Handler) PostClientAPIKeyTopup(c *gin.Context) {
	if h == nil || h.billingManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing unavailable"})
		return
	}
	var body struct {
		Amount      *int64  `json:"amount"`
		CreatedBy   *string `json:"created-by"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Amount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	createdBy := ""
	if body.CreatedBy != nil {
		createdBy = strings.TrimSpace(*body.CreatedBy)
	}
	description := ""
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
	}
	entry, err := h.billingManager.TopUp(strings.TrimSpace(c.Param("id")), *body.Amount, createdBy, description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"client-api-key": entry})
}

func (h *Handler) PostClientAPIKeyAdjustment(c *gin.Context) {
	if h == nil || h.billingManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing unavailable"})
		return
	}
	var body struct {
		Amount      *int64  `json:"amount"`
		CreatedBy   *string `json:"created-by"`
		Description *string `json:"description"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Amount == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	createdBy := ""
	if body.CreatedBy != nil {
		createdBy = strings.TrimSpace(*body.CreatedBy)
	}
	description := ""
	if body.Description != nil {
		description = strings.TrimSpace(*body.Description)
	}
	entry, err := h.billingManager.Adjust(strings.TrimSpace(c.Param("id")), *body.Amount, createdBy, description)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"client-api-key": entry})
}

func (h *Handler) GetClientAPIKeyUsage(c *gin.Context) {
	apiKey := strings.TrimSpace(c.Param("id"))
	if apiKey == "" || h == nil || h.usageStats == nil {
		c.JSON(http.StatusOK, gin.H{"usage": nil})
		return
	}
	snapshot := h.usageStats.Snapshot()
	if api, ok := snapshot.APIs[apiKey]; ok {
		c.JSON(http.StatusOK, gin.H{"usage": api})
		return
	}
	c.JSON(http.StatusOK, gin.H{"usage": nil})
}

func (h *Handler) GetClientAPIKeyActivity(c *gin.Context) {
	apiKey := strings.TrimSpace(c.Param("id"))
	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing api key"})
		return
	}
	page := 1
	pageSize := 20
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 100 {
			pageSize = parsed
		}
	}
	rows := make([]activityRow, 0)
	if h != nil && h.billingManager != nil {
		for _, entry := range h.billingManager.Ledger(apiKey) {
			if entry.Type == "debit" {
				rows = append(rows, ledgerToActivityRow(entry))
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].SortUnix > rows[j].SortUnix
	})
	total := len(rows)
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page > totalPages {
		page = totalPages
	}
	start := 0
	if total > 0 {
		start = (page - 1) * pageSize
	}
	if start < 0 {
		start = 0
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paged := rows
	if total > 0 {
		paged = rows[start:end]
	}
	c.JSON(http.StatusOK, gin.H{
		"items": paged,
		"pagination": gin.H{
			"page":        page,
			"page_size":   pageSize,
			"total":       total,
			"total_pages": totalPages,
		},
	})
}

func (h *Handler) GetBillingOverview(c *gin.Context) {
	if h == nil || h.billingManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"billing": h.billingManager.Overview()})
}

func ledgerToActivityRow(entry managedtypes.LedgerEntry) activityRow {
	timeValue := strings.TrimSpace(entry.CreatedAt)
	sortUnix := parseTimeUnix(timeValue)
	totalTokens := entry.InputTokens + entry.OutputTokens + entry.ReasoningTokens
	return activityRow{
		ID:           "ledger-" + entry.ID,
		Time:         timeValue,
		Model:        entry.Model,
		Amount:       entry.Amount,
		InputTokens:  entry.InputTokens,
		OutputTokens: entry.OutputTokens,
		TotalTokens:  totalTokens,
		SortUnix:     sortUnix,
	}
}

func parseTimeUnix(value string) int64 {
	if strings.TrimSpace(value) == "" {
		return 0
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return 0
	}
	return parsed.UnixNano()
}
