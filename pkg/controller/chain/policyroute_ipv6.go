// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
)

var errNoPreferredIPv6Source = errors.New("no preferred global IPv6 source")

func (c IPv4PolicyRouteController) desiredPolicyIPv6Effects(ctx context.Context) ([]policyRouteEffect, error) {
	policies, _, err := c.desiredIPv6HostPolicies(ctx)
	if err != nil {
		return nil, err
	}
	return c.policyIPv6Effects(ctx, policies)
}

func (c IPv4PolicyRouteController) policyIPv6Effects(ctx context.Context, policies []ipv6HostPolicy) ([]policyRouteEffect, error) {
	var effects []policyRouteEffect
	for _, policy := range policies {
		table := policy.Table
		if policy.Lookup == "main" {
			table = 254
		}
		if policy.isSourceRule() {
			effects = append(effects, policyRouteEffect{Family: 6, Kind: "rule", Priority: policy.Priority, Table: table, Source: policy.Source})
			continue
		}
		if policy.Priority <= 1 {
			return nil, errors.New("IPv6 host default policy requires a priority above 1 for its main-route guard")
		}
		index := 1
		if !c.DryRun {
			var present bool
			var err error
			index, present, err = c.policyRouteIndex(ctx, policy.Interface)
			if err != nil {
				return nil, err
			}
			if !present {
				return nil, errors.New("IPv6 host policy source interface disappeared")
			}
		}
		effects = append(effects,
			policyRouteEffect{Family: 6, Kind: "route", Table: table, Gateway: policy.Gateway, Interface: policy.Interface, Source: policy.Source, Metric: policy.Metric, IfIndex: index},
			policyRouteEffect{Family: 6, Kind: "rule", Table: 254, Priority: policy.Priority - 1, InputIf: "lo", SuppressMainDefault: true},
			policyRouteEffect{Family: 6, Kind: "rule", Table: table, Priority: policy.Priority, InputIf: "lo"},
		)
	}
	return sortedPolicyRouteEffects(effects)
}
