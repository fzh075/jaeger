// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

func TestExplainerChainBuildPrompt(t *testing.T) {
	chain := &ExplainerChain{}

	req := types.SpanExplainRequest{
		SpanData: types.SpanData{
			TraceID:       "trace123",
			SpanID:        "span456",
			ServiceName:   "payment-service",
			OperationName: "processPayment",
			Duration:      "2.5s",
			Status:        "ERROR",
			StatusMessage: "Payment gateway timeout",
			Attributes: map[string]string{
				"http.method":      "POST",
				"http.status_code": "503",
			},
		},
		Context: "Part of checkout flow",
	}

	prompt := chain.buildPrompt(req)

	// Verify prompt contains key information
	assert.Contains(t, prompt, "payment-service")
	assert.Contains(t, prompt, "processPayment")
	assert.Contains(t, prompt, "2.5s")
	assert.Contains(t, prompt, "ERROR")
	assert.Contains(t, prompt, "Payment gateway timeout")
	assert.Contains(t, prompt, "Part of checkout flow")
}

func TestExplainerChainParseResponse(t *testing.T) {
	chain := &ExplainerChain{}

	tests := []struct {
		name     string
		response string
		want     types.SpanExplainResponse
	}{
		{
			name:     "valid JSON response",
			response: `{"summary":"Slow payment","explanation":"The payment took too long","possible_causes":["Network latency"],"suggestions":["Add timeout"]}`,
			want: types.SpanExplainResponse{
				Summary:        "Slow payment",
				Explanation:    "The payment took too long",
				PossibleCauses: []string{"Network latency"},
				Suggestions:    []string{"Add timeout"},
			},
		},
		{
			name:     "raw text response",
			response: "This is a plain text explanation without JSON formatting.",
			want: types.SpanExplainResponse{
				Explanation: "This is a plain text explanation without JSON formatting.",
			},
		},
		{
			name:     "JSON in code block",
			response: "```json\n{\"summary\":\"Test\",\"explanation\":\"Test explanation\"}\n```",
			want: types.SpanExplainResponse{
				Summary:     "Test",
				Explanation: "Test explanation",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := chain.parseResponse(tt.response)
			assert.NoError(t, err)
			assert.Equal(t, tt.want.Summary, got.Summary)
			assert.Equal(t, tt.want.Explanation, got.Explanation)
		})
	}
}
