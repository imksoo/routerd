// SPDX-License-Identifier: BSD-3-Clause

package framework

import (
	"context"
	"testing"

	"github.com/imksoo/routerd/pkg/daemonapi"
)

func TestBootstrapCallbackPreservesDefaultAndNamedEventPaths(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "dedicated"}[dedicated], func(t *testing.T) {
			bootstraps, events := 0, 0
			controller := FuncController{
				ControllerName: "fixture",
				ReconcileFunc:  func(context.Context, daemonapi.DaemonEvent) error { events++; return nil },
			}
			if dedicated {
				controller.BootstrapFunc = func(context.Context) error { bootstraps++; return nil }
			}
			if err := (Runner{}).Bootstrap(t.Context(), controller); err != nil {
				t.Fatal(err)
			}
			if err := controller.Reconcile(t.Context(), daemonapi.DaemonEvent{Type: "routerd.controller.bootstrap"}); err != nil {
				t.Fatal(err)
			}
			wantBootstraps, wantEvents := 0, 2
			if dedicated {
				wantBootstraps, wantEvents = 1, 1
			}
			if bootstraps != wantBootstraps || events != wantEvents {
				t.Fatalf("bootstrap=%d events=%d, want bootstrap=%d events=%d", bootstraps, events, wantBootstraps, wantEvents)
			}
		})
	}
}
