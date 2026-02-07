// Copyright (c) 2026 The Jaeger Authors.
// SPDX-License-Identifier: Apache-2.0

package aianalysis

import (
	"context"
	"errors"
	"testing"

	"github.com/gorilla/mux"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/component/componenttest"
	"go.opentelemetry.io/collector/extension"

	"github.com/jaegertracing/jaeger/cmd/jaeger/internal/extension/jaegerquery/querysvc"
)

type fakeExtension struct {
	extension.Extension
}

func (*fakeExtension) RegisterRoutes(*mux.Router, *querysvc.QueryService) error {
	return nil
}

type fakeHost struct {
	component.Host
	ext component.Component
}

type wrongComponent struct{}

func (wrongComponent) Start(context.Context, component.Host) error {
	return nil
}

func (wrongComponent) Shutdown(context.Context) error {
	return nil
}

func (h *fakeHost) GetExtensions() map[component.ID]component.Component {
	if h.ext == nil {
		return map[component.ID]component.Component{}
	}
	return map[component.ID]component.Component{
		ID: h.ext,
	}
}

func TestGetExtension(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		host := &fakeHost{
			Host: componenttest.NewNopHost(),
			ext:  &fakeExtension{},
		}
		ext, err := GetExtension(host)
		require.NoError(t, err)
		require.NotNil(t, ext)
	})

	t.Run("not found", func(t *testing.T) {
		host := &fakeHost{Host: componenttest.NewNopHost()}
		ext, err := GetExtension(host)
		require.Nil(t, ext)
		require.Error(t, err)
		require.True(t, errors.Is(err, ErrExtensionNotFound))
	})

	t.Run("wrong type", func(t *testing.T) {
		host := &fakeHost{
			Host: componenttest.NewNopHost(),
			ext:  wrongComponent{},
		}
		ext, err := GetExtension(host)
		require.Nil(t, ext)
		require.ErrorContains(t, err, "not of expected type")
	})
}
