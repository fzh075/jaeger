// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
)

// componentType is the name of this extension in configuration.
var componentType = component.MustNewType("ai_analysis")

// ID is the identifier of this extension.
var ID = component.NewID(componentType)

// NewFactory creates a factory for the AI Analysis extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		componentType,
		createDefaultConfig,
		createExtension,
		component.StabilityLevelAlpha,
	)
}

// createDefaultConfig creates the default configuration for the extension.
func createDefaultConfig() component.Config {
	return &Config{
		LLM: LLMConfig{
			Provider: "ollama",
			Ollama: &OllamaConfig{
				BaseURL:     "http://localhost:11434",
				Model:       "qwen2.5:1.5b",
				Temperature: 0.1,
				MaxTokens:   2048,
			},
		},
		Features: FeaturesConfig{
			NLSearch:        true,
			SpanExplanation: true,
			SmartFilter:     true,
		},
		Performance: PerformanceConfig{
			RequestTimeout:   30 * time.Second,
			StreamingEnabled: true,
			CacheEnabled:     false,
			CacheTTL:         5 * time.Minute,
		},
	}
}

// createExtension creates the extension based on this config.
func createExtension(_ context.Context, set extension.Settings, cfg component.Config) (extension.Extension, error) {
	return newAIAnalysisExtension(cfg.(*Config), set.TelemetrySettings), nil
}
