// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestValidatePluginResources(t *testing.T) {
	router := testPluginRouter(
		testPluginResource(api.PluginSpec{
			Executable:   "/usr/local/libexec/routerd/plugins/cloud/bin/cloud",
			Timeout:      "10s",
			Capabilities: []string{"observe.cloud", "propose.dynamicConfig", "propose.providerAction"},
			Triggers:     []api.PluginTrigger{{Type: "interval", Every: "5m"}, {Type: "event", Topic: "routerd.cloud.refresh"}},
			Context: api.PluginContextSpec{Resources: []api.PluginContextResourceRef{
				{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile", Name: "azure-1"},
			}},
		}),
		testDynamicConfigSourceResource(api.DynamicConfigSourceSpec{
			PluginRef: "cloud",
			TTL:       "5m",
			MergePolicy: &api.MergePolicy{
				Conflict: "reject",
			},
			Triggers: []api.PluginTrigger{{Type: "interval", Every: "5m"}},
		}),
	)
	if err := Validate(router); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestValidatePluginRejectsInvalidResources(t *testing.T) {
	tests := []struct {
		name string
		res  api.Resource
		want string
	}{
		{
			name: "plugin wrong api version",
			res:  withAPIVersion(testPluginResource(api.PluginSpec{Executable: "/x"}), api.ConfigAPIVersion),
			want: "must use apiVersion plugin.routerd.net/v1alpha1",
		},
		{
			name: "plugin missing executable",
			res:  testPluginResource(api.PluginSpec{}),
			want: "spec.executable is required",
		},
		{
			name: "plugin relative executable",
			res:  testPluginResource(api.PluginSpec{Executable: "bin/cloud"}),
			want: "spec.executable must be an absolute path",
		},
		{
			name: "plugin bad timeout",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Timeout: "soon"}),
			want: "spec.timeout must be a valid duration",
		},
		{
			name: "plugin unknown capability",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Capabilities: []string{"mutate.cloud"}}),
			want: "spec.capabilities[0] must be observe.cloud, propose.dynamicConfig, or propose.providerAction",
		},
		{
			name: "plugin context missing apiVersion",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Context: api.PluginContextSpec{Resources: []api.PluginContextResourceRef{{Kind: "CloudProviderProfile", Name: "azure-1"}}}}),
			want: "spec.context.resources[0].apiVersion is required",
		},
		{
			name: "plugin context missing kind",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Context: api.PluginContextSpec{Resources: []api.PluginContextResourceRef{{APIVersion: api.HybridAPIVersion, Name: "azure-1"}}}}),
			want: "spec.context.resources[0].kind is required",
		},
		{
			name: "plugin context missing name",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Context: api.PluginContextSpec{Resources: []api.PluginContextResourceRef{{APIVersion: api.HybridAPIVersion, Kind: "CloudProviderProfile"}}}}),
			want: "spec.context.resources[0].name is required",
		},
		{
			name: "plugin unknown trigger",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Triggers: []api.PluginTrigger{{Type: "cron"}}}),
			want: "spec.triggers[0].type must be interval or event",
		},
		{
			name: "plugin interval missing every",
			res:  testPluginResource(api.PluginSpec{Executable: "/x", Triggers: []api.PluginTrigger{{Type: "interval"}}}),
			want: "spec.triggers[0].every is required",
		},
		{
			name: "dynamic source missing plugin ref",
			res:  testDynamicConfigSourceResource(api.DynamicConfigSourceSpec{TTL: "5m"}),
			want: "spec.pluginRef is required",
		},
		{
			name: "dynamic source missing ttl",
			res:  testDynamicConfigSourceResource(api.DynamicConfigSourceSpec{PluginRef: "cloud"}),
			want: "spec.ttl is required",
		},
		{
			name: "dynamic source bad conflict",
			res:  testDynamicConfigSourceResource(api.DynamicConfigSourceSpec{PluginRef: "cloud", TTL: "5m", MergePolicy: &api.MergePolicy{Conflict: "replace"}}),
			want: "spec.mergePolicy.conflict must be reject",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Validate(testPluginRouter(tt.res))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func testPluginRouter(resources ...api.Resource) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{
			Name: "test",
		},
		Spec: api.RouterSpec{Resources: resources},
	}
}

func testPluginResource(spec api.PluginSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "Plugin"},
		Metadata: api.ObjectMeta{
			Name: "cloud",
		},
		Spec: spec,
	}
}

func testDynamicConfigSourceResource(spec api.DynamicConfigSourceSpec) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.PluginAPIVersion, Kind: "DynamicConfigSource"},
		Metadata: api.ObjectMeta{
			Name: "cloud",
		},
		Spec: spec,
	}
}

func withAPIVersion(res api.Resource, apiVersion string) api.Resource {
	res.APIVersion = apiVersion
	return res
}
