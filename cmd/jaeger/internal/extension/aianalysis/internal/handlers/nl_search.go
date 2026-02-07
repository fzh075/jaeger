// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/chains"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/aianalysis/internal/types"
)

// NLSearchHandler handles natural language search requests.
type NLSearchHandler struct {
	chain  *chains.NLSearchChain
	logger *zap.Logger
}

// NewNLSearchHandler creates a new NL search handler.
func NewNLSearchHandler(provider llm.Provider, logger *zap.Logger) *NLSearchHandler {
	return &NLSearchHandler{
		chain:  chains.NewNLSearchChain(provider),
		logger: logger,
	}
}

// Handle processes non-streaming NL search requests.
func (h *NLSearchHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req types.NLSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		h.sendError(w, http.StatusBadRequest, "Query is required")
		return
	}

	h.logger.Info("Processing NL search request", zap.String("query", req.Query))

	response, err := h.chain.Parse(r.Context(), req.Query)
	if err != nil {
		h.logger.Error("NL search parsing failed", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "Failed to parse query: "+err.Error())
		return
	}

	if response.Error != "" {
		h.logger.Warn("NL search returned error", zap.String("error", response.Error))
	}

	h.sendJSON(w, http.StatusOK, response)
}

func (h *NLSearchHandler) sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *NLSearchHandler) sendError(w http.ResponseWriter, status int, message string) {
	h.sendJSON(w, status, map[string]string{"error": message})
}
