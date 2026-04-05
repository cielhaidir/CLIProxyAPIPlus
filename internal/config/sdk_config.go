// Package config provides configuration management for the CLI Proxy API server.
// It handles loading and parsing YAML configuration files, and provides structured
// access to application settings including server port, authentication directory,
// debug settings, proxy configuration, and API keys.
package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// SDKConfig represents the application's configuration, loaded from a YAML file.
type SDKConfig struct {
	// ProxyURL is the URL of an optional proxy server to use for outbound requests.
	ProxyURL string `yaml:"proxy-url" json:"proxy-url"`

	// ForceModelPrefix requires explicit model prefixes (e.g., "teamA/gemini-3-pro-preview")
	// to target prefixed credentials. When false, unprefixed model requests may use prefixed
	// credentials as well.
	ForceModelPrefix bool `yaml:"force-model-prefix" json:"force-model-prefix"`

	// RequestLog enables or disables detailed request logging functionality.
	RequestLog bool `yaml:"request-log" json:"request-log"`

	// APIKeys defines managed client API keys accepted by this proxy server.
	APIKeys ClientAPIKeys `yaml:"api-keys" json:"api-keys"`

	// ModelPricing defines per-model usage pricing configuration.
	ModelPricing []ModelPricing `yaml:"model-pricing,omitempty" json:"model-pricing,omitempty"`

	// PassthroughHeaders controls whether upstream response headers are forwarded to downstream clients.
	// Default is false (disabled).
	PassthroughHeaders bool `yaml:"passthrough-headers" json:"passthrough-headers"`

	// Streaming configures server-side streaming behavior (keep-alives and safe bootstrap retries).
	Streaming StreamingConfig `yaml:"streaming" json:"streaming"`

	// NonStreamKeepAliveInterval controls how often blank lines are emitted for non-streaming responses.
	// <= 0 disables keep-alives. Value is in seconds.
	NonStreamKeepAliveInterval int `yaml:"nonstream-keepalive-interval,omitempty" json:"nonstream-keepalive-interval,omitempty"`
}

// ClientAPIKey represents a client-facing API key and its management metadata.
type ClientAPIKey struct {
	Key           string   `yaml:"key" json:"key"`
	Name          string   `yaml:"name,omitempty" json:"name,omitempty"`
	Enabled       *bool    `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	AllowedModels []string `yaml:"allowed-models,omitempty" json:"allowed-models,omitempty"`
	CreditBalance int64    `yaml:"credit-balance,omitempty" json:"credit-balance,omitempty"`
	Currency      string   `yaml:"currency,omitempty" json:"currency,omitempty"`
	TotalTopup    int64    `yaml:"total-topup,omitempty" json:"total-topup,omitempty"`
	TotalSpent    int64    `yaml:"total-spent,omitempty" json:"total-spent,omitempty"`
	Notes         string   `yaml:"notes,omitempty" json:"notes,omitempty"`
	CreatedAt     string   `yaml:"created-at,omitempty" json:"created-at,omitempty"`
	UpdatedAt     string   `yaml:"updated-at,omitempty" json:"updated-at,omitempty"`
}

// ClientAPIKeys supports legacy string entries and managed object entries.
type ClientAPIKeys []ClientAPIKey

// ModelPricing represents pricing settings for a model.
type ModelPricing struct {
	Model            string `yaml:"model" json:"model"`
	Currency         string `yaml:"currency,omitempty" json:"currency,omitempty"`
	PricingType      string `yaml:"pricing-type,omitempty" json:"pricing-type,omitempty"`
	InputPrice       int64  `yaml:"input-price,omitempty" json:"input-price,omitempty"`
	OutputPrice      int64  `yaml:"output-price,omitempty" json:"output-price,omitempty"`
	ReasoningPrice   int64  `yaml:"reasoning-price,omitempty" json:"reasoning-price,omitempty"`
	CachedInputPrice int64  `yaml:"cached-input-price,omitempty" json:"cached-input-price,omitempty"`
	RequestPrice     int64  `yaml:"request-price,omitempty" json:"request-price,omitempty"`
	Enabled          *bool  `yaml:"enabled,omitempty" json:"enabled,omitempty"`
}

func (keys *ClientAPIKeys) UnmarshalYAML(value *yaml.Node) error {
	if value == nil {
		*keys = nil
		return nil
	}
	if value.Kind != yaml.SequenceNode {
		return fmt.Errorf("api-keys must be a sequence")
	}
	out := make([]ClientAPIKey, 0, len(value.Content))
	for _, item := range value.Content {
		if item == nil {
			continue
		}
		switch item.Kind {
		case yaml.ScalarNode:
			key := strings.TrimSpace(item.Value)
			if key == "" {
				continue
			}
			enabled := true
			out = append(out, ClientAPIKey{
				Key:           key,
				Enabled:       &enabled,
				Currency:      "USD",
				AllowedModels: nil,
			})
		case yaml.MappingNode:
			var entry ClientAPIKey
			if err := item.Decode(&entry); err != nil {
				return err
			}
			out = append(out, entry)
		default:
			return fmt.Errorf("api-keys entries must be strings or objects")
		}
	}
	*keys = out
	return nil
}

// StreamingConfig holds server streaming behavior configuration.
type StreamingConfig struct {
	// KeepAliveSeconds controls how often the server emits SSE heartbeats (": keep-alive\n\n").
	// <= 0 disables keep-alives. Default is 0.
	KeepAliveSeconds int `yaml:"keepalive-seconds,omitempty" json:"keepalive-seconds,omitempty"`

	// BootstrapRetries controls how many times the server may retry a streaming request before any bytes are sent,
	// to allow auth rotation / transient recovery.
	// <= 0 disables bootstrap retries. Default is 0.
	BootstrapRetries int `yaml:"bootstrap-retries,omitempty" json:"bootstrap-retries,omitempty"`
}
