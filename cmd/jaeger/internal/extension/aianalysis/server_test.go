// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

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

func TestRegisterRoutes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	s := newServer(cfg, component.TelemetrySettings{})

	router := mux.NewRouter()
	err := s.RegisterRoutes(router, &querysvc.QueryService{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/ai-analysis/capabilities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterRoutesValidation(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	s := newServer(cfg, component.TelemetrySettings{})

	err := s.RegisterRoutes(nil, &querysvc.QueryService{})
	require.ErrorContains(t, err, "router is required")

	err = s.RegisterRoutes(mux.NewRouter(), nil)
	require.ErrorContains(t, err, "query service is required")
}

func TestRegisterRoutesFeatureGate(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Features.NLSearch = false
	cfg.Features.SpanExplanation = false
	cfg.Features.SmartFilter = false
	s := newServer(cfg, component.TelemetrySettings{})

	router := mux.NewRouter()
	err := s.RegisterRoutes(router, &querysvc.QueryService{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/search", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStartShutdown(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	s := newServer(cfg, component.TelemetrySettings{})

	err := s.Start(t.Context(), componenttest.NewNopHost())
	require.NoError(t, err)

	err = s.Shutdown(t.Context())
	require.NoError(t, err)
}
