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
	declared, err := controllerchain.BuildDynamicRouteSAMEffectiveRouter(router, store, time.Now().UTC(), platformDefaults.OS)
	if err != nil {
		return nil, err
	}
	effective, err := filterRouterByWhen(declared, store)
	if err != nil {
		return nil, err
	}
	result, err := apply.New(ctx).PlanEffective(effective, declared)
	if err != nil {
		return nil, err
	}
	appendPrefixDelegationStateWarnings(result, declared, store)
	if err := appendLedgerOwnedOrphans(ctx, result, effective, declared, opts.LedgerPath, true); err != nil {
		return nil, err
	}
	return result, nil
}
