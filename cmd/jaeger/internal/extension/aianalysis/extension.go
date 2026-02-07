// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"errors"
	"fmt"

	"github.com/gorilla/mux"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

// ErrExtensionNotFound indicates ai_analysis extension is not configured.
var ErrExtensionNotFound = errors.New("cannot find ai_analysis extension")

// Extension is the interface implemented by ai_analysis extension.
type Extension interface {
	extension.Extension
	RegisterRoutes(router *mux.Router, queryAPI *querysvc.QueryService) error
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
		return nil, fmt.Errorf("%w: '%s'", ErrExtensionNotFound, componentType)
	}
	ext, ok := comp.(Extension)
	if !ok {
		return nil, fmt.Errorf("extension '%s' is not of expected type", id)
	}
	return ext, nil
}
