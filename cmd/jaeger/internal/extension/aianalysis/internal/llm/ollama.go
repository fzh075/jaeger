// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/ollama"
)

// OllamaOptions contains options for the Ollama provider.
type OllamaOptions struct {
	BaseURL     string
	Model       string
	Temperature float64
	MaxTokens   int
}

// OllamaProvider implements Provider using Ollama.
type OllamaProvider struct {
	llm     *ollama.LLM
	model   string
	options OllamaOptions
}

// NewOllamaProvider creates a new Ollama provider.
func NewOllamaProvider(opts OllamaOptions) (*OllamaProvider, error) {
	if opts.BaseURL == "" {
		opts.BaseURL = "http://localhost:11434"
	}
	if opts.Model == "" {
		opts.Model = "qwen2.5:1.5b"
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.1
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 2048
	}

	llm, err := ollama.New(
		ollama.WithServerURL(opts.BaseURL),
		ollama.WithModel(opts.Model),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Ollama client: %w", err)
	}

	return &OllamaProvider{
		llm:     llm,
		model:   opts.Model,
		options: opts,
	}, nil
}

// Generate sends a prompt to Ollama and returns the response.
func (p *OllamaProvider) Generate(ctx context.Context, prompt string) (string, error) {
	response, err := llms.GenerateFromSinglePrompt(
		ctx,
		p.llm,
		prompt,
		llms.WithTemperature(p.options.Temperature),
		llms.WithMaxTokens(p.options.MaxTokens),
	)
	if err != nil {
		return "", fmt.Errorf("ollama generation failed: %w", err)
	}
	return response, nil
}

// GenerateStream sends a prompt and streams the response.
func (p *OllamaProvider) GenerateStream(ctx context.Context, prompt string, handler StreamHandler) error {
	_, err := llms.GenerateFromSinglePrompt(
		ctx,
		p.llm,
		prompt,
		llms.WithTemperature(p.options.Temperature),
		llms.WithMaxTokens(p.options.MaxTokens),
		llms.WithStreamingFunc(func(_ context.Context, chunk []byte) error {
			return handler(string(chunk))
		}),
	)
	return err
}

// Close releases resources (no-op for Ollama).
func (p *OllamaProvider) Close() error {
	_ = p
	return nil
}

// Model returns the model name.
func (p *OllamaProvider) Model() string {
	return p.model
}
