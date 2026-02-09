// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
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
  "lookback": "1h"
}
` + "```",
			expected: `{
  "service_name": "payment-service",
  "lookback": "1h"
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
	assert.Contains(t, nlSearchPromptTemplate, "attributes.error")
	assert.Contains(t, nlSearchPromptTemplate, "duration_min")
	assert.Contains(t, nlSearchPromptTemplate, "lookback")
}

type sequenceProvider struct {
	responses []string
	errs      []error
	calls     int
}

func (p *sequenceProvider) Generate(_ context.Context, _ string) (string, error) {
	idx := p.calls
	p.calls++
	if idx < len(p.errs) && p.errs[idx] != nil {
		return "", p.errs[idx]
	}
	if idx >= len(p.responses) {
		return "", errors.New("no response configured")
	}
	return p.responses[idx], nil
}

func (*sequenceProvider) GenerateStream(context.Context, string, llm.StreamHandler) error {
	return nil
}

func (*sequenceProvider) Close() error {
	return nil
}

func (*sequenceProvider) Model() string {
	return "test-model"
}

func TestNLSearchChainParse_NormalizeCandidatesAndBounds(t *testing.T) {
	provider := &sequenceProvider{
		responses: []string{
			`{
				"service_name":"svca",
				"span_name":"ALL",
				"duration_min":" 500 MS ",
				"duration_max":"2S",
				"lookback":"1H",
				"attributes":{"ERROR":"true","HTTP.STATUS_CODE":"500"},
				"limit":50000,
				"confidence":1.2
			}`,
		},
	}
	chain := NewNLSearchChain(provider)
	resp, err := chain.Parse(context.Background(), types.NLSearchRequest{
		Query: "show errors in svcA with 500 in 1h",
		Candidates: types.NLSearchCandidates{
			Services:   []string{"svcA", "svcB"},
			Operations: []string{"all", "createPayment"},
			Lookbacks:  []string{"1h", "24h", "custom"},
			TagKeys:    []string{"error", "http.status_code"},
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "svcA", resp.ParsedQuery.ServiceName)
	assert.Equal(t, "all", resp.ParsedQuery.SpanName)
	assert.Equal(t, "500ms", resp.ParsedQuery.DurationMin)
	assert.Equal(t, "2s", resp.ParsedQuery.DurationMax)
	assert.Equal(t, "1h", resp.ParsedQuery.Lookback)
	assert.Equal(t, "", resp.ParsedQuery.StartTimeMin)
	assert.Equal(t, "", resp.ParsedQuery.StartTimeMax)
	assert.Equal(t, "true", resp.ParsedQuery.Attributes["error"])
	assert.Equal(t, "500", resp.ParsedQuery.Attributes["http.status_code"])
	assert.Equal(t, maxNLSearchLimit, resp.ParsedQuery.Limit)
	assert.Equal(t, 1.0, resp.Confidence)
}

func TestNLSearchChainParse_CustomTimeNormalization(t *testing.T) {
	provider := &sequenceProvider{
		responses: []string{
			`{
				"service_name":"svcA",
				"lookback":"custom",
				"start_time_min":"-2h",
				"start_time_max":"now",
				"attributes":{"error":"true"},
				"limit":20,
				"confidence":0.8
			}`,
		},
	}
	chain := NewNLSearchChain(provider)
	resp, err := chain.Parse(context.Background(), types.NLSearchRequest{
		Query: "errors for svcA in last two hours",
		Candidates: types.NLSearchCandidates{
			Services:  []string{"svcA"},
			Lookbacks: []string{"1h", "24h", "custom"},
			TagKeys:   []string{"error"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "custom", resp.ParsedQuery.Lookback)
	assert.Equal(t, 1, provider.calls)
	start, err := time.Parse(time.RFC3339, resp.ParsedQuery.StartTimeMin)
	require.NoError(t, err)
	end, err := time.Parse(time.RFC3339, resp.ParsedQuery.StartTimeMax)
	require.NoError(t, err)
	assert.True(t, end.After(start))
}

func TestNLSearchChainParse_RepairRetry(t *testing.T) {
	provider := &sequenceProvider{
		responses: []string{
			`{"service_name":"svcA",`,
			`{"service_name":"svcA","lookback":"1h","attributes":{"error":"true"},"limit":20,"confidence":0.9}`,
		},
	}
	chain := NewNLSearchChain(provider)
	resp, err := chain.Parse(context.Background(), types.NLSearchRequest{
		Query: "show errors in svcA for one hour",
		Candidates: types.NLSearchCandidates{
			Services:  []string{"svcA"},
			Lookbacks: []string{"1h", "custom"},
			TagKeys:   []string{"error"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, provider.calls)
	assert.Equal(t, "svcA", resp.ParsedQuery.ServiceName)
	assert.Equal(t, "1h", resp.ParsedQuery.Lookback)
}

func TestNLSearchChainParse_RepairStillInvalid(t *testing.T) {
	provider := &sequenceProvider{
		responses: []string{
			`{"service_name":"svcA",`,
			`{"service_name":"unknown-service","lookback":"1h","attributes":{"error":"true"},"limit":20,"confidence":0.9}`,
		},
	}
	chain := NewNLSearchChain(provider)
	_, err := chain.Parse(context.Background(), types.NLSearchRequest{
		Query: "show errors in unknown-service for one hour",
		Candidates: types.NLSearchCandidates{
			Services:  []string{"svcA"},
			Lookbacks: []string{"1h", "custom"},
			TagKeys:   []string{"error"},
		},
	})
	require.Error(t, err)
	assert.Equal(t, 2, provider.calls)
	assert.True(t, errors.Is(err, ErrInvalidLLMResponse))
}
