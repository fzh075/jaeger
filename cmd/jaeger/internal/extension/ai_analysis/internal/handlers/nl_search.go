// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"

	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/chains"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/types"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

// NLSearchHandler handles natural language search requests.
type NLSearchHandler struct {
	chain        *chains.NLSearchChain
	queryService *querysvc.QueryService
	logger       *zap.Logger
}

// NewNLSearchHandler creates a new NL search handler.
func NewNLSearchHandler(provider llm.Provider, queryService *querysvc.QueryService, logger *zap.Logger) *NLSearchHandler {
	return &NLSearchHandler{
		chain:        chains.NewNLSearchChain(provider),
		queryService: queryService,
		logger:       logger,
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

// HandleStream processes streaming NL search requests using SSE.
func (h *NLSearchHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	var req types.NLSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.Query == "" {
		h.sendError(w, http.StatusBadRequest, "Query is required")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.sendError(w, http.StatusInternalServerError, "Streaming not supported")
		return
	}

	h.logger.Info("Processing streaming NL search request", zap.String("query", req.Query))

	err := h.chain.ParseStream(r.Context(), req.Query, func(chunk string) error {
		_, err := fmt.Fprintf(w, "data: %s\n\n", chunk)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})

	if err != nil {
		h.logger.Error("Streaming NL search failed", zap.Error(err))
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	// Send done event
	fmt.Fprint(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (h *NLSearchHandler) sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *NLSearchHandler) sendError(w http.ResponseWriter, status int, message string) {
	h.sendJSON(w, status, map[string]string{"error": message})
}
