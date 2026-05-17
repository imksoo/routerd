// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux || !cgo || !libndpi

package main

import (
	"context"

	"routerd/pkg/dpi"
)

func newBackend(options) ndpiBackend {
	return unavailableBackend{}
}

func backendExpectedLoaded() bool {
	return false
}

type unavailableBackend struct{}

func (unavailableBackend) Status() backendStatus {
	return backendStatus{Reason: "libndpi backend is not enabled in this build"}
}

func (unavailableBackend) Classify(_ context.Context, _ string, req dpi.ClassifyRequest, _ *flowState) (dpi.ClassifyResult, error) {
	result := metadataOnlyResult(req)
	result.Engine = "ndpi-agent"
	result.Source = "ndpi-agent"
	result.Reason = "libndpi_backend_unavailable"
	return result, nil
}

func (unavailableBackend) Forget(string) {}

func (unavailableBackend) Close() {}
