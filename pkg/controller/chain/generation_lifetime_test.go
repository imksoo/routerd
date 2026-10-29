// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
)

func TestGenerationPreparationFailureWaitsForBothAuxiliaryOwners(t *testing.T) {
	for _, stage := range []string{"builder", "bootstrap"} {
		t.Run(stage, func(t *testing.T) {
			failed := errors.New("prepare rejected")
			canceled := make(chan struct{})
			eventDone, derivedDone := make(chan struct{}), make(chan struct{})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			runner := &Runner{Router: staticPreparationRouter("accepted"), Store: mapStore{}, Bus: bus.New()}
			runner.generationBuilder = func(ctx context.Context, _ *slog.Logger, _ eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
				go func() { <-ctx.Done(); close(canceled) }()
				owners := controllerLifetimes{eventRuleDone: eventDone, derivedDone: derivedDone}
				if stage == "builder" {
					return nil, DaemonStatusController{}, owners, failed
				}
				return []framework.Controller{framework.FuncController{ControllerName: "canceled-bootstrap", PeriodicFunc: func(context.Context) (bool, error) {
					// Ordinary reconcile errors are retryable. Cancellation
					// during the actual bootstrap makes preparation fail.
					cancel()
					return false, failed
				}}}, DaemonStatusController{}, owners, nil
			}
			result := make(chan error, 1)
			go func() {
				_, err := runner.prepareControllerGeneration(ctx, slog.Default(), eventedStore{Store: runner.Store, Bus: runner.Bus, Router: runner.Router})
				result <- err
			}()
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("failed preparation did not cancel its owners")
			}
			close(eventDone)
			select {
			case err := <-result:
				t.Fatalf("preparation returned before derived owner exited: %v", err)
			default:
			}
			close(derivedDone)
			select {
			case err := <-result:
				if err == nil {
					t.Fatal("failed preparation returned success")
				}
			case <-time.After(time.Second):
				t.Fatal("preparation did not acknowledge both completed owners")
			}
		})
	}
}
