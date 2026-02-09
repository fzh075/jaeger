// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package httpapi

import (
	"encoding/json"
	"net/http"
)

// Meta contains optional metadata for successful responses.
type Meta struct {
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
}

// SuccessResponse is the unified success response envelope.
type SuccessResponse[T any] struct {
	Data T    `json:"data"`
	Meta Meta `json:"meta,omitempty"`
}

// APIError is the structured error payload.
type APIError struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// ErrorResponse is the unified error envelope.
type ErrorResponse struct {
	Error APIError `json:"error"`
}

// WriteData writes a successful JSON response with data and optional metadata.
func WriteData[T any](w http.ResponseWriter, status int, data T, meta *Meta) {
	resp := SuccessResponse[T]{Data: data}
	if meta != nil {
		resp.Meta = *meta
	}
	writeJSON(w, status, resp)
}

// WriteError writes a structured JSON error response.
func WriteError(w http.ResponseWriter, status int, code, message string, details map[string]any) {
	writeJSON(w, status, ErrorResponse{
		Error: APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
