// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr bool
	}{
		{
			name: "valid ollama config",
			config: Config{
				LLM: LLMConfig{
					Provider: "ollama",
					Ollama: &OllamaConfig{
						BaseURL:     "http://localhost:11434",
						Model:       "qwen2.5:7b",
						Temperature: 0.1,
						MaxTokens:   2048,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid openai config",
			config: Config{
				LLM: LLMConfig{
					Provider: "openai",
					OpenAI: &OpenAIConfig{
						APIKey:      "test-key",
						Model:       "gpt-4o-mini",
						Temperature: 0.1,
						MaxTokens:   4096,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "valid anthropic config",
			config: Config{
				LLM: LLMConfig{
					Provider: "anthropic",
					Anthropic: &AnthropicConfig{
						APIKey:      "anthropic-key",
						Model:       "claude-3-5-sonnet-latest",
						Temperature: 0.1,
						MaxTokens:   4096,
					},
				},
			},
			wantErr: false,
		},
		{
			name: "ollama config auto-created when nil",
			config: Config{
				LLM: LLMConfig{
					Provider: "ollama",
				},
			},
			wantErr: false,
		},
		{
			name: "invalid provider",
			config: Config{
				LLM: LLMConfig{
					Provider: "invalid",
				},
			},
			wantErr: true,
		},
		{
			name: "invalid temperature",
			config: Config{
				LLM: LLMConfig{
					Provider: "ollama",
					Ollama: &OllamaConfig{
						Temperature: 2.0, // Out of range for ollama (0-1)
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid max request body bytes",
			config: Config{
				LLM: LLMConfig{
					Provider: "ollama",
				},
				Performance: PerformanceConfig{
					MaxRequestBodyBytes: 32,
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestConfigValidateAutoCreateOllama(t *testing.T) {
	cfg := &Config{
		LLM: LLMConfig{
			Provider: "ollama",
		},
	}

	err := cfg.Validate()
	require.NoError(t, err)
	require.NotNil(t, cfg.LLM.Ollama)
	assert.Equal(t, "http://localhost:11434", cfg.LLM.Ollama.BaseURL)
	assert.Equal(t, "qwen2.5:1.5b", cfg.LLM.Ollama.Model)
	assert.Equal(t, 30_000, int(cfg.Performance.RequestTimeout.Milliseconds()))
	assert.EqualValues(t, 256*1024, cfg.Performance.MaxRequestBodyBytes)
	assert.Equal(t, 200, cfg.Performance.MaxSpansPerClassify)
	assert.Equal(t, 16, cfg.Performance.MaxConcurrentRequests)
}
