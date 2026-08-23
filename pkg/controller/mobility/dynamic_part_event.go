// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"context"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

// publishDynamicConfigPartChanged wakes consumers only after a producer has
// durably changed a DynamicConfigPart. The part remains the source of truth;
// consumers reload it from the store instead of trusting event payloads.
func publishDynamicConfigPartChanged(ctx context.Context, events *bus.Bus, producer string, owner api.Resource, source, digest string, now time.Time) {
	if events == nil || strings.TrimSpace(source) == "" || strings.TrimSpace(digest) == "" {
		return
	}
	event := daemonapi.NewEvent(
		daemonapi.DaemonRef{Name: strings.TrimSpace(producer), Kind: "controller"},
		dynamicconfig.PartChangedEvent,
		daemonapi.SeverityInfo,
	)
	event.Time = now.UTC()
	if strings.TrimSpace(owner.APIVersion) != "" && strings.TrimSpace(owner.Kind) != "" && strings.TrimSpace(owner.Metadata.Name) != "" {
		event.Resource = &daemonapi.ResourceRef{
			APIVersion: owner.APIVersion,
			Kind:       owner.Kind,
			Name:       owner.Metadata.Name,
		}
	}
	event.Attributes = map[string]string{
		"source": strings.TrimSpace(source),
		"digest": strings.TrimSpace(digest),
	}
	// A bus persistence error must not roll back a durable desired-config
	// update. Local delivery is still attempted and periodic reconciliation is
	// the bounded fallback.
	_ = events.Publish(ctx, event)
}
