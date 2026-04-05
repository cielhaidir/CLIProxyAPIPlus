package billing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

type LedgerEntry struct {
	ID              string `json:"id"`
	APIKey          string `json:"api-key"`
	Type            string `json:"type"`
	Amount          int64  `json:"amount"`
	Currency        string `json:"currency"`
	Model           string `json:"model,omitempty"`
	RequestID       string `json:"request-id,omitempty"`
	InputTokens     int64  `json:"input-tokens,omitempty"`
	OutputTokens    int64  `json:"output-tokens,omitempty"`
	ReasoningTokens int64  `json:"reasoning-tokens,omitempty"`
	Description     string `json:"description,omitempty"`
	CreatedAt       string `json:"created-at"`
	CreatedBy       string `json:"created-by,omitempty"`
}

type ledgerFile struct {
	Entries []LedgerEntry `json:"entries"`
}

type Overview struct {
	Currency      string                `json:"currency"`
	TotalBalance  int64                 `json:"total-balance"`
	TotalTopup    int64                 `json:"total-topup"`
	TotalSpent    int64                 `json:"total-spent"`
	LedgerEntries int                   `json:"ledger-entries"`
	ClientAPIKeys []config.ClientAPIKey `json:"client-api-keys"`
	ModelPricing  []config.ModelPricing `json:"model-pricing"`
}

type Manager struct {
	mu             sync.Mutex
	cfg            *config.Config
	configFilePath string
	ledgerPath     string
	entries        []LedgerEntry
}

var (
	defaultManagerMu sync.Mutex
	defaultManager   *Manager
)

func NewManager(cfg *config.Config, configFilePath string) *Manager {
	m := &Manager{}
	m.SetConfig(cfg, configFilePath)
	return m
}

func DefaultManager() *Manager {
	defaultManagerMu.Lock()
	defer defaultManagerMu.Unlock()
	if defaultManager == nil {
		defaultManager = NewManager(nil, "")
	}
	return defaultManager
}

func (m *Manager) SetConfig(cfg *config.Config, configFilePath string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cfg = cfg
	m.configFilePath = strings.TrimSpace(configFilePath)
	m.ledgerPath = resolveLedgerPath(m.configFilePath)
	if err := m.loadLocked(); err != nil {
		log.Warnf("billing: failed to load ledger: %v", err)
	}
	defaultManagerMu.Lock()
	if defaultManager == nil {
		defaultManager = m
	}
	defaultManagerMu.Unlock()
}

func (m *Manager) Ledger(apiKey string) []LedgerEntry {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	filtered := make([]LedgerEntry, 0, len(m.entries))
	for i := len(m.entries) - 1; i >= 0; i-- {
		if apiKey == "" || strings.EqualFold(m.entries[i].APIKey, apiKey) {
			filtered = append(filtered, m.entries[i])
		}
	}
	return filtered
}

