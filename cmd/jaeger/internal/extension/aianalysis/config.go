// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"time"

	"github.com/asaskevich/govalidator"
	"go.opentelemetry.io/collector/confmap/xconfmap"
)

// Config represents the configuration for the AI Analysis extension.
type Config struct {
	// LLM contains the LLM provider configuration.
	LLM LLMConfig `mapstructure:"llm"`

	// Features enables/disables specific AI Analysis features.
	Features FeaturesConfig `mapstructure:"features"`

	// Performance contains performance-related settings.
	Performance PerformanceConfig `mapstructure:"performance"`
}

// LLMConfig contains the configuration for LLM providers.
type LLMConfig struct {
	// Provider specifies which LLM provider to use: "ollama", "openai", "anthropic"
	Provider string `mapstructure:"provider" valid:"required,in(ollama|openai|anthropic)"`

	// Ollama contains Ollama-specific configuration.
	Ollama *OllamaConfig `mapstructure:"ollama,omitempty"`

	// OpenAI contains OpenAI-specific configuration.
	OpenAI *OpenAIConfig `mapstructure:"openai,omitempty"`

	// Anthropic contains Anthropic-specific configuration.
	Anthropic *AnthropicConfig `mapstructure:"anthropic,omitempty"`
}

// OllamaConfig contains configuration for Ollama LLM provider.
type OllamaConfig struct {
	// BaseURL is the Ollama server URL (default: http://localhost:11434)
	BaseURL string `mapstructure:"base_url"`

	// Model is the Ollama model name (default: qwen2.5:7b)
	Model string `mapstructure:"model"`

	// Temperature controls randomness (0.0-1.0, default: 0.1)
	Temperature float64 `mapstructure:"temperature" valid:"range(0|1)"`

	// MaxTokens limits response length
	MaxTokens int `mapstructure:"max_tokens" valid:"range(1|32768)"`
}

// OpenAIConfig contains configuration for OpenAI LLM provider.
type OpenAIConfig struct {
	// APIKey is the OpenAI API key (required for OpenAI provider)
	APIKey string `mapstructure:"api_key"`

	// BaseURL is the OpenAI API base URL (optional, for custom endpoints)
	BaseURL string `mapstructure:"base_url,omitempty"`

	// Model is the OpenAI model name (default: gpt-4o-mini)
	Model string `mapstructure:"model"`

	// Temperature controls randomness (0.0-2.0, default: 0.1)
	Temperature float64 `mapstructure:"temperature" valid:"range(0|2)"`

	// MaxTokens limits response length
	MaxTokens int `mapstructure:"max_tokens" valid:"range(1|128000)"`
}

// AnthropicConfig contains configuration for Anthropic LLM provider.
type AnthropicConfig struct {
	// APIKey is the Anthropic API key. If empty, provider may use ANTHROPIC_API_KEY.
	APIKey string `mapstructure:"api_key"`

	// BaseURL is the Anthropic API base URL (optional, for custom endpoints).
	BaseURL string `mapstructure:"base_url,omitempty"`

	// Model is the Anthropic model name (default: claude-3-5-sonnet-latest).
	Model string `mapstructure:"model"`

	// Temperature controls randomness (0.0-1.0, default: 0.1).
	Temperature float64 `mapstructure:"temperature" valid:"range(0|1)"`

	// MaxTokens limits response length.
	MaxTokens int `mapstructure:"max_tokens" valid:"range(1|200000)"`
}

// FeaturesConfig enables/disables specific AI Analysis features.
type FeaturesConfig struct {
	// NLSearch enables natural language trace search
	NLSearch bool `mapstructure:"nl_search"`

	// SpanExplanation enables AI-powered span explanations
	SpanExplanation bool `mapstructure:"span_explanation"`

	// SmartFilter enables intelligent span filtering/classification
	SmartFilter bool `mapstructure:"smart_filter"`
}

// PerformanceConfig contains performance-related settings.
type PerformanceConfig struct {
	//
	// RequestTimeout is the maximum time for AI requests TODO(fzh075)
	RequestTimeout time.Duration `mapstructure:"request_timeout"`

	// MaxRequestBodyBytes limits request body size for AI endpoints.
	MaxRequestBodyBytes int64 `mapstructure:"max_request_body_bytes" valid:"range(1024|10485760)"`

	// MaxSpansPerClassify limits how many spans can be classified in one request.
	MaxSpansPerClassify int `mapstructure:"max_spans_per_classify" valid:"range(1|2000)"`

	// MaxConcurrentRequests limits concurrent AI requests.
	MaxConcurrentRequests int `mapstructure:"max_concurrent_requests" valid:"range(1|1024)"`

	// RetryAttempts controls retry count for transient provider errors.
	RetryAttempts int `mapstructure:"retry_attempts" valid:"range(0|5)"`

	// StreamingEnabled enables SSE streaming for long responses
	StreamingEnabled bool `mapstructure:"streaming_enabled"`

	// CacheEnabled enables response caching
	CacheEnabled bool `mapstructure:"cache_enabled"`

	// CacheTTL is the cache time-to-live duration
	CacheTTL time.Duration `mapstructure:"cache_ttl"`
}

// Validate checks if the configuration is valid.
func (cfg *Config) Validate() error {
	if cfg.LLM.Provider == "ollama" && cfg.LLM.Ollama == nil {
		cfg.LLM.Ollama = &OllamaConfig{
			BaseURL:     "http://localhost:11434",
			Model:       "qwen2.5:1.5b",
			Temperature: 0.1,
			MaxTokens:   2048,
		}
	}
	if cfg.LLM.Provider == "openai" && cfg.LLM.OpenAI == nil {
		cfg.LLM.OpenAI = &OpenAIConfig{
			Model:       "gpt-4o-mini",
			Temperature: 0.1,
			MaxTokens:   4096,
		}
	}
	if cfg.LLM.Provider == "anthropic" && cfg.LLM.Anthropic == nil {
		cfg.LLM.Anthropic = &AnthropicConfig{
			Model:       "claude-3-5-sonnet-latest",
			Temperature: 0.1,
			MaxTokens:   4096,
		}
	}

	if cfg.Performance.RequestTimeout == 0 {
		cfg.Performance.RequestTimeout = 30 * time.Second
	}
	if cfg.Performance.MaxRequestBodyBytes == 0 {
		cfg.Performance.MaxRequestBodyBytes = 256 * 1024
	}
	if cfg.Performance.MaxSpansPerClassify == 0 {
		cfg.Performance.MaxSpansPerClassify = 200
	}
	if cfg.Performance.MaxConcurrentRequests == 0 {
		cfg.Performance.MaxConcurrentRequests = 16
	}

	_, err := govalidator.ValidateStruct(cfg)
	return err
}

var _ xconfmap.Validator = (*Config)(nil)
