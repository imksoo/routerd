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
		// Identity-only EventGroup resources may omit auth. Runtime eventd
		// delivery/receive config is checked after all resources are indexed.
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
	case "FederationSLO":
		if res.APIVersion != api.FederationAPIVersion {
			return true, fmt.Errorf("%s must use apiVersion %s", res.ID(), api.FederationAPIVersion)
		}
		spec, err := res.FederationSLOSpec()
		if err != nil {
			return true, err
		}
		if strings.TrimSpace(spec.GroupRef) == "" {
			return true, fmt.Errorf("%s spec.groupRef is required", res.ID())
		}
		if spec.Delivery.LagWarnSeconds < 0 {
			return true, fmt.Errorf("%s spec.delivery.lagWarnSeconds must be >= 0", res.ID())
		}
		if spec.Delivery.LagFailSeconds < 0 {
			return true, fmt.Errorf("%s spec.delivery.lagFailSeconds must be >= 0", res.ID())
		}
		if spec.Delivery.ExpiresSoonSeconds < 0 {
			return true, fmt.Errorf("%s spec.delivery.expiresSoonSeconds must be >= 0", res.ID())
		}
		// Apply defaults for effective-value validation.
		effectiveWarn := spec.Delivery.LagWarnSeconds
		if effectiveWarn == 0 {
			effectiveWarn = 60 // defaultFederationWarnLag
		}
		effectiveFail := spec.Delivery.LagFailSeconds
		if effectiveFail == 0 {
			effectiveFail = 180 // defaultFederationFailLag
		}
		if effectiveWarn >= effectiveFail {
			return true, fmt.Errorf("%s spec.delivery.lagWarnSeconds (effective %d) must be less than lagFailSeconds (effective %d)", res.ID(), effectiveWarn, effectiveFail)
		}
		if spec.Subscription.MaxPendingRuns < 0 {
			return true, fmt.Errorf("%s spec.subscription.maxPendingRuns must be >= 0", res.ID())
		}
		if spec.Subscription.MaxFailedRuns < 0 {
			return true, fmt.Errorf("%s spec.subscription.maxFailedRuns must be >= 0", res.ID())
		}
	default:
		return false, nil
	}
	return true, nil
}

// validateFederationSLOCrossRefs checks cross-resource constraints for
// FederationSLO: each groupRef must be unique across all FederationSLO
// resources, and must reference an existing EventGroup resource.
func validateFederationSLOCrossRefs(router *api.Router) error {
	eventGroups := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.Kind == "EventGroup" {
			eventGroups[res.Metadata.Name] = true
		}
	}

	sloGroupRefs := map[string]string{} // groupRef → resource name
	for _, res := range router.Spec.Resources {
		if res.Kind != "FederationSLO" {
			continue
		}
		spec, err := res.FederationSLOSpec()
		if err != nil {
			continue
		}
		groupRef := strings.TrimSpace(spec.GroupRef)
		if existing, ok := sloGroupRefs[groupRef]; ok {
			return fmt.Errorf("%s spec.groupRef %q conflicts with %s: only one FederationSLO per EventGroup is allowed", res.ID(), groupRef, existing)
		}
		sloGroupRefs[groupRef] = res.ID()
		if !eventGroups[groupRef] {
			return fmt.Errorf("%s spec.groupRef %q does not reference an existing EventGroup resource", res.ID(), groupRef)
		}
	}
	return nil
}

func validateEventGroupRuntimeAuth(router *api.Router) error {
	type groupInfo struct {
		id   string
		spec api.EventGroupSpec
	}
	groups := map[string]groupInfo{}
	peerRefs := map[string]string{}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "EventGroup":
			spec, err := res.EventGroupSpec()
			if err != nil {
				continue
			}
			groups[res.Metadata.Name] = groupInfo{id: res.ID(), spec: spec}
		case "EventPeer":
			spec, err := res.EventPeerSpec()
			if err != nil {
				continue
			}
			groupRef := strings.TrimSpace(spec.GroupRef)
			if groupRef != "" {
				peerRefs[groupRef] = res.ID()
			}
		}
	}
	for name, group := range groups {
		if strings.TrimSpace(group.spec.Auth.SecretFile) != "" {
			continue
		}
		switch {
		case strings.TrimSpace(group.spec.Listen.Address) != "":
			return fmt.Errorf("%s spec.auth.secretFile is required when spec.listen.address is set", group.id)
		case len(group.spec.PeersFrom) > 0:
			return fmt.Errorf("%s spec.auth.secretFile is required when spec.peersFrom is set", group.id)
		case peerRefs[name] != "":
			return fmt.Errorf("%s spec.auth.secretFile is required because %s references this EventGroup", group.id, peerRefs[name])
		}
	}
	return nil
}

func validateEventPeersFrom(resourceID string, index int, source api.EventPeersSourceSpec) error {
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind != "SAMNodeSet" || strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s spec.peersFrom[%d].resource must reference SAMNodeSet/<name>", resourceID, index)
	}
	return nil
}
