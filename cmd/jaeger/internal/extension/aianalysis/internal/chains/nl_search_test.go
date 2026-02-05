// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "raw json",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "json in markdown code block",
			input:    "```json\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "json in generic code block",
			input:    "```\n{\"key\": \"value\"}\n```",
			expected: `{"key": "value"}`,
		},
		{
			name:     "json with surrounding text",
			input:    "Here is the result: {\"key\": \"value\"} as requested",
			expected: `{"key": "value"}`,
		},
		{
			name: "complex json in code block",
			input: `Here is the parsed query:
` + "```json" + `
{
  "service_name": "payment-service",
  "has_errors": true
}
` + "```",
			expected: `{
  "service_name": "payment-service",
  "has_errors": true
}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNLSearchChainPromptFormat(t *testing.T) {
	// Test that the prompt template is valid
	chain := &NLSearchChain{}
	assert.NotNil(t, chain)

	// The template should contain expected placeholders
	assert.Contains(t, nlSearchPromptTemplate, "%s")
	assert.Contains(t, nlSearchPromptTemplate, "service_name")
	assert.Contains(t, nlSearchPromptTemplate, "has_errors")
	assert.Contains(t, nlSearchPromptTemplate, "duration_min")
}
