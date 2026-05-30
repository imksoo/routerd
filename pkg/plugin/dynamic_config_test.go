// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func TestDynamicConfigPartFromResult(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	observedAt := now.Add(-time.Minute)
	result := validPluginResult(observedAt)

	part, err := DynamicConfigPartFromResult("Plugin/cloud", 7, result, now)
	if err != nil {
		t.Fatalf("DynamicConfigPartFromResult: %v", err)
	}
	if part.Spec.Source != "Plugin/cloud" || part.Spec.Generation != 7 {
		t.Fatalf("part source/generation = %s/%d", part.Spec.Source, part.Spec.Generation)
	}
	if !part.Spec.ObservedAt.Equal(observedAt) {
		t.Fatalf("observedAt = %s", part.Spec.ObservedAt)
	}
	if !part.Spec.ExpiresAt.Equal(observedAt.Add(5 * time.Minute)) {
		t.Fatalf("expiresAt = %s", part.Spec.ExpiresAt)
	}
	if part.Spec.Digest == "" {
		t.Fatalf("digest is empty")
	}
	if _, ok := part.Spec.Resources[0].Spec.(api.IPv4RouteSpec); !ok {
		t.Fatalf("resource spec type = %T, want api.IPv4RouteSpec", part.Spec.Resources[0].Spec)
	}
	if len(part.Spec.Directives) != 1 {
		t.Fatalf("directives = %#v", part.Spec.Directives)
	}

	part2, err := DynamicConfigPartFromResult("Plugin/cloud", 7, result, now)
	if err != nil {
		t.Fatalf("second DynamicConfigPartFromResult: %v", err)
	}
	if part.Spec.Digest != part2.Spec.Digest {
		t.Fatalf("digest not stable: %q != %q", part.Spec.Digest, part2.Spec.Digest)
	}
}

func TestDynamicConfigPartFromResultDefaultsObservedAt(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	result := validPluginResult(time.Time{})
	part, err := DynamicConfigPartFromResult("Plugin/cloud", 1, result, now)
	if err != nil {
		t.Fatalf("DynamicConfigPartFromResult: %v", err)
	}
	if !part.Spec.ObservedAt.Equal(now) {
		t.Fatalf("observedAt = %s, want %s", part.Spec.ObservedAt, now)
	}
}

func TestDynamicConfigPartFromResultRejectsInvalidOutput(t *testing.T) {
	now := time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		mutate func(*PluginResult)
		want   string
	}{
		{
			name: "missing ttl",
			mutate: func(result *PluginResult) {
				result.Status.TTL = ""
			},
			want: "status.ttl is required",
		},
		{
			name: "zero ttl",
			mutate: func(result *PluginResult) {
				result.Status.TTL = "0s"
			},
			want: "greater than 0",
		},
		{
			name: "unknown directive op",
			mutate: func(result *PluginResult) {
				result.Status.Directives[0].Op = "replace"
			},
			want: "op must be mask",
		},
		{
			name: "resource empty name",
			mutate: func(result *PluginResult) {
				result.Status.Resources[0].Metadata.Name = ""
			},
			want: "metadata.name is required",
		},
		{
			name: "untyped resource spec",
			mutate: func(result *PluginResult) {
				result.Status.Resources[0].Spec = map[string]any{"destination": "10.0.0.0/24"}
			},
			want: "untyped map",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validPluginResult(now)
			tt.mutate(&result)
			_, err := DynamicConfigPartFromResult("Plugin/cloud", 1, result, now)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func validPluginResult(observedAt time.Time) PluginResult {
	return PluginResult{
		TypeMeta: api.TypeMeta{APIVersion: PluginAPIVersion, Kind: "PluginResult"},
		Metadata: api.ObjectMeta{
			Name: "cloud",
		},
		Status: PluginResultStatus{
			ObservedAt: observedAt,
			TTL:        "5m",
			Resources: []api.Resource{{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{
					Name: "cloud-route",
				},
				Spec: api.IPv4RouteSpec{
					Destination: "10.0.0.0/24",
					Gateway:     "192.0.2.1",
				},
			}},
			Directives: []dynamicconfig.DynamicConfigDirective{{
				Op: dynamicconfig.DirectiveOpMask,
				Target: dynamicconfig.DirectiveTarget{
					APIVersion: api.NetAPIVersion,
					Kind:       "IPv4Route",
					Name:       "static-route",
				},
				Reason: "cloud route active",
			}},
		},
	}
}
