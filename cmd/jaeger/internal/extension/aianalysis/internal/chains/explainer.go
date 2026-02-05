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
- Attributes: %s
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
	prompt := c.buildPrompt(req)

	response, err := c.provider.Generate(ctx, prompt)
	if err != nil {
		return types.SpanExplainResponse{
			Error: fmt.Sprintf("LLM generation failed: %v", err),
		}, nil
	}

	return c.parseResponse(response)
}

// ExplainStream generates an explanation with streaming response.
func (c *ExplainerChain) ExplainStream(ctx context.Context, req types.SpanExplainRequest, handler llm.StreamHandler) error {
	prompt := c.buildPrompt(req)
	return c.provider.GenerateStream(ctx, prompt, handler)
}

func (c *ExplainerChain) buildPrompt(req types.SpanExplainRequest) string {
	span := req.SpanData

	attrs, _ := json.Marshal(span.Attributes)
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
		string(attrs),
		string(events),
		span.ParentSpanID,
		span.Children,
		contextStr,
	)
}

func (c *ExplainerChain) parseResponse(response string) (types.SpanExplainResponse, error) {
	jsonStr := extractJSON(response)

	var result struct {
		Summary        string   `json:"summary"`
		Explanation    string   `json:"explanation"`
		PossibleCauses []string `json:"possible_causes"`
		Suggestions    []string `json:"suggestions"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
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
