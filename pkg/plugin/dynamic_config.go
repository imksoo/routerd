// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func DynamicConfigPartFromResult(source string, generation int64, result PluginResult, now time.Time) (dynamicconfig.DynamicConfigPart, error) {
	if strings.TrimSpace(source) == "" {
		return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("source is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	if result.APIVersion != PluginAPIVersion {
		return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult apiVersion must be %s", PluginAPIVersion)
	}
	if result.Kind != "PluginResult" {
		return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult kind must be PluginResult")
	}
	observedAt := result.Status.ObservedAt
	if observedAt.IsZero() {
		observedAt = now
	} else {
		observedAt = observedAt.UTC()
	}
	ttlText := strings.TrimSpace(result.Status.TTL)
	if ttlText == "" {
		return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.ttl is required")
	}
	ttl, err := time.ParseDuration(ttlText)
	if err != nil {
		return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.ttl must be a valid duration: %w", err)
	}
	if ttl <= 0 {
		return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.ttl must be greater than 0")
	}
	for i, resource := range result.Status.Resources {
		if strings.TrimSpace(resource.APIVersion) == "" {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.resources[%d].apiVersion is required", i)
		}
		if strings.TrimSpace(resource.Kind) == "" {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.resources[%d].kind is required", i)
		}
		if strings.TrimSpace(resource.Metadata.Name) == "" {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.resources[%d].metadata.name is required", i)
		}
		if resource.Spec == nil {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.resources[%d].spec is required", i)
		}
		if isUntypedMap(resource.Spec) {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.resources[%d].spec decoded as untyped map; decode plugin output with yaml.Unmarshal", i)
		}
	}
	for i, directive := range result.Status.Directives {
		if directive.Op != dynamicconfig.DirectiveOpMask {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.directives[%d].op must be mask", i)
		}
		if strings.TrimSpace(directive.Target.APIVersion) == "" {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.directives[%d].target.apiVersion is required", i)
		}
		if strings.TrimSpace(directive.Target.Kind) == "" {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.directives[%d].target.kind is required", i)
		}
		if strings.TrimSpace(directive.Target.Name) == "" {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.directives[%d].target.name is required", i)
		}
	}

	for i, plan := range result.Status.ActionPlans {
		if err := ValidateActionPlan(plan); err != nil {
			return dynamicconfig.DynamicConfigPart{}, fmt.Errorf("PluginResult status.actionPlans[%d]: %w", i, err)
		}
	}

	digest, err := dynamicPayloadDigest(result.Status.Resources, result.Status.Directives)
	if err != nil {
		return dynamicconfig.DynamicConfigPart{}, err
	}
	return dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: dynamicPartName(source, generation),
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:      source,
			Generation:  generation,
			ObservedAt:  observedAt,
			ExpiresAt:   observedAt.Add(ttl),
			Digest:      digest,
			Resources:   result.Status.Resources,
			Directives:  result.Status.Directives,
			ActionPlans: result.Status.ActionPlans,
		},
	}, nil
}

func dynamicPayloadDigest(resources []api.Resource, directives []dynamicconfig.DynamicConfigDirective) (string, error) {
	payload := struct {
		Resources  []api.Resource                         `json:"resources"`
		Directives []dynamicconfig.DynamicConfigDirective `json:"directives"`
	}{
		Resources:  resources,
		Directives: directives,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode dynamic config digest payload: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func dynamicPartName(source string, generation int64) string {
	name := strings.Trim(strings.ReplaceAll(source, "/", "-"), "-")
	if name == "" {
		name = "dynamic"
	}
	return fmt.Sprintf("%s-%d", name, generation)
}

func isUntypedMap(value any) bool {
	switch value.(type) {
	case map[string]any, map[any]any:
		return true
	}
	typ := reflect.TypeOf(value)
	return typ != nil && typ.Kind() == reflect.Map
}
