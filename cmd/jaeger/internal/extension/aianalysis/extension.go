// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.opentelemetry.io/collector/extension/extensioncapabilities"
	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/handlers"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/httpapi"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

// ErrExtensionNotConfigured indicates ai_analysis extension is not configured.
var ErrExtensionNotConfigured = errors.New("cannot find ai_analysis extension")

// Extension is the interface implemented by ai_analysis extension.
type Extension interface {
	extension.Extension
	RegisterRoutes(router *mux.Router) error
}

var (
	_ extension.Extension             = (*aiAnalysisExtension)(nil)
	_ extensioncapabilities.Dependent = (*aiAnalysisExtension)(nil)
	_ Extension                       = (*aiAnalysisExtension)(nil)
)

var jaegerQueryExtensionID = component.NewID(component.MustNewType("jaeger_query"))

type queryServiceAccessor interface {
	QueryService() *querysvc.QueryService
}

// aiAnalysisExtension implements the AI Analysis extension.
type aiAnalysisExtension struct {
	config             *Config
	telset             component.TelemetrySettings
	llmProvider        llm.Provider
	requestSem         chan struct{}
	initErr            error
	initOnce           sync.Once
	queryServiceSource queryServiceAccessor
}

// newAIAnalysisExtension creates a new AI Analysis extension instance.
func newAIAnalysisExtension(config *Config, telset component.TelemetrySettings) *aiAnalysisExtension {
	if telset.Logger == nil {
		telset.Logger = zap.NewNop()
	}
	maxConcurrent := config.Performance.MaxConcurrentRequests
	if maxConcurrent <= 0 {
		maxConcurrent = 16
	}
	return &aiAnalysisExtension{
		config:     config,
		telset:     telset,
		requestSem: make(chan struct{}, maxConcurrent),
	}
}

// Dependencies implements extensioncapabilities.Dependent.
func (*aiAnalysisExtension) Dependencies() []component.ID {
	return []component.ID{jaegerQueryExtensionID}
}

