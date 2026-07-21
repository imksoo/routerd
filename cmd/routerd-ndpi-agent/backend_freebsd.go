// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package main

import (
	"context"

	"github.com/imksoo/routerd/pkg/dpi"
)

// FreeBSD packages may provide command-line nDPI tools, but routerd's native
// libndpi agent ABI is currently Linux-only.  Keep that capability explicit so
// a FreeBSD build cannot be mistaken for a loaded native classifier.
func newBackend(options) ndpiBackend { return freeBSDUnavailableBackend{} }

func backendExpectedLoaded() bool { return false }

type freeBSDUnavailableBackend struct{}

func (freeBSDUnavailableBackend) Status() backendStatus {
	return backendStatus{Reason: "libndpi backend is not supported on FreeBSD builds"}
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
