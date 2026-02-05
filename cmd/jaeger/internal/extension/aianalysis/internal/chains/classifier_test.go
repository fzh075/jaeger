// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

func TestClassifierChainBuildPrompt(t *testing.T) {
	chain := &ClassifierChain{}

	spans := []types.SpanData{
		{
			SpanID:        "span1",
			ServiceName:   "api-gateway",
			OperationName: "GET /users",
			Duration:      "50ms",
			Status:        "OK",
		},
		{
			SpanID:        "span2",
			ServiceName:   "db-service",
			OperationName: "SELECT users",
			Duration:      "2s",
			Status:        "OK",
		},
	}

	prompt := chain.buildPrompt(spans)

	// Verify prompt contains span information
	assert.Contains(t, prompt, "span1")
	assert.Contains(t, prompt, "span2")
	assert.Contains(t, prompt, "api-gateway")
	assert.Contains(t, prompt, "db-service")
	assert.Contains(t, prompt, "GET /users")
	assert.Contains(t, prompt, "SELECT users")
}

func TestClassifierChainParseResponse(t *testing.T) {
	chain := &ClassifierChain{}

	tests := []struct {
		name     string
		response string
		wantLen  int
		wantErr  bool
	}{
		{
			name:     "valid classification response",
			response: `[{"span_id":"span1","category":"business","is_noise":false,"importance":0.8,"reason":"API endpoint"},{"span_id":"span2","category":"slow","is_noise":false,"importance":0.9,"reason":"Slow query"}]`,
			wantLen:  2,
			wantErr:  false,
		},
		{
			name:     "empty array",
			response: `[]`,
			wantLen:  0,
			wantErr:  false,
		},
		{
			name:     "invalid JSON",
			response: `not json`,
			wantLen:  0,
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := chain.parseResponse(tt.response)
			if tt.wantErr {
				assert.NotEmpty(t, got.Error)
			} else {
				assert.Len(t, got.Classifications, tt.wantLen)
			}
		})
	}
}

func TestClassifierChainParseResponseWithCodeBlock(t *testing.T) {
	chain := &ClassifierChain{}

	response := "```json\n[{\"span_id\":\"span1\",\"category\":\"error\",\"is_noise\":false,\"importance\":1.0,\"reason\":\"Error span\"}]\n```"

	got, _ := chain.parseResponse(response)

	assert.Len(t, got.Classifications, 1)
	assert.Equal(t, "error", got.Classifications["span1"].Category)
	assert.Equal(t, 1.0, got.Classifications["span1"].Importance)
}
