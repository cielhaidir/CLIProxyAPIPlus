package management

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

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

func (h *Handler) GetBillingOverview(c *gin.Context) {
	if h == nil || h.billingManager == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "billing unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"billing": h.billingManager.Overview()})
}
