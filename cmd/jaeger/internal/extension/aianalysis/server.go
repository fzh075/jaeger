// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/handlers"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

var (
	_ extension.Extension = (*server)(nil)
	_ Extension           = (*server)(nil)
)

// server implements the AI Analysis extension.
type server struct {
	config      *Config
	telset      component.TelemetrySettings
	llmProvider llm.Provider
	initErr     error
	initOnce    sync.Once
}

// newServer creates a new AI Analysis instance.
func newServer(config *Config, telset component.TelemetrySettings) *server {
	if telset.Logger == nil {
		telset.Logger = zap.NewNop()
	}
	return &server{
		config: config,
		telset: telset,
	}
}

// Start initializes AI provider.
func (s *server) Start(_ context.Context, _ component.Host) error {
	s.telset.Logger.Info("Starting AI Analysis extension")
	if err := s.ensureLLMProvider(); err != nil {
		return fmt.Errorf("failed to initialize LLM provider: %w", err)
	}
	s.telset.Logger.Info("AI Analysis extension started successfully",
		zap.Bool("nl_search", s.config.Features.NLSearch),
		zap.Bool("span_explanation", s.config.Features.SpanExplanation),
		zap.Bool("smart_filter", s.config.Features.SmartFilter),
	)
	return nil
}

// Shutdown gracefully stops the AI Analysis extension.
func (s *server) Shutdown(_ context.Context) error {
	s.telset.Logger.Info("Shutting down AI Analysis extension")
	if s.llmProvider == nil {
		return nil
	}
	if err := s.llmProvider.Close(); err != nil {
		return fmt.Errorf("failed to close LLM provider: %w", err)
	}
	return nil
}

// RegisterRoutes registers AI Analysis HTTP endpoints into Query router.
func (s *server) RegisterRoutes(router *mux.Router, queryAPI *querysvc.QueryService) error {
	if router == nil {
		return errors.New("router is required")
	}
	if queryAPI == nil {
		return errors.New("query service is required")
	}
	if err := s.ensureLLMProvider(); err != nil {
		return fmt.Errorf("failed to initialize LLM provider: %w", err)
	}

	router.HandleFunc("/api/ai-analysis/capabilities", s.handleCapabilities).Methods(http.MethodGet)

	if s.config.Features.NLSearch {
		nlSearchHandler := handlers.NewNLSearchHandler(s.llmProvider, queryAPI, s.telset.Logger)
		router.HandleFunc("/api/ai-analysis/search", nlSearchHandler.Handle).Methods(http.MethodPost)
		router.HandleFunc("/api/ai-analysis/search/stream", nlSearchHandler.Handle).Methods(http.MethodPost)
	}

	if s.config.Features.SpanExplanation {
		explainHandler := handlers.NewExplainHandler(s.llmProvider, s.telset.Logger)
		router.HandleFunc("/api/ai-analysis/explain/span", explainHandler.Handle).Methods(http.MethodPost)
		router.HandleFunc("/api/ai-analysis/explain/span/stream", explainHandler.HandleStream).Methods(http.MethodPost)
	}

	if s.config.Features.SmartFilter {
		classifyHandler := handlers.NewClassifyHandler(s.llmProvider, s.telset.Logger)
		router.HandleFunc("/api/ai-analysis/classify", classifyHandler.Handle).Methods(http.MethodPost)
	}

	s.telset.Logger.Info("AI Analysis routes registered",
		zap.Bool("nl_search", s.config.Features.NLSearch),
		zap.Bool("span_explanation", s.config.Features.SpanExplanation),
		zap.Bool("smart_filter", s.config.Features.SmartFilter),
	)
	return nil
}

func (s *server) ensureLLMProvider() error {
	s.initOnce.Do(func() {
		s.llmProvider, s.initErr = s.createLLMProvider()
		if s.initErr != nil {
			return
		}
		s.telset.Logger.Info("LLM provider initialized",
			zap.String("provider", s.config.LLM.Provider))
	})
	return s.initErr
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

// CapabilitiesResponse represents the AI Analysis capabilities response.
type CapabilitiesResponse struct {
	NLSearch        bool   `json:"nl_search"`
	SpanExplanation bool   `json:"span_explanation"`
	SmartFilter     bool   `json:"smart_filter"`
	Streaming       bool   `json:"streaming"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
}

// handleCapabilities returns the enabled AI Analysis capabilities.
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
