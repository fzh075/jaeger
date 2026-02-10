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

const explainerPromptTemplate = `You are an expert distributed systems engineer analyzing trace spans from Jaeger.

Analyze the following span and provide a clear, actionable explanation.

SPAN DETAILS:
- Service: %s
- Operation: %s
- Duration: %s
- Status: %s
- Status Message: %s
- Tags: %s
- Events: %s
- Parent Span ID: %s
- Child Count: %d

CONTEXT (if provided):
%s

ANALYSIS REQUIREMENTS:
1. Explain what this span represents in plain language
2. If there's an error, identify likely root causes
3. If slow (>500ms for typical operations), suggest optimization areas
4. Provide actionable recommendations

OUTPUT FORMAT (JSON only):
{
  "summary": "One-line summary of the span",
  "explanation": "Detailed explanation (2-4 sentences)",
  "possible_causes": ["cause1", "cause2"] or [] if no errors,
  "suggestions": ["suggestion1", "suggestion2"]
}

EXAMPLES:

Span: HTTP 500 error in payment-service
{"summary":"Payment service internal server error","explanation":"The payment-service encountered an HTTP 500 error during a POST request to /api/checkout. This typically indicates an unhandled exception or backend service failure.","possible_causes":["Database connection timeout","Invalid payment data format","Downstream payment gateway unavailable"],"suggestions":["Check payment-service logs for stack traces","Verify database connectivity","Review payment gateway status"]}

Span: Database query taking 3 seconds
{"summary":"Slow database query detected","explanation":"A database query in order-service took 3 seconds to complete, significantly above normal latency. This could impact user experience and cascade to downstream services.","possible_causes":["Missing database index","Large result set without pagination","Table lock contention"],"suggestions":["Analyze query execution plan","Add appropriate indexes","Implement query pagination"]}

Now analyze this span:
`

// ExplainerChain handles span explanation using LLM.
type ExplainerChain struct {
	provider llm.Provider
}

// NewExplainerChain creates a new explainer chain.
func NewExplainerChain(provider llm.Provider) *ExplainerChain {
	return &ExplainerChain{provider: provider}
}

// Explain generates an explanation for a span.
func (c *ExplainerChain) Explain(ctx context.Context, req types.SpanExplainRequest) (types.SpanExplainResponse, error) {
	if c.provider == nil {
		return types.SpanExplainResponse{}, ErrProviderUnavailable
	}

	prompt := c.buildPrompt(req)

	response, err := c.provider.Generate(ctx, prompt)
	if err != nil {
		return types.SpanExplainResponse{}, fmt.Errorf("%w: %w", ErrLLMGeneration, err)
	}

	parsed, err := c.parseResponse(response)
	if err != nil {
		return types.SpanExplainResponse{}, err
	}
	if strings.TrimSpace(parsed.Explanation) == "" {
		return types.SpanExplainResponse{}, fmt.Errorf("%w: empty explanation", ErrInvalidLLMResponse)
	}
	return parsed, nil
}

// ExplainStream generates an explanation with streaming response.
func (c *ExplainerChain) ExplainStream(ctx context.Context, req types.SpanExplainRequest, handler llm.StreamHandler) error {
	if c.provider == nil {
		return ErrProviderUnavailable
	}

	prompt := c.buildPrompt(req)
	if err := c.provider.GenerateStream(ctx, prompt, handler); err != nil {
		return fmt.Errorf("%w: %w", ErrLLMGeneration, err)
	}
	return nil
}

func (c *ExplainerChain) buildPrompt(req types.SpanExplainRequest) string {
	_ = c

	span := req.SpanData

	tags, _ := json.Marshal(span.Tags)
	events, _ := json.Marshal(span.Events)

	statusMessage := span.StatusMessage
	if statusMessage == "" {
		statusMessage = "N/A"
	}

	contextStr := req.Context
	if contextStr == "" {
		contextStr = "No additional context provided"
	}

	return fmt.Sprintf(explainerPromptTemplate,
		span.ServiceName,
		span.OperationName,
		span.Duration,
		span.Status,
		statusMessage,
		string(tags),
		string(events),
		span.ParentSpanID,
		span.Children,
		contextStr,
	)
}

func (c *ExplainerChain) parseResponse(response string) (types.SpanExplainResponse, error) {
	_ = c

	jsonStr := extractJSON(response)

	var result struct {
		Summary        string   `json:"summary"`
		Explanation    string   `json:"explanation"`
		PossibleCauses []string `json:"possible_causes"`
		Suggestions    []string `json:"suggestions"`
	}

	if !decodeExplainResponse(jsonStr, &result) {
		// If JSON parsing fails, use raw response as explanation
		return types.SpanExplainResponse{
			Explanation: strings.TrimSpace(response),
		}, nil
	}

	return types.SpanExplainResponse{
		Summary:        result.Summary,
		Explanation:    result.Explanation,
		PossibleCauses: result.PossibleCauses,
		Suggestions:    result.Suggestions,
	}, nil
}

func decodeExplainResponse(jsonStr string, out any) bool {
	return json.Unmarshal([]byte(jsonStr), out) == nil
}
