// SPDX-License-Identifier: BSD-3-Clause
// Package mobility derives BGP /32 mobility paths and provider trap action
// plans from MobilityPool intent and federation observed facts.
package mobility

import (
	"context"
	"fmt"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

// reconcileBGPDelivery is intentionally only orchestration: collect one typed
// snapshot, run the functional core, and apply its outputs.
type poolReconcileResult struct {
	Source  string
	Pending bool
}

func (c Controller) reconcileBGPDelivery(ctx context.Context, res api.Resource, spec api.MobilityPoolSpec, now time.Time) (poolReconcileResult, error) {
	if c.BGPPaths == nil {
		return poolReconcileResult{}, fmt.Errorf("MobilityPool/%s requires routerd-bgp control client", res.Metadata.Name)
	}
	state, err := c.collectPoolReconcileState(res, spec, now)
	if err != nil {
		return poolReconcileResult{}, err
	}
	if state.Pending {
		return poolReconcileResult{Pending: true}, c.savePlannerStatus(res.Metadata.Name, state.pendingStatus())
	}
	plan, err := ReconcilePool(state.Runtime)
	if err != nil {
		return poolReconcileResult{}, err
	}
	return poolReconcileResult{Source: state.Runtime.Pool.Source}, c.applyPoolPlan(ctx, state, plan)
}
