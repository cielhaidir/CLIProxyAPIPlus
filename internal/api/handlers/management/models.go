package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
)

// GetModelsCatalog returns the full admin-visible merged model catalog without client API key filtering.
func (h *Handler) GetModelsCatalog(c *gin.Context) {
	models := registry.GetGlobalRegistry().GetAvailableModels("openai")
	filteredModels := make([]map[string]any, len(models))
	for i, model := range models {
		filteredModel := map[string]any{
			"id":     model["id"],
			"object": model["object"],
		}
		if created, exists := model["created"]; exists {
			filteredModel["created"] = created
		}
		if ownedBy, exists := model["owned_by"]; exists {
			filteredModel["owned_by"] = ownedBy
		}
		if displayName, exists := model["display_name"]; exists {
			filteredModel["display_name"] = displayName
		}
		if description, exists := model["description"]; exists {
			filteredModel["description"] = description
		}
		filteredModels[i] = filteredModel
	}
	c.JSON(http.StatusOK, gin.H{
		"object": "list",
		"data":   filteredModels,
	})
}
