// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// validateEventResource performs local field validation for CloudEdge Event
// Federation Kinds (ADR 0006). Phase 1 introduces only EventGroup. It returns
// handled=true for Kinds it owns so the caller's Kind switch accepts them.
func validateEventResource(res api.Resource, _ platform.OS) (bool, error) {
	switch res.Kind {
	case "EventGroup":
		if res.APIVersion != api.FederationAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FederationAPIVersion)
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.NodeName) == "" {
			return true, fmt.Errorf("%s spec.nodeName is required", res.ID())
		}
		for i, source := range spec.PeersFrom {
			if err := validateEventPeersFrom(res.ID(), i, source); err != nil {
				return true, err
			}
		}
		if spec.Retention.MaxEvents < 0 {
			return true, fmt.Errorf("%s spec.retention.maxEvents must be >= 0", res.ID())
		}
		if maxAge := strings.TrimSpace(spec.Retention.MaxAge); maxAge != "" {
			if _, err := time.ParseDuration(maxAge); err != nil {
				return true, fmt.Errorf("%s spec.retention.maxAge must be a Go duration: %w", res.ID(), err)
			}
		}
		// Auth is reserved for Phase 2 peer delivery; validate leniently.
		switch strings.TrimSpace(spec.Auth.Mode) {
		case "", "hmac":
		default:
			return true, fmt.Errorf("%s spec.auth.mode must be empty or hmac", res.ID())
		}
		if strings.TrimSpace(spec.Listen.Address) != "" {
			if spec.Listen.Port < 1 || spec.Listen.Port > 65535 {
				return true, fmt.Errorf("%s spec.listen.port must be 1..65535 when listen.address is set", res.ID())
			}
		}
		if window := strings.TrimSpace(spec.ReplayWindow); window != "" {
			if _, err := time.ParseDuration(window); err != nil {
				return true, fmt.Errorf("%s spec.replayWindow must be a Go duration: %w", res.ID(), err)
			}
		}
	case "EventPeer":
		if res.APIVersion != api.FederationAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FederationAPIVersion)
		}
		spec, err := res.EventPeerSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.GroupRef) == "" {
			return true, fmt.Errorf("%s spec.groupRef is required", res.ID())
		}
		if strings.TrimSpace(spec.NodeName) == "" {
			return true, fmt.Errorf("%s spec.nodeName is required", res.ID())
		}
		// Direction defaults to push when empty; only push is supported in Phase 2.
		direction := strings.TrimSpace(spec.Direction)
		switch direction {
		case "", "push":
		default:
			return true, fmt.Errorf("%s spec.direction must be empty or push", res.ID())
		}
		// Endpoint is required for push delivery (the only Phase 2 direction).
		if strings.TrimSpace(spec.Endpoint) == "" {
			return true, fmt.Errorf("%s spec.endpoint is required for push delivery", res.ID())
		}
		for i, t := range spec.Types {
			if strings.TrimSpace(t) == "" {
				return true, fmt.Errorf("%s spec.types[%d] must not be empty", res.ID(), i)
			}
		}
		for i, p := range spec.SubjectPrefixes {
			if strings.TrimSpace(p) == "" {
				return true, fmt.Errorf("%s spec.subjectPrefixes[%d] must not be empty", res.ID(), i)
			}
		}
	case "EventSubscription":
		if res.APIVersion != api.FederationAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FederationAPIVersion)
		}
		spec, err := res.EventSubscriptionSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.GroupRef) == "" {
			return true, fmt.Errorf("%s spec.groupRef is required", res.ID())
		}
		// Match.Types is required: a subscription must not blanket-trigger a
		// plugin on every event in the group.
		if len(spec.Match.Types) == 0 {
			return true, fmt.Errorf("%s spec.match.types is required (at least one type)", res.ID())
		}
		for i, t := range spec.Match.Types {
			if strings.TrimSpace(t) == "" {
				return true, fmt.Errorf("%s spec.match.types[%d] must not be empty", res.ID(), i)
			}
		}
		for i, p := range spec.Match.SubjectPrefixes {
			if strings.TrimSpace(p) == "" {
				return true, fmt.Errorf("%s spec.match.subjectPrefixes[%d] must not be empty", res.ID(), i)
			}
		}
		for i, n := range spec.Match.SourceNodes {
			if strings.TrimSpace(n) == "" {
				return true, fmt.Errorf("%s spec.match.sourceNodes[%d] must not be empty", res.ID(), i)
			}
		}
		for k := range spec.Match.Payload {
			if strings.TrimSpace(k) == "" {
				return true, fmt.Errorf("%s spec.match.payload has a blank key", res.ID())
			}
		}
		if strings.TrimSpace(spec.Trigger.PluginRef) == "" {
			return true, fmt.Errorf("%s spec.trigger.pluginRef is required", res.ID())
		}
		if window := strings.TrimSpace(spec.Trigger.BatchWindow); window != "" {
			if _, err := time.ParseDuration(window); err != nil {
				return true, fmt.Errorf("%s spec.trigger.batchWindow must be a Go duration: %w", res.ID(), err)
			}
		}
		if debounce := strings.TrimSpace(spec.Trigger.Debounce); debounce != "" {
			if _, err := time.ParseDuration(debounce); err != nil {
				return true, fmt.Errorf("%s spec.trigger.debounce must be a Go duration: %w", res.ID(), err)
			}
		}
	default:
		return false, nil
	}
	return true, nil
}

func validateEventPeersFrom(resourceID string, index int, source api.EventPeersSourceSpec) error {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind != "SAMNodeSet" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s spec.peersFrom[%d].resource must reference SAMNodeSet/<name>", resourceID, index)
	}
	return nil
}
