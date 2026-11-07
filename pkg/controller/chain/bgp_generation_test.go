// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"strings"
	"testing"

	bgpcontroller "github.com/imksoo/routerd/pkg/controller/bgp"
)

func TestGenerationCloseAcknowledgesActualBGPWatchAndRetiresActor(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	actor := &bgpcontroller.Controller{}
	done := actor.Start(ctx)
	generation := &controllerGeneration{ctx: ctx, cancel: cancel, lifetimes: controllerLifetimes{bgp: actor}}
	runner := &Runner{}
	if err := runner.closeControllerGeneration(generation); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	default:
		t.Fatal("generation released before its actual BGP watch exit")
	}
	if err := actor.Reconcile(context.Background()); err == nil || !strings.Contains(err.Error(), "generation has stopped") {
		t.Fatalf("old kernel owner remained admissible: %v", err)
	}
}
