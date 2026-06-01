// SPDX-License-Identifier: BSD-3-Clause

package provideraction

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
)

// PolicyAndPlugins extracts the first ProviderActionPolicy and every Plugin
// from a Router. An absent policy returns the zero-value locked-down policy.
func PolicyAndPlugins(router *api.Router) (api.ProviderActionPolicySpec, []api.Resource, error) {
	if router == nil {
		return api.ProviderActionPolicySpec{}, nil, nil
	}
	var policy api.ProviderActionPolicySpec
	var foundPolicy bool
	var plugins []api.Resource
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "ProviderActionPolicy":
			if foundPolicy {
				continue
			}
			spec, err := res.ProviderActionPolicySpec()
			if err != nil {
				return api.ProviderActionPolicySpec{}, nil, fmt.Errorf("ProviderActionPolicy %q: %w", res.Metadata.Name, err)
			}
			policy = spec
			foundPolicy = true
		case "Plugin":
			plugins = append(plugins, res)
		}
	}
	return policy, plugins, nil
}

// AutoExecutionEnabled reports whether daemon reconcile may execute pending
// provider actions without operator approval. Per ADR 0007, this is exactly the
// policy auto-approve posture: enabled, live mutation allowed, approval disabled,
// and a positive per-run cap.
func AutoExecutionEnabled(policy api.ProviderActionPolicySpec) (bool, string) {
	if !policy.Enabled {
		return false, "policy.enabled=false"
	}
	if dryRunOnly(policy) {
		return false, "policy.dryRunOnly=true"
	}
	if policy.RequireApproval == nil || *policy.RequireApproval {
		return false, "policy.requireApproval=true"
	}
	if policy.MaxActionsPerRun <= 0 {
		return false, "policy.maxActionsPerRun<=0"
	}
	return true, ""
}
