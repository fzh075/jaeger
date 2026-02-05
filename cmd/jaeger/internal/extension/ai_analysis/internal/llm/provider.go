// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
)

// Provider defines the interface for LLM providers.
type Provider interface {
	// Generate sends a prompt to the LLM and returns the complete response.
	Generate(ctx context.Context, prompt string) (string, error)

	// GenerateStream sends a prompt and streams the response chunk by chunk.
	GenerateStream(ctx context.Context, prompt string, handler StreamHandler) error

	// Close releases any resources held by the provider.
	Close() error

	// Model returns the model name being used.
	Model() string
}

// StreamHandler is called for each chunk of streaming response.
type StreamHandler func(chunk string) error
