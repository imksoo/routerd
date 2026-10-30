// SPDX-License-Identifier: BSD-3-Clause
package config

import (
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
)

// ValidatePFFirewallRule is shared by target validation and direct rendering.
// Reject selectors without a PF owner rather than accepting an ignored field.
func ValidatePFFirewallRule(id string, spec api.FirewallRuleSpec) error {
	if spec.FromZone == "self" && spec.ToZone == "self" {
		return fmt.Errorf("%s FreeBSD PF skips loopback; self-to-self filtering is unavailable", id)
	}
	if len(spec.DestinationSetRefs) > 0 || len(spec.ExcludeDestinationSetRefs) > 0 {
		return fmt.Errorf("%s FreeBSD PF FirewallRule has no address-set table owner; use destinationCIDRs", id)
	}
	if spec.RateLimit.Rate > 0 || spec.RateLimit.PacketsPerSecond > 0 || spec.RateLimit.Burst > 0 {
		return fmt.Errorf("%s FreeBSD PF FirewallRule packet/byte rateLimit is not implemented; TCP connection-rate tracking is not equivalent", id)
	}
	if spec.ConnLimit.MaxPerSource > 0 {
		return fmt.Errorf("%s FreeBSD PF FirewallRule connLimit is not implemented", id)
	}
	return nil
}
