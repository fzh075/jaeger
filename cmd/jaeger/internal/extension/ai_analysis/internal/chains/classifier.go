// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/types"
)

const classifierPromptTemplate = `You are an expert in distributed tracing analysis. Classify the following spans to help users filter noise and focus on important spans.

CLASSIFICATION CATEGORIES:
- "error": Spans with error status or exception events
- "slow": Spans with unusually high latency
- "infrastructure": Internal framework/library spans (e.g., connection pools, serialization)
- "business": Business logic operations
- "external": External service calls (databases, APIs)
- "health": Health checks and monitoring spans

NOISE INDICATORS:
- Very short duration (<1ms) without meaningful work
- Internal framework operations users rarely need
- Repeated identical spans without errors

IMPORTANCE SCORING (0.0-1.0):
- 1.0: Critical errors, business-critical operations
- 0.7-0.9: Slow operations, external calls
- 0.4-0.6: Normal business operations
- 0.1-0.3: Infrastructure, internal framework
- 0.0-0.1: Likely noise, health checks

SPANS TO CLASSIFY:
%s

OUTPUT FORMAT (JSON array):
[
  {
    "span_id": "span_id_here",
    "category": "category_name",
    "is_noise": true/false,
    "importance": 0.0-1.0,
    "reason": "brief reason"
  }
]
`

// ClassifierChain handles span classification using LLM.
type ClassifierChain struct {
	provider llm.Provider
}

// NewClassifierChain creates a new classifier chain.
func NewClassifierChain(provider llm.Provider) *ClassifierChain {
	return &ClassifierChain{provider: provider}
}

// Classify classifies a list of spans.
func (c *ClassifierChain) Classify(ctx context.Context, spans []types.SpanData) (types.ClassifyResponse, error) {
	if len(spans) == 0 {
		return types.ClassifyResponse{
			Classifications: make(map[string]types.SpanClassification),
		}, nil
	}

	prompt := c.buildPrompt(spans)

	response, err := c.provider.Generate(ctx, prompt)
	if err != nil {
		return types.ClassifyResponse{
			Error: fmt.Sprintf("LLM generation failed: %v", err),
		}, nil
	}

	return c.parseResponse(response)
}

func (c *ClassifierChain) buildPrompt(spans []types.SpanData) string {
	// Build compact span descriptions
	type compactSpan struct {
		SpanID        string            `json:"span_id"`
		ServiceName   string            `json:"service"`
		OperationName string            `json:"operation"`
		Duration      string            `json:"duration"`
		Status        string            `json:"status"`
		Attributes    map[string]string `json:"attrs,omitempty"`
	}

	compactSpans := make([]compactSpan, len(spans))
	for i, span := range spans {
		compactSpans[i] = compactSpan{
			SpanID:        span.SpanID,
			ServiceName:   span.ServiceName,
			OperationName: span.OperationName,
			Duration:      span.Duration,
			Status:        span.Status,
			Attributes:    span.Attributes,
		}
	}

	spansJSON, _ := json.MarshalIndent(compactSpans, "", "  ")
	return fmt.Sprintf(classifierPromptTemplate, string(spansJSON))
}

func (c *ClassifierChain) parseResponse(response string) (types.ClassifyResponse, error) {
	jsonStr := extractJSON(response)

	var results []struct {
		SpanID     string  `json:"span_id"`
		Category   string  `json:"category"`
		IsNoise    bool    `json:"is_noise"`
		Importance float64 `json:"importance"`
		Reason     string  `json:"reason"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &results); err != nil {
		return types.ClassifyResponse{
			Error: fmt.Sprintf("Failed to parse classification response: %v", err),
		}, nil
	}

	classifications := make(map[string]types.SpanClassification)
	for _, r := range results {
		classifications[r.SpanID] = types.SpanClassification{
			Category:   r.Category,
			IsNoise:    r.IsNoise,
			Importance: r.Importance,
			Reason:     r.Reason,
		}
	}

	return types.ClassifyResponse{
		Classifications: classifications,
	}, nil
}
