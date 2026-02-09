// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package chains

import "errors"

var (
	// ErrProviderUnavailable indicates LLM provider is not configured.
	ErrProviderUnavailable = errors.New("llm provider unavailable")
	// ErrLLMGeneration indicates model generation failed.
	ErrLLMGeneration = errors.New("llm generation failed")
	// ErrInvalidLLMResponse indicates model output cannot be parsed to expected schema.
	ErrInvalidLLMResponse = errors.New("invalid llm response")
)
