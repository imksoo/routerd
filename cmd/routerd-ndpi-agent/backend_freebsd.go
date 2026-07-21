// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd && (!cgo || !libndpi)

package main

import (
	"context"

	"github.com/imksoo/routerd/pkg/dpi"
)

// Keep the optional capability explicit in default FreeBSD builds. Builds made
// with CGO and the libndpi tag use the native backend instead.
func newBackend(options) ndpiBackend { return freeBSDUnavailableBackend{} }

func backendExpectedLoaded() bool { return false }

type freeBSDUnavailableBackend struct{}

func (freeBSDUnavailableBackend) Status() backendStatus {
	return backendStatus{Reason: "libndpi backend is not enabled in this FreeBSD build"}
}

func (freeBSDUnavailableBackend) Classify(_ context.Context, _ string, req dpi.ClassifyRequest, _ *flowState) (dpi.ClassifyResult, error) {
	result := metadataOnlyResult(req)
	result.Engine = "ndpi-agent"
	result.Source = "ndpi-agent"
	result.Reason = "libndpi_backend_unavailable_freebsd"
	return result, nil
}

func (freeBSDUnavailableBackend) Forget(string) {}
func (freeBSDUnavailableBackend) Close()        {}
