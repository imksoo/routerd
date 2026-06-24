// SPDX-License-Identifier: BSD-3-Clause

package dynamicconfig

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"gopkg.in/yaml.v3"
)

type EffectiveResult struct {
	Suppressed     []SuppressedResource `json:"suppressed" yaml:"suppressed"`
	AddedResources []AddedResource      `json:"addedResources" yaml:"addedResources"`
	ActiveParts    []string             `json:"activeParts" yaml:"activeParts"`
	ExpiredParts   []string             `json:"expiredParts" yaml:"expiredParts"`
	Warnings       []string             `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type SuppressedResource struct {
	Target      DirectiveTarget `json:"target" yaml:"target"`
	MaskedBy    string          `json:"maskedBy" yaml:"maskedBy"`
	MaskedUntil time.Time       `json:"maskedUntil" yaml:"maskedUntil"`
	Reason      string          `json:"reason,omitempty" yaml:"reason,omitempty"`
}

type AddedResource struct {
	Source     string `json:"source" yaml:"source"`
	Generation int64  `json:"generation" yaml:"generation"`
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Name       string `json:"name" yaml:"name"`
}

type resourceIdentity struct {
	apiVersion string
	kind       string
	name       string
}

type maskRequest struct {
	source     string
	generation int64
	expiresAt  time.Time
	target     DirectiveTarget
	reason     string
}

type suppressionAccumulator struct {
	target  DirectiveTarget
	until   time.Time
	sources map[string]bool
	reasons map[string]bool
}

// BuildEffectiveConfig merges startup config with active dynamic config parts.
// It is pure: it never mutates startup, never writes startup config, and records
// dynamic ownership in EffectiveResult.AddedResources instead of extending
// api.ObjectMeta annotations for the MVP.
func BuildEffectiveConfig(startup api.Router, parts []DynamicConfigPart, policies []DynamicOverridePolicy, now time.Time) (api.Router, EffectiveResult, error) {
	effective, err := cloneRouter(startup)
	if err != nil {
		return api.Router{}, EffectiveResult{}, err
	}
	result := EffectiveResult{}

	startupIDs := map[resourceIdentity]api.Resource{}
	for _, res := range startup.Spec.Resources {
		startupIDs[identityFromResource(res)] = res
	}

	activeParts := make([]DynamicConfigPart, 0, len(parts))
	for _, part := range parts {
		if part.IsExpired(now) {
			result.ExpiredParts = append(result.ExpiredParts, part.Spec.Source)
			continue
		}
		result.ActiveParts = append(result.ActiveParts, part.Spec.Source)
		activeParts = append(activeParts, part)
	}

	masks := []maskRequest{}
	dynamicIDsInParts := map[resourceIdentity]bool{}
	for _, part := range activeParts {
		for _, res := range part.Spec.Resources {
			dynamicIDsInParts[identityFromResource(res)] = true
		}
		for _, directive := range part.Spec.Directives {
			if directive.Op != DirectiveOpMask {
				return api.Router{}, result, fmt.Errorf("unsupported dynamic directive op %q from source %q", directive.Op, part.Spec.Source)
			}
			mask := maskRequest{
				source:     part.Spec.Source,
				generation: part.Spec.Generation,
				expiresAt:  part.Spec.ExpiresAt,
				target:     directive.Target,
				reason:     directive.Reason,
			}
			targetID := identityFromTarget(mask.target)
			if !policyAllowsMask(policies, mask.source, mask.target) {
				return api.Router{}, result, fmt.Errorf("dynamic mask not allowed by DynamicOverridePolicy: source=%q target=%s", mask.source, targetString(mask.target))
			}
			if _, ok := startupIDs[targetID]; !ok {
				return api.Router{}, result, fmt.Errorf("mask target not found: source=%q target=%s", mask.source, targetString(mask.target))
			}
			if dynamicIDsInParts[targetID] {
				return api.Router{}, result, fmt.Errorf("mask target names a dynamic resource: source=%q target=%s", mask.source, targetString(mask.target))
			}
			masks = append(masks, mask)
		}
	}

	masked := map[resourceIdentity]*suppressionAccumulator{}
	for _, mask := range masks {
		id := identityFromTarget(mask.target)
		acc, ok := masked[id]
		if !ok {
			acc = &suppressionAccumulator{
				target:  mask.target,
				sources: map[string]bool{},
				reasons: map[string]bool{},
			}
			masked[id] = acc
		}
		if mask.expiresAt.After(acc.until) {
			acc.until = mask.expiresAt
		}
		acc.sources["DynamicConfigPart/"+mask.source] = true
		if strings.TrimSpace(mask.reason) != "" {
			acc.reasons[strings.TrimSpace(mask.reason)] = true
		}
	}
	if len(masked) > 0 {
		resources := effective.Spec.Resources[:0]
		for _, res := range effective.Spec.Resources {
			if _, ok := masked[identityFromResource(res)]; ok {
				continue
			}
			resources = append(resources, res)
		}
		effective.Spec.Resources = resources
		for _, acc := range sortedSuppressions(masked) {
			result.Suppressed = append(result.Suppressed, SuppressedResource{
				Target:      acc.target,
				MaskedBy:    joinSortedMapKeys(acc.sources, ","),
				MaskedUntil: acc.until,
				Reason:      joinSortedMapKeys(acc.reasons, "; "),
			})
		}
	}

	addedDynamicIDs := map[resourceIdentity]string{}
	for _, part := range activeParts {
		for _, res := range part.Spec.Resources {
			id := identityFromResource(res)
			if _, ok := startupIDs[id]; ok {
				return api.Router{}, result, fmt.Errorf("dynamic resource conflicts with startup resource: source=%q resource=%s", part.Spec.Source, res.ID())
			}
			if existing := addedDynamicIDs[id]; existing != "" {
				return api.Router{}, result, fmt.Errorf("dynamic resource conflicts with another dynamic resource: source=%q resource=%s existingSource=%q", part.Spec.Source, res.ID(), existing)
			}
			resCopy, err := cloneResource(res)
			if err != nil {
				return api.Router{}, result, err
			}
			effective.Spec.Resources = append(effective.Spec.Resources, resCopy)
			addedDynamicIDs[id] = part.Spec.Source
			result.AddedResources = append(result.AddedResources, AddedResource{
				Source:     part.Spec.Source,
				Generation: part.Spec.Generation,
				APIVersion: res.APIVersion,
				Kind:       res.Kind,
				Name:       res.Metadata.Name,
			})
		}
	}

	if err := config.Validate(&effective); err != nil {
		return api.Router{}, result, err
	}
	return effective, result, nil
}

func ExtractDynamicOverridePolicies(router api.Router) ([]DynamicOverridePolicy, error) {
	var policies []DynamicOverridePolicy
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.ConfigAPIVersion || res.Kind != "DynamicOverridePolicy" {
			continue
		}
		spec, err := res.DynamicOverridePolicySpec()
		if err != nil {
			return nil, err
		}
		policies = append(policies, DynamicOverridePolicy{
			TypeMeta: api.TypeMeta{APIVersion: ConfigAPIVersion, Kind: "DynamicOverridePolicy"},
			Metadata: res.Metadata,
			Spec: DynamicOverridePolicySpec{
				Allow: convertPolicyRules(spec.Allow),
			},
		})
	}
	return policies, nil
}

func convertPolicyRules(rules []api.DynamicOverrideAllowRule) []OverrideAllowRule {
	out := make([]OverrideAllowRule, 0, len(rules))
	for _, rule := range rules {
		targets := make([]DirectiveTarget, 0, len(rule.Targets))
		for _, target := range rule.Targets {
			targets = append(targets, DirectiveTarget{
				APIVersion: target.APIVersion,
				Kind:       target.Kind,
				Name:       target.Name,
			})
		}
		out = append(out, OverrideAllowRule{
			Source:     rule.Source,
			Operations: append([]string(nil), rule.Operations...),
			Targets:    targets,
		})
	}
	return out
}

func policyAllowsMask(policies []DynamicOverridePolicy, source string, target DirectiveTarget) bool {
	for _, policy := range policies {
		for _, rule := range policy.Spec.Allow {
			if rule.Source != source || !containsOperation(rule.Operations, DirectiveOpMask) {
				continue
			}
			for _, allowedTarget := range rule.Targets {
				if targetsEqual(allowedTarget, target) {
					return true
				}
			}
		}
	}
	return false
}

func containsOperation(operations []string, want string) bool {
	for _, op := range operations {
		if op == want {
			return true
		}
	}
	return false
}

func targetsEqual(a, b DirectiveTarget) bool {
	return a.APIVersion == b.APIVersion && a.Kind == b.Kind && a.Name == b.Name
}

func identityFromResource(res api.Resource) resourceIdentity {
	return resourceIdentity{apiVersion: res.APIVersion, kind: res.Kind, name: res.Metadata.Name}
}

func identityFromTarget(target DirectiveTarget) resourceIdentity {
	return resourceIdentity{apiVersion: target.APIVersion, kind: target.Kind, name: target.Name}
}

func targetString(target DirectiveTarget) string {
	return target.APIVersion + "/" + target.Kind + "/" + target.Name
}

func sortedSuppressions(masked map[resourceIdentity]*suppressionAccumulator) []*suppressionAccumulator {
	keys := make([]resourceIdentity, 0, len(masked))
	for key := range masked {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].apiVersion != keys[j].apiVersion {
			return keys[i].apiVersion < keys[j].apiVersion
		}
		if keys[i].kind != keys[j].kind {
			return keys[i].kind < keys[j].kind
		}
		return keys[i].name < keys[j].name
	})
	out := make([]*suppressionAccumulator, 0, len(keys))
	for _, key := range keys {
		out = append(out, masked[key])
	}
	return out
}

func joinSortedMapKeys(values map[string]bool, sep string) string {
	if len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return strings.Join(keys, sep)
}

func cloneRouter(router api.Router) (api.Router, error) {
	data, err := yaml.Marshal(router)
	if err != nil {
		return api.Router{}, fmt.Errorf("clone startup config: %w", err)
	}
	var out api.Router
	if err := yaml.Unmarshal(data, &out); err != nil {
		return api.Router{}, fmt.Errorf("clone startup config: %w", err)
	}
	return out, nil
}

func cloneResource(resource api.Resource) (api.Resource, error) {
	data, err := yaml.Marshal(resource)
	if err != nil {
		return api.Resource{}, fmt.Errorf("clone dynamic resource %s: %w", resource.ID(), err)
	}
	var out api.Resource
	if err := yaml.Unmarshal(data, &out); err != nil {
		return api.Resource{}, fmt.Errorf("clone dynamic resource %s: %w", resource.ID(), err)
	}
	return out, nil
}
