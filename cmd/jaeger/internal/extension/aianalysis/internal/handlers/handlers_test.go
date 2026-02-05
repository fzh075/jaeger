// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

func TestNLSearchHandler_InvalidRequest(t *testing.T) {
	handler := NewNLSearchHandler(nil, nil, zap.NewNop())

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid json",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty query",
			body:       `{"query":""}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ai/search", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Handle(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestExplainHandler_InvalidRequest(t *testing.T) {
	handler := NewExplainHandler(nil, zap.NewNop())

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid json",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty span_id",
			body:       `{"span_data":{"span_id":""}}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ai/explain/span", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Handle(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestClassifyHandler_InvalidRequest(t *testing.T) {
	handler := NewClassifyHandler(nil, zap.NewNop())

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "invalid json",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty spans",
			body:       `{"spans":[]}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ai/classify", bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handler.Handle(w, req)

			assert.Equal(t, tt.wantStatus, w.Code)
		})
	}
}

func TestTypesJSON(t *testing.T) {
	// Test that types serialize correctly
	query := types.ParsedQuery{
		ServiceName: "test-service",
		HasErrors:   true,
		DurationMin: "500ms",
		Attributes:  map[string]string{"key": "value"},
	}

	data, err := json.Marshal(query)
	require.NoError(t, err)

	var decoded types.ParsedQuery
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, query.ServiceName, decoded.ServiceName)
	assert.Equal(t, query.HasErrors, decoded.HasErrors)
	assert.Equal(t, query.DurationMin, decoded.DurationMin)
	assert.Equal(t, query.Attributes["key"], decoded.Attributes["key"])
}
