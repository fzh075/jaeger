// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

const nlSearchPromptTemplate = `You are a trace search query parser for Jaeger, a distributed tracing system.

Convert the natural language query into structured search parameters.

IMPORTANT RULES:
1. Extract only explicitly mentioned parameters
2. Use exact service names when mentioned
3. Duration formats: "500ms", "2s", "1m"
4. Time formats: "-1h" (relative), "2024-01-01T00:00:00Z" (RFC3339)
5. For error queries, set has_errors=true
6. Default limit is 20 if not specified

OUTPUT FORMAT (JSON only, no explanation):
{
  "service_name": "string or empty",
  "span_name": "string or empty",
  "duration_min": "string or empty",
  "duration_max": "string or empty",
  "has_errors": boolean,
  "start_time_min": "string or empty",
  "start_time_max": "string or empty",
  "attributes": {"key": "value"} or {},
  "limit": number,
  "confidence": 0.0-1.0
}

EXAMPLES:

Query: "Show me errors in payment-service"
{"service_name":"payment-service","span_name":"","duration_min":"","duration_max":"","has_errors":true,"start_time_min":"-1h","start_time_max":"","attributes":{},"limit":20,"confidence":0.95}

Query: "Find slow requests over 2 seconds in api-gateway"
{"service_name":"api-gateway","span_name":"","duration_min":"2s","duration_max":"","has_errors":false,"start_time_min":"-1h","start_time_max":"","attributes":{},"limit":20,"confidence":0.9}

Query: "找出 user-service 中超过 500ms 的请求"
{"service_name":"user-service","span_name":"","duration_min":"500ms","duration_max":"","has_errors":false,"start_time_min":"-1h","start_time_max":"","attributes":{},"limit":20,"confidence":0.9}

Query: "Show traces with http.status_code=500 in order-service"
{"service_name":"order-service","span_name":"","duration_min":"","duration_max":"","has_errors":false,"start_time_min":"-1h","start_time_max":"","attributes":{"http.status_code":"500"},"limit":20,"confidence":0.85}

Now parse this query:
Query: "%s"
`

// NLSearchChain handles natural language to search parameter conversion.
type NLSearchChain struct {
	provider llm.Provider
}

// NewNLSearchChain creates a new NL search chain.
func NewNLSearchChain(provider llm.Provider) *NLSearchChain {
	return &NLSearchChain{provider: provider}
}

// ParsedQueryWithConfidence extends ParsedQuery with confidence.
type ParsedQueryWithConfidence struct {
	types.ParsedQuery
	Confidence float64 `json:"confidence"`
}

// Parse converts a natural language query to structured search parameters.
func (c *NLSearchChain) Parse(ctx context.Context, query string) (types.NLSearchResponse, error) {
	prompt := fmt.Sprintf(nlSearchPromptTemplate, query)

	response, err := c.provider.Generate(ctx, prompt)
	if err != nil {
		return types.NLSearchResponse{
			Error: fmt.Sprintf("LLM generation failed: %v", err),
		}, nil
	}

	// Extract JSON from response (handle markdown code blocks)
	jsonStr := extractJSON(response)

	var parsed ParsedQueryWithConfidence
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return types.NLSearchResponse{
			Error: fmt.Sprintf("Failed to parse LLM response: %v", err),
		}, nil
	}

	return types.NLSearchResponse{
		ParsedQuery: parsed.ParsedQuery,
		Confidence:  parsed.Confidence,
	}, nil
}

// ParseStream converts a natural language query with streaming response.
func (c *NLSearchChain) ParseStream(ctx context.Context, query string, handler llm.StreamHandler) error {
	prompt := fmt.Sprintf(nlSearchPromptTemplate, query)
	return c.provider.GenerateStream(ctx, prompt, handler)
}

// extractJSON extracts JSON from a response that may contain markdown code blocks.
func extractJSON(response string) string {
	response = strings.TrimSpace(response)

	// Try to find JSON in code block
	if idx := strings.Index(response, "```json"); idx != -1 {
		start := idx + 7
		end := strings.Index(response[start:], "```")
		if end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// Try to find JSON in generic code block
	if idx := strings.Index(response, "```"); idx != -1 {
		start := idx + 3
		// Skip language identifier line
		if nlIdx := strings.Index(response[start:], "\n"); nlIdx != -1 {
			start = start + nlIdx + 1
		}
		end := strings.Index(response[start:], "```")
		if end != -1 {
			return strings.TrimSpace(response[start : start+end])
		}
	}

	// Find the first JSON structure (object or array)
	objIdx := strings.Index(response, "{")
	arrIdx := strings.Index(response, "[")

	// Determine which comes first
	if arrIdx != -1 && (objIdx == -1 || arrIdx < objIdx) {
		// Array comes first
		lastIdx := strings.LastIndex(response, "]")
		if lastIdx > arrIdx {
			return response[arrIdx : lastIdx+1]
		}
	} else if objIdx != -1 {
		// Object comes first
		lastIdx := strings.LastIndex(response, "}")
		if lastIdx > objIdx {
			return response[objIdx : lastIdx+1]
		}
	}

	return response
}
