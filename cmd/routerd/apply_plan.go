// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// planApply reads current observations and computes intent without running
// controllers, journaling simulated events, or opening a writable state DB.
// Observations can change during evaluation; apply observes again before effects.
func planApply(ctx context.Context, router *api.Router, opts applyOptions) (*apply.Result, error) {
	store, err := routerstate.LoadReadOnlyLive(defaultString(opts.StatePath, defaultStatePath))
	if err != nil {
		return nil, fmt.Errorf("read plan state: %w", err)
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	observations, ok := store.(controllerchain.RuntimeObservationStore)
	if !ok {
		return nil, fmt.Errorf("plan state does not expose resource observations")
	}
	view, err := controllerchain.ObserveRouterConfiguration(router, observations, time.Now().UTC(), platformDefaults.OS)
	if err != nil {
		return nil, err
	}
	result, err := apply.New(ctx).PlanEffective(view.RouteRouter, view.DeclaredRouter)
	if err != nil {
		return nil, err
	}
	appendPrefixDelegationStateWarnings(result, view.EffectiveRouter, store)
	if err := appendLedgerOwnedOrphans(ctx, result, view.RouteRouter, view.DeclaredRouter, opts.LedgerPath, true); err != nil {
		return nil, err
	}
	return result, nil
}
