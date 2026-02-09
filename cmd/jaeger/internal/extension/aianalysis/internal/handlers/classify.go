// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"context"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/chains"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/httpapi"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

// ClassifyHandler handles span classification requests.
type ClassifyHandler struct {
	chain *chains.ClassifierChain
	base  baseHandler
}

// NewClassifyHandler creates a new classify handler.
func NewClassifyHandler(provider llm.Provider, logger *zap.Logger, options ...HandlerOptions) *ClassifyHandler {
	var opt HandlerOptions
	if len(options) > 0 {
		opt = options[0]
	}
	return &ClassifyHandler{
		chain: chains.NewClassifierChain(provider),
		base:  newBaseHandler(provider, logger, opt),
	}
}

// Handle processes span classification requests.
func (h *ClassifyHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if !h.base.tryAcquire(w) {
		return
	}
	defer h.base.release()

	var req types.ClassifyRequest
	if ok := h.base.decodeJSON(w, r, &req); !ok {
		return
	}

	if len(req.Spans) == 0 {
		httpapi.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "at least one span is required", nil)
		return
	}
	if len(req.Spans) > h.base.options.MaxSpansPerClassify {
		httpapi.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "too many spans in one request", map[string]any{
			"span_count":             len(req.Spans),
			"max_spans_per_classify": h.base.options.MaxSpansPerClassify,
		})
		return
	}

	start := time.Now()
	ctx, cancel := h.base.withTimeout(r.Context())
	defer cancel()

	h.base.logger.Info("Processing span classification request",
		zap.Int("span_count", len(req.Spans)),
	)

	var response types.ClassifyResponse
	err := h.base.runWithRetry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = h.chain.Classify(callCtx, req.Spans)
		return callErr
	})
	if err != nil {
		h.base.logger.Error("Span classification failed", zap.Error(err))
		h.base.writeErrorFromErr(w, err, "Failed to classify spans")
		return
	}

	httpapi.WriteData(w, http.StatusOK, response, h.base.responseMeta(start))
}
