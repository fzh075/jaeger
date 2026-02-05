// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockProvider is a mock LLM provider for testing.
type MockProvider struct {
	GenerateResponse string
	GenerateErr      error
	StreamChunks     []string
	StreamErr        error
	ModelName        string
	Calls            []string
}

func NewMockProvider(response string) *MockProvider {
	return &MockProvider{
		GenerateResponse: response,
		ModelName:        "mock-model",
	}
}

func (m *MockProvider) Generate(_ context.Context, prompt string) (string, error) {
	m.Calls = append(m.Calls, prompt)
	return m.GenerateResponse, m.GenerateErr
}

func (m *MockProvider) GenerateStream(_ context.Context, prompt string, handler StreamHandler) error {
	m.Calls = append(m.Calls, prompt)
	if m.StreamErr != nil {
		return m.StreamErr
	}
	for _, chunk := range m.StreamChunks {
		if err := handler(chunk); err != nil {
			return err
		}
	}
	return nil
}

func (m *MockProvider) Close() error {
	return nil
}

func (m *MockProvider) Model() string {
	return m.ModelName
}

func TestMockProvider(t *testing.T) {
	mock := NewMockProvider("test response")

	response, err := mock.Generate(context.Background(), "test prompt")
	require.NoError(t, err)
	assert.Equal(t, "test response", response)
	assert.Equal(t, "mock-model", mock.Model())
	assert.Len(t, mock.Calls, 1)
	assert.Equal(t, "test prompt", mock.Calls[0])
}

func TestMockProviderStream(t *testing.T) {
	mock := NewMockProvider("")
	mock.StreamChunks = []string{"chunk1", "chunk2", "chunk3"}

	var received []string
	err := mock.GenerateStream(context.Background(), "prompt", func(chunk string) error {
		received = append(received, chunk)
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, []string{"chunk1", "chunk2", "chunk3"}, received)
}
