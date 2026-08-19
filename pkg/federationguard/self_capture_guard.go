// SPDX-License-Identifier: BSD-3-Clause

package federationguard

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/federation"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// DynamicConfigPartStore is the narrow read surface needed to reject local
// observer feedback for addresses this node is already capturing.
type DynamicConfigPartStore interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
}

// SelfCapturedObservedEventError reports that an observed client event would
// feed back a locally captured SAM address into federation.
type SelfCapturedObservedEventError struct {
	Address string
	Source  string
}

func (e SelfCapturedObservedEventError) Error() string {
	if strings.TrimSpace(e.Source) != "" {
		return fmt.Sprintf("federation observed event for %s rejected: address is locally captured by %s", e.Address, e.Source)
	}
	return fmt.Sprintf("federation observed event for %s rejected: address is locally captured", e.Address)
}

// RejectSelfCapturedObservedEvent rejects routerd.client.ipv4.observed events
// whose subject/address is currently captured by an active local
// LocalCaptureIntent. Non-observed events and unparsable/non-IP subjects are
// left untouched so legitimate federation traffic is not blocked.
func RejectSelfCapturedObservedEvent(store DynamicConfigPartStore, ev federation.Event, now time.Time) error {
	if store == nil || strings.TrimSpace(ev.Type) != federation.ObservedIPv4EventType {
		return nil
	}
	addr, ok := eventAddress(ev)
	if !ok {
		return nil
	}
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		return fmt.Errorf("list dynamic config parts for federation self-capture guard: %w", err)
	}
	activeParts, invalidPools := codec.ActiveMobilityPoolPlanRecords(parts, now)
	if len(invalidPools) != 0 {
		return fmt.Errorf("invalid active MobilityPool typed plan record prevents federation self-capture evaluation")
	}
	for _, active := range activeParts {
		part, source := active.Record, active.Source
		if source.ARPObserver {
			continue
		}
		if strings.TrimSpace(part.MobilityDataplaneJSON) == "" {
			continue
		}
		plan, err := codec.DecodeMobilityDataplanePlan(part.MobilityDataplaneJSON)
		if err != nil {
			return fmt.Errorf("decode mobility dataplane plan for federation self-capture guard source %q: %w", part.Source, err)
		}
		if err := dynamicconfig.ValidateMobilityDataplanePlanScope(plan, source.PoolRef); err != nil {
			return fmt.Errorf("validate mobility dataplane scope for federation self-capture guard source %q: %w", part.Source, err)
		}
		for _, intent := range plan.Captures {
			if intent.Disposition != dynamicconfig.CaptureDesired && intent.Disposition != dynamicconfig.CaptureProtectExisting && intent.Disposition != dynamicconfig.CaptureHold {
				continue
			}
			if strings.TrimSpace(intent.CaptureType) == "" {
				continue
			}
			if strings.TrimSpace(intent.PoolRef) != source.PoolRef {
				continue
			}
			claimAddr, ok := parseAddress(intent.Address)
			if !ok || claimAddr != addr {
				continue
			}
			source := "MobilityPool/" + strings.TrimSpace(intent.PoolRef)
			return SelfCapturedObservedEventError{Address: addr.String(), Source: source}
		}
	}
	return nil
}

func eventAddress(ev federation.Event) (netip.Addr, bool) {
	if ev.Payload != nil {
		if addr, ok := parseAddress(ev.Payload["address"]); ok {
			return addr, true
		}
	}
	return parseAddress(ev.Subject)
}

func parseAddress(raw string) (netip.Addr, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return netip.Addr{}, false
	}
	if prefix, err := netip.ParsePrefix(raw); err == nil {
		return prefix.Addr(), true
	}
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return netip.Addr{}, false
	}
	return addr, true
}
