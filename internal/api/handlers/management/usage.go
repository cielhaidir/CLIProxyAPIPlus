package management

import (
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type usageExportPayload struct {
	Version    int                      `json:"version"`
	ExportedAt time.Time                `json:"exported_at"`
	Usage      usage.StatisticsSnapshot `json:"usage"`
}

type usageImportPayload struct {
	Version int                      `json:"version"`
	Usage   usage.StatisticsSnapshot `json:"usage"`
}

type usageQuery struct {
	APIKey              string
	ClientAPIKeyName    string
	From                time.Time
	To                  time.Time
	DetailLimit         int
	DetailOffset        int
	SortDesc            bool
	FilterApplied       bool
	MatchedClientAPIKey string
}

type usageDetailMatch struct {
	APIKey     string
	ModelName  string
	Detail     usage.RequestDetail
	Timestamp  time.Time
	TimestampN int64
}

const (
	defaultUsageDetailLimit = 500
	maxUsageDetailLimit     = 5000
)

// GetUsageStatistics returns the in-memory request statistics snapshot.
func (h *Handler) GetUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}

	query, err := parseUsageQuery(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if query.FilterApplied {
		filtered, matched, returned := filterUsageSnapshot(snapshot, query)
		c.JSON(http.StatusOK, gin.H{
			"usage":           filtered,
			"failed_requests": filtered.FailureCount,
			"details-pagination": gin.H{
				"offset":   query.DetailOffset,
				"limit":    query.DetailLimit,
				"matched":  matched,
				"returned": returned,
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"usage":           snapshot,
		"failed_requests": snapshot.FailureCount,
	})
}

// ExportUsageStatistics returns a complete usage snapshot for backup/migration.
func (h *Handler) ExportUsageStatistics(c *gin.Context) {
	var snapshot usage.StatisticsSnapshot
	if h != nil && h.usageStats != nil {
		snapshot = h.usageStats.Snapshot()
	}
	c.JSON(http.StatusOK, usageExportPayload{
		Version:    1,
		ExportedAt: time.Now().UTC(),
		Usage:      snapshot,
	})
}

// ImportUsageStatistics merges a previously exported usage snapshot into memory.
func (h *Handler) ImportUsageStatistics(c *gin.Context) {
	if h == nil || h.usageStats == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "usage statistics unavailable"})
		return
	}

	data, err := c.GetRawData()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
		return
	}

	var payload usageImportPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json"})
		return
	}
	if payload.Version != 0 && payload.Version != 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported version"})
		return
	}

	result := h.usageStats.MergeSnapshot(payload.Usage)
	snapshot := h.usageStats.Snapshot()
	c.JSON(http.StatusOK, gin.H{
		"added":           result.Added,
		"skipped":         result.Skipped,
		"total_requests":  snapshot.TotalRequests,
		"failed_requests": snapshot.FailureCount,
	})
}

func resolveManagementHandlerFromContext(c *gin.Context) *Handler {
	if c == nil {
		return nil
	}
	if value, ok := c.Get("management_handler"); ok {
		if handler, ok := value.(*Handler); ok {
			return handler
		}
	}
	return nil
}

func parseUsageQuery(c *gin.Context) (usageQuery, error) {
	query := usageQuery{SortDesc: true}
	if c == nil {
		return query, nil
	}

	if value := strings.TrimSpace(c.Query("api_key")); value != "" {
		query.APIKey = value
		query.FilterApplied = true
	}
	if value := strings.TrimSpace(c.Query("client_api_key_name")); value != "" {
		query.ClientAPIKeyName = value
		query.FilterApplied = true
	}
	if value := strings.TrimSpace(c.Query("sort")); value != "" {
		query.FilterApplied = true
		query.SortDesc = !strings.EqualFold(value, "asc")
	}

	if value := strings.TrimSpace(c.Query("from")); value != "" {
		parsed, err := parseUsageTimeParam(value)
		if err != nil {
			return query, err
		}
		query.From = parsed
		query.FilterApplied = true
	}
	if value := strings.TrimSpace(c.Query("to")); value != "" {
		parsed, err := parseUsageTimeParam(value)
		if err != nil {
			return query, err
		}
		query.To = parsed
		query.FilterApplied = true
	}
	if !query.From.IsZero() && !query.To.IsZero() && query.From.After(query.To) {
		return query, &usageQueryError{message: "from must be earlier than or equal to to"}
	}
	if query.ClientAPIKeyName != "" {
		if h := resolveManagementHandlerFromContext(c); h != nil && h.cfg != nil {
			matchedKey := ""
			for _, entry := range h.cfg.APIKeys {
				if strings.EqualFold(strings.TrimSpace(entry.Name), query.ClientAPIKeyName) {
					if matchedKey != "" && matchedKey != strings.TrimSpace(entry.Key) {
						return query, &usageQueryError{message: "client_api_key_name is ambiguous"}
					}
					matchedKey = strings.TrimSpace(entry.Key)
				}
			}
			if matchedKey == "" {
				query.APIKey = "__no_matching_client_api_key__"
			} else {
				query.APIKey = matchedKey
				query.MatchedClientAPIKey = matchedKey
			}
		}
	}

	limitRaw := strings.TrimSpace(c.Query("detail_limit"))
	offsetRaw := strings.TrimSpace(c.Query("detail_offset"))
	if limitRaw != "" || offsetRaw != "" {
		query.FilterApplied = true
	}
	if limitRaw != "" {
		limit, err := strconv.Atoi(limitRaw)
		if err != nil || limit < 0 {
			return query, &usageQueryError{message: "detail_limit must be a non-negative integer"}
		}
		if limit > maxUsageDetailLimit {
			limit = maxUsageDetailLimit
		}
		query.DetailLimit = limit
	}
	if offsetRaw != "" {
		offset, err := strconv.Atoi(offsetRaw)
		if err != nil || offset < 0 {
			return query, &usageQueryError{message: "detail_offset must be a non-negative integer"}
		}
		query.DetailOffset = offset
	}

	if query.FilterApplied && query.DetailLimit == 0 {
		query.DetailLimit = defaultUsageDetailLimit
	}
	return query, nil
}

