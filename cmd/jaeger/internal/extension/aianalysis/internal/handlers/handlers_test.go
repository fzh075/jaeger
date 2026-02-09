// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/httpapi"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

type flakyProvider struct {
	calls int
}

func (f *flakyProvider) Generate(_ context.Context, _ string) (string, error) {
	f.calls++
	if f.calls == 1 {
		return "", errors.New("transient provider failure")
	}
	return `{"service_name":"payment-service","span_name":"","duration_min":"","duration_max":"","has_errors":true,"attributes":{"http.status_code":"500"},"limit":20,"confidence":0.9,"explanation":"ok"}`, nil
}

func (f *flakyProvider) GenerateStream(_ context.Context, _ string, _ llm.StreamHandler) error {
	_ = f
	return nil
}

func (f *flakyProvider) Close() error {
	_ = f
	return nil
}

func (f *flakyProvider) Model() string {
	_ = f
	return "test-model"
}

func TestNLSearchHandler_InvalidRequest(t *testing.T) {
	handler := NewNLSearchHandler(nil, zap.NewNop())

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
			req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/nl-search/parse", bytes.NewBufferString(tt.body))
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
			req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/spans/explain", bytes.NewBufferString(tt.body))
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
			req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/spans/classify", bytes.NewBufferString(tt.body))
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

func TestNLSearchHandler_RetryAttempts(t *testing.T) {
	provider := &flakyProvider{}
	handler := NewNLSearchHandler(provider, zap.NewNop(), HandlerOptions{
		ProviderName:  "mock",
		RetryAttempts: 1,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/nl-search/parse", bytes.NewBufferString(`{"query":"show me payment errors"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handler.Handle(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 2, provider.calls)

	var response httpapi.SuccessResponse[types.NLSearchResponse]
	err := json.NewDecoder(w.Body).Decode(&response)
	require.NoError(t, err)
	assert.Equal(t, "payment-service", response.Data.ParsedQuery.ServiceName)
	assert.True(t, response.Data.ParsedQuery.HasErrors)
}
