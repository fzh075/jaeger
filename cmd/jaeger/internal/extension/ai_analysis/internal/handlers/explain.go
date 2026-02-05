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
)

// ExplainHandler handles span explanation requests.
type ExplainHandler struct {
	chain  *chains.ExplainerChain
	logger *zap.Logger
}

// NewExplainHandler creates a new explain handler.
func NewExplainHandler(provider llm.Provider, logger *zap.Logger) *ExplainHandler {
	return &ExplainHandler{
		chain:  chains.NewExplainerChain(provider),
		logger: logger,
	}
}

// Handle processes non-streaming span explanation requests.
func (h *ExplainHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req types.SpanExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.SpanData.SpanID == "" {
		h.sendError(w, http.StatusBadRequest, "span_data.span_id is required")
		return
	}

	h.logger.Info("Processing span explanation request",
		zap.String("span_id", req.SpanData.SpanID),
		zap.String("service", req.SpanData.ServiceName))

	response, err := h.chain.Explain(r.Context(), req)
	if err != nil {
		h.logger.Error("Span explanation failed", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "Failed to explain span: "+err.Error())
		return
	}

	if response.Error != "" {
		h.logger.Warn("Span explanation returned error", zap.String("error", response.Error))
	}

	h.sendJSON(w, http.StatusOK, response)
}

// HandleStream processes streaming span explanation requests using SSE.
func (h *ExplainHandler) HandleStream(w http.ResponseWriter, r *http.Request) {
	var req types.SpanExplainRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if req.SpanData.SpanID == "" {
		h.sendError(w, http.StatusBadRequest, "span_data.span_id is required")
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

	h.logger.Info("Processing streaming span explanation request",
		zap.String("span_id", req.SpanData.SpanID))

	err := h.chain.ExplainStream(r.Context(), req, func(chunk string) error {
		_, err := fmt.Fprintf(w, "data: %s\n\n", chunk)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	})

	if err != nil {
		h.logger.Error("Streaming explanation failed", zap.Error(err))
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", err.Error())
		flusher.Flush()
		return
	}

	fmt.Fprint(w, "event: done\ndata: {}\n\n")
	flusher.Flush()
}

func (h *ExplainHandler) sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *ExplainHandler) sendError(w http.ResponseWriter, status int, message string) {
	h.sendJSON(w, status, map[string]string{"error": message})
}
