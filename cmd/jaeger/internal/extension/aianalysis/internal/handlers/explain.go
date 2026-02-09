// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"net/http"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/chains"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/httpapi"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

// ExplainHandler handles span explanation requests.
type ExplainHandler struct {
	chain *chains.ExplainerChain
	base  baseHandler
}

// NewExplainHandler creates a new explain handler.
func NewExplainHandler(provider llm.Provider, logger *zap.Logger, options ...HandlerOptions) *ExplainHandler {
	var opt HandlerOptions
	if len(options) > 0 {
		opt = options[0]
	}
	return &ExplainHandler{
		chain: chains.NewExplainerChain(provider),
		base:  newBaseHandler(provider, logger, opt),
	}
}

// Handle processes span explanation requests.
// For streaming responses, clients should send Accept: text/event-stream.
func (h *ExplainHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if !h.base.tryAcquire(w) {
		return
	}
	defer h.base.release()

	var req types.SpanExplainRequest
	if ok := h.base.decodeJSON(w, r, &req); !ok {
		return
	}

	req.SpanData.SpanID = strings.TrimSpace(req.SpanData.SpanID)
	if req.SpanData.SpanID == "" {
		httpapi.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "span_data.span_id is required", nil)
		return
	}

	start := time.Now()
	ctx, cancel := h.base.withTimeout(r.Context())
	defer cancel()

	h.base.logger.Info("Processing span explanation request",
		zap.String("span_id", req.SpanData.SpanID),
		zap.String("service", req.SpanData.ServiceName),
	)

	wantsStream := strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
	if wantsStream {
		if !h.base.options.StreamingEnabled {
			httpapi.WriteError(w, http.StatusBadRequest, errCodeStreamingNotEnabled, "streaming is disabled by configuration", nil)
			return
		}
		h.handleStream(ctx, w, req, start)
		return
	}

	var response types.SpanExplainResponse
	err := h.base.runWithRetry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = h.chain.Explain(callCtx, req)
		return callErr
	})
	if err != nil {
		h.base.logger.Error("Span explanation failed", zap.Error(err))
		h.base.writeErrorFromErr(w, err, "Failed to explain span")
		return
	}

	httpapi.WriteData(w, http.StatusOK, response, h.base.responseMeta(start))
}

func (h *ExplainHandler) handleStream(ctx context.Context, w http.ResponseWriter, req types.SpanExplainRequest, start time.Time) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		httpapi.WriteError(w, http.StatusInternalServerError, errCodeStreamingUnsupported, "streaming is not supported by this response writer", nil)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	err := h.chain.ExplainStream(ctx, req, func(chunk string) error {
		return h.base.writeSSEEvent(w, "message", map[string]string{"chunk": chunk})
	})
	if err != nil {
		h.base.logger.Error("Streaming explanation failed", zap.Error(err))
		h.base.writeSSEError(w, errCodeLLMGenerationFailed, "Failed to stream span explanation", map[string]any{
			"cause": err.Error(),
		})
		flusher.Flush()
		return
	}

	_ = h.base.writeSSEEvent(w, "done", map[string]any{
		"meta": h.base.responseMeta(start),
	})
	flusher.Flush()
}
