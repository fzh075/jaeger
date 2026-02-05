// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"fmt"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

// OpenAIOptions contains options for the OpenAI provider.
type OpenAIOptions struct {
	APIKey      string
	BaseURL     string
	Model       string
	Temperature float64
	MaxTokens   int
}

// OpenAIProvider implements Provider using OpenAI.
type OpenAIProvider struct {
	llm     *openai.LLM
	model   string
	options OpenAIOptions
}

// NewOpenAIProvider creates a new OpenAI provider.
func NewOpenAIProvider(opts OpenAIOptions) (*OpenAIProvider, error) {
	if opts.Model == "" {
		opts.Model = "gpt-4o-mini"
	}
	if opts.Temperature == 0 {
		opts.Temperature = 0.1
	}
	if opts.MaxTokens == 0 {
		opts.MaxTokens = 4096
	}

	llmOpts := []openai.Option{
		openai.WithModel(opts.Model),
	}

	if opts.APIKey != "" {
		llmOpts = append(llmOpts, openai.WithToken(opts.APIKey))
	}

	if opts.BaseURL != "" {
		llmOpts = append(llmOpts, openai.WithBaseURL(opts.BaseURL))
	}

	llm, err := openai.New(llmOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OpenAI client: %w", err)
	}

	return &OpenAIProvider{
		llm:     llm,
		model:   opts.Model,
		options: opts,
	}, nil
}

// Generate sends a prompt to OpenAI and returns the response.
func (p *OpenAIProvider) Generate(ctx context.Context, prompt string) (string, error) {
	response, err := llms.GenerateFromSinglePrompt(
		ctx,
		p.llm,
		prompt,
		llms.WithTemperature(p.options.Temperature),
		llms.WithMaxTokens(p.options.MaxTokens),
	)
	if err != nil {
		return "", fmt.Errorf("openai generation failed: %w", err)
	}
	return response, nil
}

// GenerateStream sends a prompt and streams the response.
func (p *OpenAIProvider) GenerateStream(ctx context.Context, prompt string, handler StreamHandler) error {
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

// Close releases resources (no-op for OpenAI).
func (p *OpenAIProvider) Close() error {
	return nil
}

// Model returns the model name.
func (p *OpenAIProvider) Model() string {
	return p.model
}
