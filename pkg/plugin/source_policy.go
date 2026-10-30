// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

// SourceTTL returns the tightest declared lifetime for this Plugin source.
// Callers prepare it from startup configuration before invoking the plugin.
func SourceTTL(router *api.Router, pluginName string) (time.Duration, error) {
	if router == nil {
		return 0, nil
	}
	var limit time.Duration
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.PluginAPIVersion || resource.Kind != "DynamicConfigSource" {
			continue
		}
		spec, err := resource.DynamicConfigSourceSpec()
		if err != nil {
			return 0, err
		}
		if strings.TrimSpace(spec.PluginRef) != pluginName {
			continue
		}
		ttl, err := time.ParseDuration(strings.TrimSpace(spec.TTL))
		if err != nil || ttl <= 0 {
			return 0, fmt.Errorf("%s requires a positive source ttl", resource.ID())
		}
		if spec.MergePolicy != nil && spec.MergePolicy.Conflict != "" && spec.MergePolicy.Conflict != "reject" {
			return 0, fmt.Errorf("%s source merge policy must reject conflicts", resource.ID())
		}
		if limit == 0 || ttl < limit {
			limit = ttl
		}
	}
	return limit, nil
}

// LimitDynamicConfigLifetime caps an owned result without making an old
// observation fresh. A missing source policy leaves its valid result unchanged.
func LimitDynamicConfigLifetime(part *dynamicconfig.DynamicConfigPart, maxTTL time.Duration, now time.Time) error {
	if maxTTL < 0 {
		return fmt.Errorf("source lifetime must not be negative")
	}
	expiresAt := part.Spec.ExpiresAt
	if maxTTL > 0 {
		if bound := part.Spec.ObservedAt.Add(maxTTL); bound.Before(expiresAt) {
			expiresAt = bound
		}
	}
	if !now.Before(expiresAt) {
		return fmt.Errorf("PluginResult observation expired under source lifetime policy")
	}
	part.Spec.ExpiresAt = expiresAt
	return nil
}
