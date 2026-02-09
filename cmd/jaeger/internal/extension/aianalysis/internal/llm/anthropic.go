// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/llms"
	anthropicllm "github.com/tmc/langchaingo/llms/anthropic"
)

// AnthropicOptions contains options for the Anthropic provider.
type AnthropicOptions struct {
	APIKey      string
	BaseURL     string
	Model       string
	Temperature float64
	MaxTokens   int
}

// AnthropicProvider implements Provider using Anthropic.
type AnthropicProvider struct {
	llm     *anthropicllm.LLM
	model   string
	options AnthropicOptions
}

// NewAnthropicProvider creates a new Anthropic provider.
func NewAnthropicProvider(opts AnthropicOptions) (*AnthropicProvider, error) {
	if opts.Model == "" {
		opts.Model = "claude-3-5-sonnet-latest"
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.1
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 4096
	}

	llmOpts := []anthropicllm.Option{
		anthropicllm.WithModel(opts.Model),
	}
	if opts.APIKey != "" {
		llmOpts = append(llmOpts, anthropicllm.WithToken(opts.APIKey))
	}
	if opts.BaseURL != "" {
		llmOpts = append(llmOpts, anthropicllm.WithBaseURL(opts.BaseURL))
	}

	model, err := anthropicllm.New(llmOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create Anthropic client: %w", err)
	}

	return &AnthropicProvider{
		llm:     model,
		model:   opts.Model,
		options: opts,
	}, nil
}

// Generate sends a prompt to Anthropic and returns the response.
func (p *AnthropicProvider) Generate(ctx context.Context, prompt string) (string, error) {
	response, err := llms.GenerateFromSinglePrompt(
		ctx,
		p.llm,
		prompt,
		llms.WithTemperature(p.options.Temperature),
		llms.WithMaxTokens(p.options.MaxTokens),
	)
	if err != nil {
		return "", fmt.Errorf("anthropic generation failed: %w", err)
	}
	return response, nil
}

// GenerateStream sends a prompt and streams the response.
func (p *AnthropicProvider) GenerateStream(ctx context.Context, prompt string, handler StreamHandler) error {
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

// Close releases resources (no-op for Anthropic).
func (p *AnthropicProvider) Close() error {
	_ = p
	return nil
}

// Model returns the model name.
func (p *AnthropicProvider) Model() string {
	return p.model
}
