// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/chains"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/httpapi"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

const (
	defaultRequestTimeout      = 30 * time.Second
	defaultMaxRequestBodyBytes = 256 * 1024
	defaultMaxSpansPerClassify = 200
	defaultMaxConcurrency      = 16
)

const (
	errCodeInvalidRequest       = "invalid_request"
	errCodeRequestBodyTooLarge  = "request_body_too_large"
	errCodeTooManyRequests      = "too_many_requests"
	errCodeStreamingNotEnabled  = "streaming_not_enabled"
	errCodeStreamingUnsupported = "streaming_not_supported"
	errCodeRequestTimeout       = "request_timeout"
	errCodeRequestCanceled      = "request_canceled"
	errCodeLLMUnavailable       = "llm_unavailable"
	errCodeLLMGenerationFailed  = "llm_generation_failed"
	errCodeLLMInvalidResponse   = "llm_invalid_response"
	errCodeInternal             = "internal_error"
)

// HandlerOptions controls reliability limits for HTTP handlers.
type HandlerOptions struct {
	ProviderName         string
	RequestTimeout       time.Duration
	MaxRequestBodyBytes  int64
	MaxSpansPerClassify  int
	MaxConcurrentRequest int
	RetryAttempts        int
	StreamingEnabled     bool
	ConcurrencyLimiter   chan struct{}
	NLSearchCandidates   NLSearchCandidatesProvider
}

// NLSearchCandidatesProvider enriches or overrides NLSearch candidates per request.
type NLSearchCandidatesProvider interface {
	Enrich(context.Context, types.NLSearchRequest) (types.NLSearchCandidates, error)
}

type baseHandler struct {
	provider  llm.Provider
	logger    *zap.Logger
	options   HandlerOptions
	semaphore chan struct{}
}

func newBaseHandler(provider llm.Provider, logger *zap.Logger, options HandlerOptions) baseHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = defaultRequestTimeout
	}
	if options.MaxRequestBodyBytes <= 0 {
		options.MaxRequestBodyBytes = defaultMaxRequestBodyBytes
	}
	if options.MaxSpansPerClassify <= 0 {
		options.MaxSpansPerClassify = defaultMaxSpansPerClassify
	}
	if options.MaxConcurrentRequest <= 0 {
		options.MaxConcurrentRequest = defaultMaxConcurrency
	}
	if options.RetryAttempts < 0 {
		options.RetryAttempts = 0
	}

	semaphore := options.ConcurrencyLimiter
	if semaphore == nil {
		semaphore = make(chan struct{}, options.MaxConcurrentRequest)
	}
	return baseHandler{
		provider:  provider,
		logger:    logger,
		options:   options,
		semaphore: semaphore,
	}
}

func (h *baseHandler) tryAcquire(w http.ResponseWriter) bool {
	select {
	case h.semaphore <- struct{}{}:
		return true
	default:
		httpapi.WriteError(w, http.StatusTooManyRequests, errCodeTooManyRequests, "Too many concurrent AI requests", map[string]any{
			"max_concurrency": cap(h.semaphore),
		})
		return false
	}
}

func (h *baseHandler) release() {
	select {
	case <-h.semaphore:
	default:
	}
}

func (h *baseHandler) withTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, h.options.RequestTimeout)
}

func (h *baseHandler) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, h.options.MaxRequestBodyBytes)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			httpapi.WriteError(w, http.StatusRequestEntityTooLarge, errCodeRequestBodyTooLarge, "Request body too large", map[string]any{
				"max_request_body_bytes": h.options.MaxRequestBodyBytes,
			})
			return false
		}
		httpapi.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "Invalid request body", map[string]any{
			"cause": err.Error(),
		})
		return false
	}

	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		httpapi.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "Request body must contain a single JSON object", nil)
		return false
	}
	return true
}

func (h *baseHandler) responseMeta(start time.Time) *httpapi.Meta {
	model := ""
	if h.provider != nil {
		model = h.provider.Model()
	}
	return &httpapi.Meta{
		Provider:  h.options.ProviderName,
		Model:     model,
		LatencyMS: time.Since(start).Milliseconds(),
	}
}

func (h *baseHandler) writeErrorFromErr(w http.ResponseWriter, err error, message string) {
	_ = h

	if errors.Is(err, context.DeadlineExceeded) {
		httpapi.WriteError(w, http.StatusGatewayTimeout, errCodeRequestTimeout, "AI request timed out", nil)
		return
	}
	if errors.Is(err, context.Canceled) {
		httpapi.WriteError(w, http.StatusRequestTimeout, errCodeRequestCanceled, "AI request was canceled", nil)
		return
	}
	if errors.Is(err, chains.ErrProviderUnavailable) {
		httpapi.WriteError(w, http.StatusServiceUnavailable, errCodeLLMUnavailable, "LLM provider is unavailable", nil)
		return
	}
	if errors.Is(err, chains.ErrLLMGeneration) {
		httpapi.WriteError(w, http.StatusBadGateway, errCodeLLMGenerationFailed, "LLM generation failed", map[string]any{
			"cause": err.Error(),
		})
		return
	}
	if errors.Is(err, chains.ErrInvalidLLMResponse) {
		httpapi.WriteError(w, http.StatusBadGateway, errCodeLLMInvalidResponse, "LLM returned an invalid response", map[string]any{
			"cause": err.Error(),
		})
		return
	}

	if message == "" {
		message = "Internal AI analysis error"
	}
	httpapi.WriteError(w, http.StatusInternalServerError, errCodeInternal, message, map[string]any{
		"cause": err.Error(),
	})
}

func (h *baseHandler) runWithRetry(ctx context.Context, operation func(context.Context) error) error {
	attempts := h.options.RetryAttempts + 1
	if attempts < 1 {
		attempts = 1
	}

	var err error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		err = operation(ctx)
		if err == nil {
			return nil
		}
		if !h.shouldRetry(err) || i == attempts-1 {
			return err
		}

		backoff := time.Duration(i+1) * 100 * time.Millisecond
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return err
}

func (h *baseHandler) shouldRetry(err error) bool {
	_ = h

	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return false
	}
	return errors.Is(err, chains.ErrLLMGeneration)
}

func (h *baseHandler) writeSSEEvent(w http.ResponseWriter, event string, payload any) error {
	_ = h

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal SSE payload: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, string(data)); err != nil {
		return err
	}
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	return nil
}

func (h *baseHandler) writeSSEError(w http.ResponseWriter, code, message string, details map[string]any) {
	_ = h.writeSSEEvent(w, "error", httpapi.ErrorResponse{
		Error: httpapi.APIError{
			Code:    code,
			Message: message,
			Details: details,
		},
	})
}
