// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func TestSourcePolicyBoundsExistingObservation(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"}, Metadata: api.ObjectMeta{Name: "site"}, Spec: api.DynamicConfigSourceSpec{PluginRef: "inventory", TTL: "10s"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"}, Metadata: api.ObjectMeta{Name: "local"}, Spec: api.DynamicConfigSourceSpec{PluginRef: "inventory", TTL: "3s"}},
	}}}
	limit, err := SourceTTL(router, "inventory")
	if err != nil || limit != 3*time.Second {
		t.Fatalf("source limit=%s err=%v", limit, err)
	}
	observed := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name                string
		lifetime, age, want time.Duration
		reject              bool
	}{
		{"cap long output", time.Hour, time.Second, 3 * time.Second, false},
		{"retain shorter output", 2 * time.Second, time.Second, 2 * time.Second, false},
		{"do not refresh stale output", time.Hour, 3 * time.Second, time.Hour, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			part := dynamicconfig.DynamicConfigPart{Spec: dynamicconfig.DynamicConfigPartSpec{ObservedAt: observed, ExpiresAt: observed.Add(tc.lifetime)}}
			err := LimitDynamicConfigLifetime(&part, limit, observed.Add(tc.age))
			if (err != nil) != tc.reject || !part.Spec.ObservedAt.Equal(observed) || !part.Spec.ExpiresAt.Equal(observed.Add(tc.want)) {
				t.Fatalf("bounded part=%+v err=%v", part, err)
			}
		})
	}
}