type usageQueryError struct{ message string }

func (e *usageQueryError) Error() string { return e.message }

func parseUsageTimeParam(raw string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, nil
	}
	if unixValue, err := strconv.ParseInt(value, 10, 64); err == nil {
		if unixValue <= 0 {
			return time.Time{}, &usageQueryError{message: "time values must be positive"}
		}
		if len(value) >= 13 {
			return time.UnixMilli(unixValue), nil
		}
		return time.Unix(unixValue, 0), nil
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, &usageQueryError{message: "time values must be RFC3339 or unix timestamps"}
	}
	return parsed, nil
}

func filterUsageSnapshot(snapshot usage.StatisticsSnapshot, query usageQuery) (usage.StatisticsSnapshot, int, int) {
	if !query.FilterApplied {
		return snapshot, 0, 0
	}

	result := usage.StatisticsSnapshot{
		APIs:           map[string]usage.APISnapshot{},
		RequestsByDay:  map[string]int64{},
		RequestsByHour: map[string]int64{},
		TokensByDay:    map[string]int64{},
		TokensByHour:   map[string]int64{},
	}

	matches := make([]usageDetailMatch, 0)
	for apiKey, apiSnapshot := range snapshot.APIs {
		if query.APIKey != "" && apiKey != query.APIKey {
			continue
		}
		for modelName, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				timestamp := detail.Timestamp
				if !query.From.IsZero() && timestamp.Before(query.From) {
					continue
				}
				if !query.To.IsZero() && timestamp.After(query.To) {
					continue
				}
				matches = append(matches, usageDetailMatch{
					APIKey:     apiKey,
					ModelName:  modelName,
					Detail:     detail,
					Timestamp:  timestamp,
					TimestampN: timestamp.UnixNano(),
				})
			}
		}
	}

	sort.Slice(matches, func(i, j int) bool {
		if matches[i].TimestampN == matches[j].TimestampN {
			if matches[i].APIKey == matches[j].APIKey {
				return matches[i].ModelName < matches[j].ModelName
			}
			return matches[i].APIKey < matches[j].APIKey
		}
		if query.SortDesc {
			return matches[i].TimestampN > matches[j].TimestampN
		}
		return matches[i].TimestampN < matches[j].TimestampN
	})

	matched := len(matches)
	if query.DetailOffset >= matched {
		return result, matched, 0
	}
	selected := matches[query.DetailOffset:]
	if query.DetailLimit > 0 && len(selected) > query.DetailLimit {
		selected = selected[:query.DetailLimit]
	}

	for _, match := range selected {
		result.TotalRequests++
		if match.Detail.Failed {
			result.FailureCount++
		} else {
			result.SuccessCount++
		}
		totalTokens := match.Detail.Tokens.TotalTokens
		result.TotalTokens += totalTokens

		apiSnapshot := result.APIs[match.APIKey]
		if apiSnapshot.Models == nil {
			apiSnapshot.Models = map[string]usage.ModelSnapshot{}
		}
		apiSnapshot.TotalRequests++
		apiSnapshot.TotalTokens += totalTokens

		modelSnapshot := apiSnapshot.Models[match.ModelName]
		modelSnapshot.TotalRequests++
		modelSnapshot.TotalTokens += totalTokens
		modelSnapshot.Details = append(modelSnapshot.Details, match.Detail)
		apiSnapshot.Models[match.ModelName] = modelSnapshot
		result.APIs[match.APIKey] = apiSnapshot

		dayKey := match.Timestamp.Format("2006-01-02")
		hourKey := match.Timestamp.Format("15")
		result.RequestsByDay[dayKey]++
		result.RequestsByHour[hourKey]++
		result.TokensByDay[dayKey] += totalTokens
		result.TokensByHour[hourKey] += totalTokens
	}

	return result, matched, len(selected)
}