// Start initializes AI provider.
func (s *aiAnalysisExtension) Start(_ context.Context, host component.Host) error {
	s.telset.Logger.Info("Starting AI Analysis extension")
	s.queryServiceSource = resolveQueryServiceAccessor(host)
	if s.queryServiceSource == nil {
		s.telset.Logger.Warn("jaeger_query extension not available; NL tag candidates will use fallback set")
	}
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
func (s *aiAnalysisExtension) Shutdown(_ context.Context) error {
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
func (s *aiAnalysisExtension) RegisterRoutes(router *mux.Router) error {
	if router == nil {
		return errors.New("router is required")
	}
	if err := s.ensureLLMProvider(); err != nil {
		return fmt.Errorf("failed to initialize LLM provider: %w", err)
	}

	router.HandleFunc("/api/ai-analysis/capabilities", s.handleCapabilities).Methods(http.MethodGet)

	opts := handlers.HandlerOptions{
		ProviderName:         s.config.LLM.Provider,
		RequestTimeout:       s.config.Performance.RequestTimeout,
		MaxRequestBodyBytes:  s.config.Performance.MaxRequestBodyBytes,
		MaxSpansPerClassify:  s.config.Performance.MaxSpansPerClassify,
		MaxConcurrentRequest: s.config.Performance.MaxConcurrentRequests,
		RetryAttempts:        s.config.Performance.RetryAttempts,
		StreamingEnabled:     s.config.Performance.StreamingEnabled,
		ConcurrencyLimiter:   s.requestSem,
		NLSearchCandidates: handlers.NewQueryBackedNLSearchCandidatesProvider(
			s,
			s.telset.Logger,
		),
	}

	if s.config.Features.NLSearch {
		nlSearchHandler := handlers.NewNLSearchHandler(s.llmProvider, s.telset.Logger, opts)
		router.HandleFunc("/api/ai-analysis/nl-search/parse", nlSearchHandler.Handle).Methods(http.MethodPost)
	}

	if s.config.Features.SpanExplanation {
		explainHandler := handlers.NewExplainHandler(s.llmProvider, s.telset.Logger, opts)
		router.HandleFunc("/api/ai-analysis/spans/explain", explainHandler.Handle).Methods(http.MethodPost)
	}

	if s.config.Features.SmartFilter {
		classifyHandler := handlers.NewClassifyHandler(s.llmProvider, s.telset.Logger, opts)
		router.HandleFunc("/api/ai-analysis/spans/classify", classifyHandler.Handle).Methods(http.MethodPost)
	}

	s.telset.Logger.Info("AI Analysis routes registered",
		zap.Bool("nl_search", s.config.Features.NLSearch),
		zap.Bool("span_explanation", s.config.Features.SpanExplanation),
		zap.Bool("smart_filter", s.config.Features.SmartFilter),
	)
	return nil
}

func (s *aiAnalysisExtension) QueryService() *querysvc.QueryService {
	if s.queryServiceSource == nil {
		return nil
	}
	return s.queryServiceSource.QueryService()
}

func resolveQueryServiceAccessor(host component.Host) queryServiceAccessor {
	if host == nil {
		return nil
	}
	for id, ext := range host.GetExtensions() {
		if id.Type() != jaegerQueryExtensionID.Type() {
			continue
		}
		accessor, ok := ext.(queryServiceAccessor)
		if !ok {
			return nil
		}
		return accessor
	}
	return nil
}

func (s *aiAnalysisExtension) ensureLLMProvider() error {
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
func (s *aiAnalysisExtension) createLLMProvider() (llm.Provider, error) {
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
	case "anthropic":
		if s.config.LLM.Anthropic == nil {
			return nil, errors.New("anthropic configuration is required when provider is anthropic")
		}
		return llm.NewAnthropicProvider(llm.AnthropicOptions{
			APIKey:      s.config.LLM.Anthropic.APIKey,
			BaseURL:     s.config.LLM.Anthropic.BaseURL,
			Model:       s.config.LLM.Anthropic.Model,
			Temperature: s.config.LLM.Anthropic.Temperature,
			MaxTokens:   s.config.LLM.Anthropic.MaxTokens,
		})
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", s.config.LLM.Provider)
	}
}

// FeatureFlags represents enabled AI features.
type FeatureFlags struct {
	NLSearch        bool `json:"nl_search"`
	SpanExplanation bool `json:"span_explanation"`
	SmartFilter     bool `json:"smart_filter"`
}

// CapabilitiesResponse represents the AI Analysis capabilities response.
type CapabilitiesResponse struct {
	Features         FeatureFlags `json:"features"`
	Streaming        bool         `json:"streaming"`
	Provider         string       `json:"provider"`
	Model            string       `json:"model"`
	RequestTimeoutMS int64        `json:"request_timeout_ms"`
	MaxInputBytes    int64        `json:"max_input_bytes"`
}

// handleCapabilities returns the enabled AI Analysis capabilities.
func (s *aiAnalysisExtension) handleCapabilities(w http.ResponseWriter, _ *http.Request) {
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
	case "anthropic":
		if s.config.LLM.Anthropic != nil {
			model = s.config.LLM.Anthropic.Model
		}
	default:
		// Keep an empty model string for unsupported providers.
	}

	response := CapabilitiesResponse{
		Features: FeatureFlags{
			NLSearch:        s.config.Features.NLSearch,
			SpanExplanation: s.config.Features.SpanExplanation,
			SmartFilter:     s.config.Features.SmartFilter,
		},
		Streaming:        s.config.Performance.StreamingEnabled,
		Provider:         s.config.LLM.Provider,
		Model:            model,
		RequestTimeoutMS: s.config.Performance.RequestTimeout.Milliseconds(),
		MaxInputBytes:    s.config.Performance.MaxRequestBodyBytes,
	}

	httpapi.WriteData(w, http.StatusOK, response, &httpapi.Meta{
		Provider: s.config.LLM.Provider,
		Model:    model,
	})
}

// GetExtension retrieves the ai_analysis extension from the host.
func GetExtension(host component.Host) (Extension, error) {
	var id component.ID
	var comp component.Component
	for i, ext := range host.GetExtensions() {
		if i.Type() == componentType {
			id, comp = i, ext
			break
		}
	}
	if comp == nil {
		return nil, fmt.Errorf("%w: '%s'", ErrExtensionNotConfigured, componentType)
	}
	ext, ok := comp.(Extension)
	if !ok {
		return nil, fmt.Errorf("extension '%s' is not of expected type", id)
	}
	return ext, nil
}
