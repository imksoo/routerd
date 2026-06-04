// SPDX-License-Identifier: BSD-3-Clause

package federationguard

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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
// RemoteAddressClaim. Non-observed events and unparsable/non-IP subjects are
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
	for _, part := range parts {
		if part.EffectiveStatus(now) != "active" || strings.TrimSpace(part.ResourcesJSON) == "" {
			continue
		}
		var resources []api.Resource
		if err := json.Unmarshal([]byte(part.ResourcesJSON), &resources); err != nil {
			return fmt.Errorf("decode dynamic resources for federation self-capture guard source %q: %w", part.Source, err)
		}
		for _, res := range resources {
			if res.Kind != "RemoteAddressClaim" {
				continue
			}
			spec, err := res.RemoteAddressClaimSpec()
			if err != nil {
				return fmt.Errorf("decode RemoteAddressClaim/%s for federation self-capture guard: %w", res.Metadata.Name, err)
			}
			if strings.TrimSpace(spec.Capture.Type) == "" {
				continue
			}
			claimAddr, ok := parseAddress(spec.Address)
			if !ok || claimAddr != addr {
				continue
			}
			source := res.Kind + "/" + res.Metadata.Name
			if annSource := strings.TrimSpace(res.Metadata.Annotations["routerd.net/dynamic-source"]); annSource != "" {
				source += " from " + annSource
			}
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