func (m *Manager) Overview() Overview {
	if m == nil {
		return Overview{Currency: "USD"}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	overview := Overview{Currency: "USD", LedgerEntries: len(m.entries)}
	if m.cfg == nil {
		return overview
	}
	overview.ClientAPIKeys = append([]config.ClientAPIKey(nil), m.cfg.APIKeys...)
	overview.ModelPricing = append([]config.ModelPricing(nil), m.cfg.ModelPricing...)
	for _, entry := range m.cfg.APIKeys {
		overview.TotalBalance += entry.CreditBalance
		overview.TotalTopup += entry.TotalTopup
		overview.TotalSpent += entry.TotalSpent
	}
	return overview
}

func (m *Manager) TopUp(apiKey string, amount int64, createdBy, description string) (*config.ClientAPIKey, error) {
	return m.applyManualEntry(apiKey, LedgerEntry{
		Type:        "topup",
		Amount:      amount,
		Currency:    "USD",
		Description: strings.TrimSpace(description),
		CreatedBy:   strings.TrimSpace(createdBy),
	})
}

func (m *Manager) Adjust(apiKey string, amount int64, createdBy, description string) (*config.ClientAPIKey, error) {
	return m.applyManualEntry(apiKey, LedgerEntry{
		Type:        "adjustment",
		Amount:      amount,
		Currency:    "USD",
		Description: strings.TrimSpace(description),
		CreatedBy:   strings.TrimSpace(createdBy),
	})
}

func (m *Manager) HandleUsageRecord(ctx context.Context, record coreusage.Record) {
	if m == nil || record.APIKey == "" || record.Failed {
		return
	}
	amount, pricing, ok := m.calculateDebit(record)
	if !ok {
		return
	}
	if amount <= 0 {
		log.WithField("api_key", record.APIKey).Warn("billing: usage record had no billable amount")
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, clientKey, err := m.prepareUsageDebitLocked(ctx, record, amount, pricing)
	if err != nil {
		log.WithError(err).WithField("api_key", record.APIKey).Warn("billing: failed to apply usage debit")
		return
	}
	clientKey.CreditBalance -= amount
	clientKey.TotalSpent += amount
	setUpdatedAt(clientKey)
	m.entries = append(m.entries, entry)
	if err := m.persistLocked(); err != nil {
		log.WithError(err).WithField("api_key", record.APIKey).Warn("billing: failed to persist usage debit")
	}
}

func (m *Manager) applyManualEntry(apiKey string, entry LedgerEntry) (*config.ClientAPIKey, error) {
	if m == nil {
		return nil, fmt.Errorf("billing manager unavailable")
	}
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("api key is required")
	}
	if entry.Type == "topup" && entry.Amount <= 0 {
		return nil, fmt.Errorf("topup amount must be positive")
	}
	if entry.Type == "adjustment" && entry.Amount == 0 {
		return nil, fmt.Errorf("adjustment amount must be non-zero")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	clientKey, err := m.findClientKeyLocked(apiKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	entry.ID = uuid.NewString()
	entry.APIKey = apiKey
	entry.CreatedAt = now
	if entry.Currency == "" {
		entry.Currency = "USD"
	}
	if entry.CreatedBy == "" {
		entry.CreatedBy = "management-api"
	}
	clientKey.CreditBalance += entry.Amount
	if entry.Type == "topup" {
		clientKey.TotalTopup += entry.Amount
	}
	setUpdatedAt(clientKey)
	m.entries = append(m.entries, entry)
	if err := m.persistLocked(); err != nil {
		return nil, err
	}
	clone := *clientKey
	return &clone, nil
}

func (m *Manager) prepareUsageDebitLocked(ctx context.Context, record coreusage.Record, amount int64, pricing *config.ModelPricing) (LedgerEntry, *config.ClientAPIKey, error) {
	clientKey, err := m.findClientKeyLocked(record.APIKey)
	if err != nil {
		return LedgerEntry{}, nil, err
	}
	entry := LedgerEntry{
		ID:              uuid.NewString(),
		APIKey:          record.APIKey,
		Type:            "debit",
		Amount:          -amount,
		Currency:        pricing.Currency,
		Model:           strings.TrimSpace(record.Model),
		RequestID:       logging.GetRequestID(ctx),
		InputTokens:     record.Detail.InputTokens,
		OutputTokens:    record.Detail.OutputTokens,
		ReasoningTokens: record.Detail.ReasoningTokens,
		CreatedAt:       time.Now().UTC().Format(time.RFC3339),
		CreatedBy:       "usage-billing",
		Description:     fmt.Sprintf("usage debit for model %s", strings.TrimSpace(record.Model)),
	}
	return entry, clientKey, nil
}

func (m *Manager) calculateDebit(record coreusage.Record) (int64, *config.ModelPricing, bool) {
	if m == nil || m.cfg == nil {
		return 0, nil, false
	}
	modelName := strings.TrimPrefix(strings.TrimSpace(record.Model), "models/")
	if modelName == "" {
		return 0, nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	pricing := m.findPricingLocked(modelName)
	if pricing == nil {
		log.WithField("model", modelName).Warn("billing: missing model pricing, skipping debit")
		return 0, nil, false
	}
	if pricing.Enabled != nil && !*pricing.Enabled {
		return 0, nil, false
	}
	amount := calculateUsageCost(record.Detail, pricing)
	return amount, pricing, true
}

func calculateUsageCost(detail coreusage.Detail, pricing *config.ModelPricing) int64 {
	if pricing == nil {
		return 0
	}
	inputTokens := detail.InputTokens
	if detail.CachedTokens > 0 && detail.CachedTokens < inputTokens {
		inputTokens -= detail.CachedTokens
	} else if detail.CachedTokens >= inputTokens {
		inputTokens = 0
	}
	amount := pricing.RequestPrice
	amount += scalePerMillion(inputTokens, pricing.InputPrice)
	amount += scalePerMillion(detail.CachedTokens, pricing.CachedInputPrice)
	amount += scalePerMillion(detail.OutputTokens, pricing.OutputPrice)
	amount += scalePerMillion(detail.ReasoningTokens, pricing.ReasoningPrice)
	return amount
}

func scalePerMillion(tokens, price int64) int64 {
	if tokens <= 0 || price <= 0 {
		return 0
	}
	return (tokens*price + 500000) / 1000000
}

func (m *Manager) findClientKeyLocked(apiKey string) (*config.ClientAPIKey, error) {
	if m.cfg == nil {
		return nil, fmt.Errorf("config unavailable")
	}
	for i := range m.cfg.APIKeys {
		if strings.EqualFold(m.cfg.APIKeys[i].Key, apiKey) {
			return &m.cfg.APIKeys[i], nil
		}
	}
	return nil, fmt.Errorf("client api key not found")
}

func (m *Manager) findPricingLocked(modelName string) *config.ModelPricing {
	if m.cfg == nil {
		return nil
	}
	for i := range m.cfg.ModelPricing {
		if strings.EqualFold(strings.TrimSpace(m.cfg.ModelPricing[i].Model), modelName) {
			return &m.cfg.ModelPricing[i]
		}
	}
	return nil
}

func (m *Manager) loadLocked() error {
	m.entries = nil
	if m.ledgerPath == "" {
		return nil
	}
	data, err := os.ReadFile(m.ledgerPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var file ledgerFile
	if err := json.Unmarshal(data, &file); err != nil {
		return err
	}
	m.entries = append([]LedgerEntry(nil), file.Entries...)
	return nil
}

func (m *Manager) persistLocked() error {
	if err := m.persistConfigLocked(); err != nil {
		return err
	}
	if m.ledgerPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.ledgerPath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(ledgerFile{Entries: m.entries}, "", "  ")
	if err != nil {
		return err
	}
	tmpPath := m.ledgerPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpPath, m.ledgerPath)
}

func (m *Manager) persistConfigLocked() error {
	if m.cfg == nil || strings.TrimSpace(m.configFilePath) == "" {
		return nil
	}
	if _, err := os.Stat(m.configFilePath); err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		rendered, err := yaml.Marshal(m.cfg)
		if err != nil {
			return err
		}
		return os.WriteFile(m.configFilePath, rendered, 0o600)
	}
	return config.SaveConfigPreserveComments(m.configFilePath, m.cfg)
}

func resolveLedgerPath(configFilePath string) string {
	configFilePath = strings.TrimSpace(configFilePath)
	if configFilePath == "" {
		return ""
	}
	base := filepath.Base(configFilePath)
	ext := filepath.Ext(base)
	name := strings.TrimSuffix(base, ext)
	if name == "" {
		name = "billing"
	}
	return filepath.Join(filepath.Dir(configFilePath), name+".billing.json")
}

func setUpdatedAt(entry *config.ClientAPIKey) {
	if entry == nil {
		return
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(entry.CreatedAt) == "" {
		entry.CreatedAt = now
	}
	entry.UpdatedAt = now
}
