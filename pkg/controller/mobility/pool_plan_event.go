// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

// PoolPlanChangedEvent is emitted only after a MobilityPool DynamicConfigPart
// has been durably replaced with different desired content. Consumers use it
// solely as a wake-up signal and read the typed DynamicConfigPart; they never
// reconstruct desired state from this event or from object status.
const PoolPlanChangedEvent = "routerd.mobility.plan.changed"

func (c Controller) publishPoolPlanChanged(ctx context.Context, poolName, source, digest string, now time.Time) {
	if c.Bus == nil {
		return
	}
	event := daemonapi.NewEvent(
		daemonapi.DaemonRef{Name: "mobility", Kind: "mobility"},
		PoolPlanChangedEvent,
		daemonapi.SeverityInfo,
	)
	event.Time = now.UTC()
	event.Resource = &daemonapi.ResourceRef{
		APIVersion: api.MobilityAPIVersion,
		Kind:       "MobilityPool",
		Name:       strings.TrimSpace(poolName),
	}
	event.Attributes = map[string]string{
		"source": strings.TrimSpace(source),
		"digest": strings.TrimSpace(digest),
	}
	// Bus persistence errors do not roll back a completed desired-plan write:
	// local delivery is still attempted by Bus.Publish and the periodic SAM
	// reconciliation remains a bounded fallback.
	_ = c.Bus.Publish(ctx, event)
}
