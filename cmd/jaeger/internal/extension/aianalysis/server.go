// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/extensioncapabilities"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/handlers"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

var (
	_ extension.Extension             = (*server)(nil)
	_ extensioncapabilities.Dependent = (*server)(nil)
)

// server implements the Jaeger AI extension.
type server struct {
	config      *Config
	telset      component.TelemetrySettings
	httpServer  *http.Server
	listener    net.Listener
	llmProvider llm.Provider
	queryAPI    *querysvc.QueryService
}

// newServer creates a new AI server instance.
func newServer(config *Config, telset component.TelemetrySettings) *server {
	return &server{
		config: config,
		telset: telset,
	}
}

// Dependencies implements extensioncapabilities.Dependent to ensure
// this extension starts after the jaegerquery extension.
func (*server) Dependencies() []component.ID {
	return []component.ID{jaegerquery.ID}
}

// Start initializes and starts the AI server.
func (s *server) Start(ctx context.Context, host component.Host) error {
	s.telset.Logger.Info("Starting Jaeger AI server", zap.String("endpoint", s.config.HTTP.NetAddr.Endpoint))

	// Get QueryService from jaegerquery extension
	queryExt, err := jaegerquery.GetExtension(host)
	if err != nil {
		return fmt.Errorf("cannot get %s extension: %w", jaegerquery.ID, err)
	}
	s.queryAPI = queryExt.QueryService()
	s.telset.Logger.Info("Successfully retrieved QueryService from jaegerquery extension")

	// Initialize LLM provider
	s.llmProvider, err = s.createLLMProvider()
	if err != nil {
		return fmt.Errorf("failed to create LLM provider: %w", err)
	}
	s.telset.Logger.Info("LLM provider initialized",
		zap.String("provider", s.config.LLM.Provider))

	// Set up TCP listener
	lc := net.ListenConfig{}
	listener, err := lc.Listen(ctx, "tcp", s.config.HTTP.NetAddr.Endpoint)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", s.config.HTTP.NetAddr.Endpoint, err)
	}
	s.listener = listener

	// Create HTTP server with routes
	mux := s.createRouter()
	s.httpServer = &http.Server{
		Handler:           corsMiddleware(mux),
		ReadHeaderTimeout: 30 * time.Second,
	}

	// Start the server in a goroutine
	go func() {
		if err := s.httpServer.Serve(s.listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.telset.Logger.Error("AI server error", zap.Error(err))
		}
	}()

	s.telset.Logger.Info("Jaeger AI server started successfully",
		zap.String("endpoint", s.config.HTTP.NetAddr.Endpoint),
		zap.Bool("nl_search", s.config.Features.NLSearch),
		zap.Bool("span_explanation", s.config.Features.SpanExplanation),
		zap.Bool("smart_filter", s.config.Features.SmartFilter))
	return nil
}

// Shutdown gracefully stops the AI server.
func (s *server) Shutdown(ctx context.Context) error {
	s.telset.Logger.Info("Shutting down Jaeger AI server")

	var errs []error

	if s.llmProvider != nil {
		if err := s.llmProvider.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close LLM provider: %w", err))
		}
	}

	if s.httpServer != nil {
		if err := s.httpServer.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to shutdown HTTP server: %w", err))
		}
	}

	return errors.Join(errs...)
}

// createLLMProvider creates the appropriate LLM provider based on configuration.
func (s *server) createLLMProvider() (llm.Provider, error) {
	switch s.config.LLM.Provider {
	case "ollama":
		if s.config.LLM.Ollama == nil {
			return nil, errors.New("ollama configuration is required when provider is ollama")
		}
		return llm.NewOllamaProvider(llm.OllamaOptions{
			BaseURL:     s.config.LLM.Ollama.BaseURL,
			Model:       s.config.LLM.Ollama.Model,
			Temperature: s.config.LLM.Ollama.Temperature,
			MaxTokens:   s.config.LLM.Ollama.MaxTokens,
		})
	case "openai":
		if s.config.LLM.OpenAI == nil {
			return nil, errors.New("openai configuration is required when provider is openai")
		}
		return llm.NewOpenAIProvider(llm.OpenAIOptions{
			APIKey:      s.config.LLM.OpenAI.APIKey,
			BaseURL:     s.config.LLM.OpenAI.BaseURL,
			Model:       s.config.LLM.OpenAI.Model,
			Temperature: s.config.LLM.OpenAI.Temperature,
			MaxTokens:   s.config.LLM.OpenAI.MaxTokens,
		})
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", s.config.LLM.Provider)
	}
}

// createRouter sets up HTTP routes for AI endpoints.
func (s *server) createRouter() *http.ServeMux {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", s.handleHealth)

	// Capabilities endpoint
	mux.HandleFunc("/api/ai/capabilities", s.handleCapabilities)

	// Natural language search endpoints
	if s.config.Features.NLSearch {
		nlSearchHandler := handlers.NewNLSearchHandler(s.llmProvider, s.queryAPI, s.telset.Logger)
		mux.HandleFunc("POST /api/ai/search", nlSearchHandler.Handle)
		mux.HandleFunc("POST /api/ai/search/stream", nlSearchHandler.HandleStream)
	}

	// Span explanation endpoints
	if s.config.Features.SpanExplanation {
		explainHandler := handlers.NewExplainHandler(s.llmProvider, s.telset.Logger)
		mux.HandleFunc("POST /api/ai/explain/span", explainHandler.Handle)
		mux.HandleFunc("POST /api/ai/explain/span/stream", explainHandler.HandleStream)
	}

	// Smart filter/classification endpoint
	if s.config.Features.SmartFilter {
		classifyHandler := handlers.NewClassifyHandler(s.llmProvider, s.telset.Logger)
		mux.HandleFunc("POST /api/ai/classify", classifyHandler.Handle)
	}

	return mux
}

// handleHealth responds to health check requests.
func (s *server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"provider": s.config.LLM.Provider,
	})
}

// CapabilitiesResponse represents the AI capabilities response.
type CapabilitiesResponse struct {
	NLSearch        bool   `json:"nl_search"`
	SpanExplanation bool   `json:"span_explanation"`
	SmartFilter     bool   `json:"smart_filter"`
	Streaming       bool   `json:"streaming"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
}

// handleCapabilities returns the enabled AI capabilities.
func (s *server) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
	model := ""
	switch s.config.LLM.Provider {
	case "ollama":
		if s.config.LLM.Ollama != nil {
			model = s.config.LLM.Ollama.Model
		}
	case "openai":
		if s.config.LLM.OpenAI != nil {
			model = s.config.LLM.OpenAI.Model
		}
	}

	response := CapabilitiesResponse{
		NLSearch:        s.config.Features.NLSearch,
		SpanExplanation: s.config.Features.SpanExplanation,
		SmartFilter:     s.config.Features.SmartFilter,
		Streaming:       s.config.Performance.StreamingEnabled,
		Provider:        s.config.LLM.Provider,
		Model:           model,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// corsMiddleware wraps an http.Handler to add CORS headers.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Accept")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
