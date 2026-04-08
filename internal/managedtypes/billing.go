package managedtypes

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
