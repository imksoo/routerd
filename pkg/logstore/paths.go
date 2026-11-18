// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func DNSQueryPaths(router *api.Router, fallback string) []string {
	if router != nil {
		var out []string
		for _, res := range router.Spec.Resources {
			if res.APIVersion != api.NetAPIVersion || res.Kind != "DNSResolver" {
				continue
			}
			spec, err := res.DNSResolverSpec()
			if err == nil && spec.QueryLog.Enabled && strings.TrimSpace(spec.QueryLog.Path) != "" {
				out = append(out, spec.QueryLog.Path)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	return []string{fallback}
}

func DNSQueryPath(router *api.Router, fallback string) string {
	return DNSQueryPaths(router, fallback)[0]
}

func TrafficFlowPath(router *api.Router, fallback string) string {
	if router != nil {
		for _, res := range router.Spec.Resources {
			if res.APIVersion != api.NetAPIVersion || res.Kind != "TrafficFlowLog" {
				continue
			}
			spec, err := res.TrafficFlowLogSpec()
			if err == nil && spec.Enabled && strings.TrimSpace(spec.Path) != "" {
				return spec.Path
			}
		}
	}
	return fallback
}

func FirewallPath(router *api.Router, fallback string) string {
	if router != nil {
		for _, res := range router.Spec.Resources {
			if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallEventLog" {
				continue
			}
			spec, err := res.FirewallEventLogSpec()
			if err == nil && spec.Enabled && strings.TrimSpace(spec.Path) != "" {
				return spec.Path
			}
		}
	}
	return fallback
}
