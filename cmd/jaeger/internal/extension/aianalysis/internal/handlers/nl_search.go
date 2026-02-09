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

// NLSearchHandler handles natural language search requests.
type NLSearchHandler struct {
	chain *chains.NLSearchChain
	base  baseHandler
}

// NewNLSearchHandler creates a new NL search handler.
func NewNLSearchHandler(provider llm.Provider, logger *zap.Logger, options ...HandlerOptions) *NLSearchHandler {
	var opt HandlerOptions
	if len(options) > 0 {
		opt = options[0]
	}
	return &NLSearchHandler{
		chain: chains.NewNLSearchChain(provider),
		base:  newBaseHandler(provider, logger, opt),
	}
}

// Handle processes NL search requests.
func (h *NLSearchHandler) Handle(w http.ResponseWriter, r *http.Request) {
	if !h.base.tryAcquire(w) {
		return
	}
	defer h.base.release()

	var req types.NLSearchRequest
	if ok := h.base.decodeJSON(w, r, &req); !ok {
		return
	}

	req.Query = strings.TrimSpace(req.Query)
	if req.Query == "" {
		httpapi.WriteError(w, http.StatusBadRequest, errCodeInvalidRequest, "query is required", nil)
		return
	}

	start := time.Now()
	ctx, cancel := h.base.withTimeout(r.Context())
	defer cancel()

	h.base.logger.Info("Processing NL search request", zap.String("query", req.Query))
	var response types.NLSearchResponse
	err := h.base.runWithRetry(ctx, func(callCtx context.Context) error {
		var callErr error
		response, callErr = h.chain.Parse(callCtx, req.Query)
		return callErr
	})
	if err != nil {
		h.base.logger.Error("NL search parsing failed", zap.Error(err))
		h.base.writeErrorFromErr(w, err, "Failed to parse natural language search query")
		return
	}

	httpapi.WriteData(w, http.StatusOK, response, h.base.responseMeta(start))
}
