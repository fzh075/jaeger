// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package ai_analysis

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/extension"
)

func TestNewFactory(t *testing.T) {
	factory := NewFactory()
	require.NotNil(t, factory)
	assert.Equal(t, componentType, factory.Type())
}

func TestCreateDefaultConfig(t *testing.T) {
	factory := NewFactory()
	cfg := factory.CreateDefaultConfig()

	require.NotNil(t, cfg)
	config, ok := cfg.(*Config)
	require.True(t, ok)

	// Check defaults
	assert.Equal(t, "ollama", config.LLM.Provider)
	assert.NotNil(t, config.LLM.Ollama)
	assert.Equal(t, "http://localhost:11434", config.LLM.Ollama.BaseURL)
	assert.Equal(t, "qwen2.5:1.5b", config.LLM.Ollama.Model)
	assert.Equal(t, 0.1, config.LLM.Ollama.Temperature)
	assert.Equal(t, 2048, config.LLM.Ollama.MaxTokens)

	// Check features
	assert.True(t, config.Features.NLSearch)
	assert.True(t, config.Features.SpanExplanation)
	assert.True(t, config.Features.SmartFilter)

	// Check performance
	assert.Equal(t, 30*time.Second, config.Performance.RequestTimeout)
	assert.True(t, config.Performance.StreamingEnabled)
}

func TestCreateExtension(t *testing.T) {
	set := extension.Settings{
		ID: ID,
	}
	cfg := createDefaultConfig()
	ext, err := createExtension(context.Background(), set, cfg)

	require.NoError(t, err)
	require.NotNil(t, ext)
}
