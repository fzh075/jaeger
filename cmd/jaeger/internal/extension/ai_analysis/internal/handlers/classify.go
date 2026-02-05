// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package handlers

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/chains"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/llm"
	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/ai_analysis/internal/types"
)

// ClassifyHandler handles span classification requests.
type ClassifyHandler struct {
	chain  *chains.ClassifierChain
	logger *zap.Logger
}

// NewClassifyHandler creates a new classify handler.
func NewClassifyHandler(provider llm.Provider, logger *zap.Logger) *ClassifyHandler {
	return &ClassifyHandler{
		chain:  chains.NewClassifierChain(provider),
		logger: logger,
	}
}

// Handle processes span classification requests.
func (h *ClassifyHandler) Handle(w http.ResponseWriter, r *http.Request) {
	var req types.ClassifyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.sendError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return
	}

	if len(req.Spans) == 0 {
		h.sendError(w, http.StatusBadRequest, "At least one span is required")
		return
	}

	h.logger.Info("Processing span classification request",
		zap.Int("span_count", len(req.Spans)))

	response, err := h.chain.Classify(r.Context(), req.Spans)
	if err != nil {
		h.logger.Error("Span classification failed", zap.Error(err))
		h.sendError(w, http.StatusInternalServerError, "Failed to classify spans: "+err.Error())
		return
	}

	if response.Error != "" {
		h.logger.Warn("Span classification returned error", zap.String("error", response.Error))
	}

	h.sendJSON(w, http.StatusOK, response)
}

func (h *ClassifyHandler) sendJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (h *ClassifyHandler) sendError(w http.ResponseWriter, status int, message string) {
	h.sendJSON(w, status, map[string]string{"error": message})
}
