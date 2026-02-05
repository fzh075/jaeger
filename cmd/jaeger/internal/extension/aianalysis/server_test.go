// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/component"
)

func TestHandleHealth(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	s := newServer(cfg, component.TelemetrySettings{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	s.handleHealth(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result map[string]string
	err := json.NewDecoder(w.Body).Decode(&result)
	assert.NoError(t, err)
	assert.Equal(t, "ok", result["status"])
	assert.Equal(t, "ollama", result["provider"])
}

func TestHandleCapabilities(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	s := newServer(cfg, component.TelemetrySettings{})

	req := httptest.NewRequest(http.MethodGet, "/api/ai-analysis/capabilities", nil)
	w := httptest.NewRecorder()

	s.handleCapabilities(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result CapabilitiesResponse
	err := json.NewDecoder(w.Body).Decode(&result)
	assert.NoError(t, err)
	assert.True(t, result.NLSearch)
	assert.True(t, result.SpanExplanation)
	assert.True(t, result.SmartFilter)
	assert.True(t, result.Streaming)
	assert.Equal(t, "ollama", result.Provider)
	assert.Equal(t, "qwen2.5:1.5b", result.Model)
}

func TestCorsMiddleware(t *testing.T) {
	handler := corsMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	t.Run("adds CORS headers", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
		assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "POST")
	})

	t.Run("handles preflight OPTIONS request", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodOptions, "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
	})
}
