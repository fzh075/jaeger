// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package types

// NLSearchRequest represents a natural language search request.
type NLSearchRequest struct {
	// Query is the natural language query (e.g., "find errors in payment-service over 2s")
	Query string `json:"query"`

	// Candidates constrains model output to known UI options.
	Candidates NLSearchCandidates `json:"candidates,omitempty"`
}

// NLSearchCandidates contains constrained options for NL search parsing.
type NLSearchCandidates struct {
	// Services is the list of selectable services in UI.
	Services []string `json:"services,omitempty"`

	// Operations is the list of selectable operations for current service.
	Operations []string `json:"operations,omitempty"`

	// Lookbacks is the list of selectable lookback options in UI.
	Lookbacks []string `json:"lookbacks,omitempty"`

	// Tags is the list of allowed tag keys.
	Tags []string `json:"tags,omitempty"`
}

// NLSearchResponse represents the response from natural language search.
type NLSearchResponse struct {
	// ParsedQuery contains the extracted search parameters
	ParsedQuery ParsedQuery `json:"parsed_query"`

	// Confidence indicates how confident the LLM is in the parsing (0-1)
	Confidence float64 `json:"confidence"`

	// Explanation provides a brief explanation of the parsing
	Explanation string `json:"explanation,omitempty"`
}

// ParsedQuery contains the structured search parameters extracted from natural language.
type ParsedQuery struct {
	// ServiceName is the target service name
	ServiceName string `json:"service_name,omitempty"`

	// OperationName is the operation/span name filter
	OperationName string `json:"operation_name,omitempty"`

	// DurationMin is the minimum duration filter (e.g., "500ms", "2s")
	DurationMin string `json:"duration_min,omitempty"`

	// DurationMax is the maximum duration filter
	DurationMax string `json:"duration_max,omitempty"`

	// Lookback is the relative time range option (e.g. "1h", "24h", "custom").
	Lookback string `json:"lookback,omitempty"`

	// StartTimeMin is the minimum start time (relative or absolute)
	StartTimeMin string `json:"start_time_min,omitempty"`

	// StartTimeMax is the maximum start time
	StartTimeMax string `json:"start_time_max,omitempty"`

	// Tags contains key-value tag filters.
	Tags map[string]string `json:"tags,omitempty"`

	// Limit is the maximum number of results
	Limit int `json:"limit,omitempty"`
}

// SpanExplainRequest represents a request to explain a span.
type SpanExplainRequest struct {
	// SpanData contains the span information to explain
	SpanData SpanData `json:"span_data"`

	// Context provides additional context (e.g., trace summary)
	Context string `json:"context,omitempty"`

	// Language is the preferred language for the explanation
	Language string `json:"language,omitempty"`
}

// SpanData contains span information for explanation.
type SpanData struct {
	// TraceID is the trace identifier
	TraceID string `json:"trace_id"`

	// SpanID is the span identifier
	SpanID string `json:"span_id"`

	// ServiceName is the service that produced the span
	ServiceName string `json:"service_name"`

	// OperationName is the span operation name
	OperationName string `json:"operation_name"`

	// Duration is the span duration
	Duration string `json:"duration"`

	// Status is the span status (OK, ERROR, UNSET)
	Status string `json:"status"`

	// StatusMessage is the error message if status is ERROR
	StatusMessage string `json:"status_message,omitempty"`

	// Tags contains span tags
	Tags map[string]string `json:"tags,omitempty"`

	// Events contains span events
	Events []SpanEvent `json:"events,omitempty"`

	// ParentSpanID is the parent span ID if any
	ParentSpanID string `json:"parent_span_id,omitempty"`

	// Children is the number of child spans
	Children int `json:"children,omitempty"`
}

// SpanEvent represents a span event.
type SpanEvent struct {
	Name      string            `json:"name"`
	Timestamp string            `json:"timestamp"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// SpanExplainResponse represents the response from span explanation.
type SpanExplainResponse struct {
	// Explanation is the AI-generated explanation
	Explanation string `json:"explanation"`

	// Summary is a brief one-line summary
	Summary string `json:"summary,omitempty"`

	// PossibleCauses lists potential causes for errors/latency
	PossibleCauses []string `json:"possible_causes,omitempty"`

	// Suggestions provides actionable suggestions
	Suggestions []string `json:"suggestions,omitempty"`
}

// ClassifyRequest represents a request to classify spans.
type ClassifyRequest struct {
	// Spans contains the spans to classify
	Spans []SpanData `json:"spans"`
}

// ClassifyResponse represents the response from span classification.
type ClassifyResponse struct {
	// Classifications maps span IDs to their classifications
	Classifications map[string]SpanClassification `json:"classifications"`
}

// SpanClassification contains the classification for a span.
type SpanClassification struct {
	// Category is the classification category
	Category string `json:"category"`

	// IsNoise indicates whether this span is likely noise
	IsNoise bool `json:"is_noise"`

	// Importance is the importance score (0-1)
	Importance float64 `json:"importance"`

	// Reason explains the classification
	Reason string `json:"reason,omitempty"`
}
