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
	default:
		return false, nil
	}
	return true, nil
}
