// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

type fakeExtension struct {
	extension.Extension
}

func (*fakeExtension) RegisterRoutes(*mux.Router, *querysvc.QueryService) error {
	return nil
}

type fakeHost struct {
	component.Host
	ext component.Component
}

type wrongComponent struct{}

func (wrongComponent) Start(context.Context, component.Host) error {
	return nil
}

func (wrongComponent) Shutdown(context.Context) error {
	return nil
}

func (h *fakeHost) GetExtensions() map[component.ID]component.Component {
	if h.ext == nil {
		return map[component.ID]component.Component{}
	}
	return map[component.ID]component.Component{
		ID: h.ext,
	}
}

func TestGetExtension(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		host := &fakeHost{
			Host: componenttest.NewNopHost(),
			ext:  &fakeExtension{},
		}
		ext, err := GetExtension(host)
		require.NoError(t, err)
		require.NotNil(t, ext)
	})

	t.Run("not found", func(t *testing.T) {
		host := &fakeHost{Host: componenttest.NewNopHost()}
		ext, err := GetExtension(host)
		require.Nil(t, ext)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrExtensionNotFound))
	})

	t.Run("wrong type", func(t *testing.T) {
		host := &fakeHost{
			Host: componenttest.NewNopHost(),
			ext:  wrongComponent{},
		}
		ext, err := GetExtension(host)
		require.Nil(t, ext)
		require.ErrorContains(t, err, "not of expected type")
	})
}

func TestHandleCapabilities(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	req := httptest.NewRequest(http.MethodGet, "/api/ai-analysis/capabilities", nil)
	w := httptest.NewRecorder()

	ext.handleCapabilities(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result CapabilitiesResponse
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)
	assert.True(t, result.NLSearch)
	assert.True(t, result.SpanExplanation)
	assert.True(t, result.SmartFilter)
	assert.True(t, result.Streaming)
	assert.Equal(t, "ollama", result.Provider)
	assert.Equal(t, "qwen2.5:1.5b", result.Model)
}

func TestRegisterRoutes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	router := mux.NewRouter()
	err := ext.RegisterRoutes(router, &querysvc.QueryService{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/ai-analysis/capabilities", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterRoutesValidation(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	err := ext.RegisterRoutes(nil, &querysvc.QueryService{})
	require.ErrorContains(t, err, "router is required")

	err = ext.RegisterRoutes(mux.NewRouter(), nil)
	require.ErrorContains(t, err, "query service is required")
}

func TestRegisterRoutesFeatureGate(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Features.NLSearch = false
	cfg.Features.SpanExplanation = false
	cfg.Features.SmartFilter = false
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	router := mux.NewRouter()
	err := ext.RegisterRoutes(router, &querysvc.QueryService{})
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/search", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestStartShutdown(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	err := ext.Start(t.Context(), componenttest.NewNopHost())
	require.NoError(t, err)

	err = ext.Shutdown(t.Context())
	require.NoError(t, err)
}
