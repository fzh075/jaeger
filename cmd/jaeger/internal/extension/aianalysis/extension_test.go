// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/httpapi"
)

type fakeExtension struct {
	extension.Extension
}

func (*fakeExtension) RegisterRoutes(*http.ServeMux) error {
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
		require.ErrorIs(t, err, ErrExtensionNotConfigured)
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

	req := httptest.NewRequest(http.MethodGet, "/api/ai-analysis/capabilities", http.NoBody)
	w := httptest.NewRecorder()

	ext.handleCapabilities(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result httpapi.SuccessResponse[CapabilitiesResponse]
	err := json.NewDecoder(w.Body).Decode(&result)
	require.NoError(t, err)
	assert.True(t, result.Data.Features.NLSearch)
	assert.True(t, result.Data.Features.SpanExplanation)
	assert.True(t, result.Data.Features.SmartFilter)
	assert.True(t, result.Data.Streaming)
	assert.Equal(t, "ollama", result.Data.Provider)
	assert.Equal(t, "qwen2.5:1.5b", result.Data.Model)
	assert.Equal(t, int64(30_000), result.Data.RequestTimeoutMS)
	assert.Equal(t, int64(256*1024), result.Data.MaxInputBytes)
}

func TestRegisterRoutes(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	router := http.NewServeMux()
	err := ext.RegisterRoutes(router)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/ai-analysis/capabilities", http.NoBody)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRegisterRoutesValidation(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	err := ext.RegisterRoutes(nil)
	require.ErrorContains(t, err, "router is required")
}

func TestRegisterRoutesFeatureGate(t *testing.T) {
	cfg := createDefaultConfig().(*Config)
	cfg.Features.NLSearch = false
	cfg.Features.SpanExplanation = false
	cfg.Features.SmartFilter = false
	ext := newAIAnalysisExtension(cfg, component.TelemetrySettings{})

	router := http.NewServeMux()
	err := ext.RegisterRoutes(router)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/ai-analysis/nl-search/parse", http.NoBody)
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
