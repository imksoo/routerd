// SPDX-License-Identifier: BSD-3-Clause

package dynamicconfig

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestBuildEffectiveConfigExpiredExcludedActiveIncluded(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	active := testPart("cloudedge", 1, now.Add(time.Hour), []api.Resource{testInterface("lan", "br0")}, nil)
	expired := testPart("cloudedge", 2, now.Add(-time.Hour), []api.Resource{testInterface("stale", "dummy0")}, nil)

	effective, result, err := BuildEffectiveConfig(startup, []DynamicConfigPart{active, expired}, nil, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if !hasResource(effective, api.NetAPIVersion, "Interface", "wan") {
		t.Fatalf("startup resource missing from effective")
	}
	if !hasResource(effective, api.NetAPIVersion, "Interface", "lan") {
		t.Fatalf("active dynamic resource missing from effective")
	}
	if hasResource(effective, api.NetAPIVersion, "Interface", "stale") {
		t.Fatalf("expired dynamic resource included in effective")
	}
	if !reflect.DeepEqual(result.ActiveParts, []string{"cloudedge"}) {
		t.Fatalf("active parts = %#v", result.ActiveParts)
	}
	if !reflect.DeepEqual(result.ExpiredParts, []string{"cloudedge"}) {
		t.Fatalf("expired parts = %#v", result.ExpiredParts)
	}
}

func TestBuildEffectiveConfigForOSUsesRequestedTarget(t *testing.T) {
	now := testNow()
	part := testPart("sam", 1, now.Add(time.Hour), []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
		Metadata: api.ObjectMeta{Name: "sam-core-a"},
		Spec: api.TunnelInterfaceSpec{
			Mode: "ipip", Local: "192.0.2.10", Remote: "192.0.2.20", TrustedUnderlay: true,
		},
	}}, nil)
	if _, _, err := BuildEffectiveConfigForOS(testRouter(), []DynamicConfigPart{part}, nil, now, platform.OSLinux); err != nil {
		t.Fatalf("Linux dynamic tunnel validation: %v", err)
	}
	if _, _, err := BuildEffectiveConfigForOS(testRouter(), []DynamicConfigPart{part}, nil, now, platform.OSFreeBSD); err == nil || !strings.Contains(err.Error(), "FreeBSD cloned interface name") {
		t.Fatalf("FreeBSD dynamic tunnel validation = %v, want cloned-interface rejection", err)
	}
}

func TestBuildEffectiveConfigAllowedMaskSuppressesStartupResource(t *testing.T) {
	now := testNow()
	expiresAt := now.Add(time.Hour)
	startup := testRouter(testInterface("wan", "ens18"))
	part := testPart("cloudedge", 1, expiresAt, nil, []DynamicConfigDirective{
		testMask("Interface", "wan", "cloud edge owns WAN"),
	})

	effective, result, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, []DynamicOverridePolicy{
		testPolicy("cloudedge", testTarget("Interface", "wan")),
	}, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if hasResource(effective, api.NetAPIVersion, "Interface", "wan") {
		t.Fatalf("masked resource still present in effective")
	}
	if len(result.Suppressed) != 1 {
		t.Fatalf("suppressed = %#v", result.Suppressed)
	}
	got := result.Suppressed[0]
	if got.MaskedBy != "DynamicConfigPart/cloudedge" {
		t.Fatalf("MaskedBy = %q", got.MaskedBy)
	}
	if !got.MaskedUntil.Equal(expiresAt) {
		t.Fatalf("MaskedUntil = %s, want %s", got.MaskedUntil, expiresAt)
	}
	if got.Reason != "cloud edge owns WAN" {
		t.Fatalf("Reason = %q", got.Reason)
	}
}

func TestBuildEffectiveConfigDisallowedMasks(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	tests := []struct {
		name     string
		part     DynamicConfigPart
		policies []DynamicOverridePolicy
		wantErr  string
	}{
		{
			name:    "no policy",
			part:    testPart("cloudedge", 1, now.Add(time.Hour), nil, []DynamicConfigDirective{testMask("Interface", "wan", "")}),
			wantErr: "dynamic mask not allowed by DynamicOverridePolicy",
		},
		{
			name:     "wrong source",
			part:     testPart("cloudedge", 1, now.Add(time.Hour), nil, []DynamicConfigDirective{testMask("Interface", "wan", "")}),
			policies: []DynamicOverridePolicy{testPolicy("other", testTarget("Interface", "wan"))},
			wantErr:  "dynamic mask not allowed by DynamicOverridePolicy",
		},
		{
			name:     "wrong target",
			part:     testPart("cloudedge", 1, now.Add(time.Hour), nil, []DynamicConfigDirective{testMask("Interface", "wan", "")}),
			policies: []DynamicOverridePolicy{testPolicy("cloudedge", testTarget("Interface", "lan"))},
			wantErr:  "dynamic mask not allowed by DynamicOverridePolicy",
		},
		{
			name: "non mask op",
			part: testPart("cloudedge", 1, now.Add(time.Hour), nil, []DynamicConfigDirective{{
				Op:     "replace",
				Target: testTarget("Interface", "wan"),
			}}),
			wantErr: "unsupported dynamic directive op",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := BuildEffectiveConfig(startup, []DynamicConfigPart{tt.part}, tt.policies, now)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestBuildEffectiveConfigMaskTargetMissing(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	part := testPart("cloudedge", 1, now.Add(time.Hour), nil, []DynamicConfigDirective{testMask("Interface", "lan", "")})

	_, _, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, []DynamicOverridePolicy{
		testPolicy("cloudedge", testTarget("Interface", "lan")),
	}, now)
	if err == nil || !strings.Contains(err.Error(), "mask target not found") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildEffectiveConfigMultipleMasksSameTarget(t *testing.T) {
	now := testNow()
	early := now.Add(time.Hour)
	late := now.Add(2 * time.Hour)
	startup := testRouter(testInterface("wan", "ens18"))
	parts := []DynamicConfigPart{
		testPart("cloudedge-a", 1, early, nil, []DynamicConfigDirective{testMask("Interface", "wan", "a")}),
		testPart("cloudedge-b", 1, late, nil, []DynamicConfigDirective{testMask("Interface", "wan", "b")}),
	}
	policies := []DynamicOverridePolicy{
		testPolicy("cloudedge-a", testTarget("Interface", "wan")),
		testPolicy("cloudedge-b", testTarget("Interface", "wan")),
	}

	_, result, err := BuildEffectiveConfig(startup, parts, policies, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if len(result.Suppressed) != 1 {
		t.Fatalf("suppressed = %#v", result.Suppressed)
	}
	if !result.Suppressed[0].MaskedUntil.Equal(late) {
		t.Fatalf("MaskedUntil = %s, want %s", result.Suppressed[0].MaskedUntil, late)
	}
	if result.Suppressed[0].MaskedBy != "DynamicConfigPart/cloudedge-a,DynamicConfigPart/cloudedge-b" {
		t.Fatalf("MaskedBy = %q", result.Suppressed[0].MaskedBy)
	}
}

func TestBuildEffectiveConfigExpiredMasksRestoreTarget(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	part := testPart("cloudedge", 1, now.Add(-time.Hour), nil, []DynamicConfigDirective{testMask("Interface", "wan", "")})

	effective, result, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, nil, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if !hasResource(effective, api.NetAPIVersion, "Interface", "wan") {
		t.Fatalf("expired mask suppressed target")
	}
	if len(result.Suppressed) != 0 {
		t.Fatalf("suppressed = %#v", result.Suppressed)
	}
}

func TestBuildEffectiveConfigDynamicResourceConflictsWithStartup(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	part := testPart("cloudedge", 1, now.Add(time.Hour), []api.Resource{testInterface("wan", "ens19")}, nil)

	_, _, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, nil, now)
	if err == nil || !strings.Contains(err.Error(), "dynamic resource conflicts with startup resource") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildEffectiveConfigDynamicResourceConflictsWithAnotherDynamicResource(t *testing.T) {
	now := testNow()
	startup := testRouter()
	parts := []DynamicConfigPart{
		testPart("cloudedge-a", 1, now.Add(time.Hour), []api.Resource{testInterface("wan", "ens18")}, nil),
		testPart("cloudedge-b", 1, now.Add(time.Hour), []api.Resource{testInterface("wan", "ens19")}, nil),
	}

	_, _, err := BuildEffectiveConfig(startup, parts, nil, now)
	if err == nil || !strings.Contains(err.Error(), "dynamic resource conflicts with another dynamic resource") {
		t.Fatalf("error = %v", err)
	}
}

func TestBuildEffectiveConfigMaskStartupAddDifferentIdentityAllowed(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	part := testPart("cloudedge", 1, now.Add(time.Hour), []api.Resource{testInterface("wan-dynamic", "ens19")}, []DynamicConfigDirective{
		testMask("Interface", "wan", ""),
	})

	effective, _, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, []DynamicOverridePolicy{
		testPolicy("cloudedge", testTarget("Interface", "wan")),
	}, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if hasResource(effective, api.NetAPIVersion, "Interface", "wan") {
		t.Fatalf("masked startup resource present")
	}
	if !hasResource(effective, api.NetAPIVersion, "Interface", "wan-dynamic") {
		t.Fatalf("dynamic replacement resource missing")
	}
}

func TestBuildEffectiveConfigDoesNotMutateStartup(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	original := startup
	original.Spec.Resources = append([]api.Resource(nil), startup.Spec.Resources...)
	part := testPart("cloudedge", 1, now.Add(time.Hour), nil, []DynamicConfigDirective{testMask("Interface", "wan", "")})

	effective, _, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, []DynamicOverridePolicy{
		testPolicy("cloudedge", testTarget("Interface", "wan")),
	}, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if hasResource(effective, api.NetAPIVersion, "Interface", "wan") {
		t.Fatalf("test setup failed: effective still has masked resource")
	}
	if !reflect.DeepEqual(startup, original) {
		t.Fatalf("startup mutated:\n got %#v\nwant %#v", startup, original)
	}
}

func TestBuildEffectiveConfigPreservesTypedSpecs(t *testing.T) {
	now := testNow()
	startup := testRouter(testInterface("wan", "ens18"))
	original := startup
	original.Spec.Resources = append([]api.Resource(nil), startup.Spec.Resources...)
	part := testPart("cloudedge", 1, now.Add(time.Hour), []api.Resource{testInterface("lan", "br0")}, nil)

	effective, _, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, nil, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}

	startupResource, ok := findResource(effective, api.NetAPIVersion, "Interface", "wan")
	if !ok {
		t.Fatalf("startup resource missing from effective")
	}
	if _, ok := startupResource.Spec.(api.InterfaceSpec); !ok {
		t.Fatalf("startup resource spec type = %T, want api.InterfaceSpec", startupResource.Spec)
	}

	dynamicResource, ok := findResource(effective, api.NetAPIVersion, "Interface", "lan")
	if !ok {
		t.Fatalf("dynamic resource missing from effective")
	}
	if _, ok := dynamicResource.Spec.(api.InterfaceSpec); !ok {
		t.Fatalf("dynamic resource spec type = %T, want api.InterfaceSpec", dynamicResource.Spec)
	}

	if !reflect.DeepEqual(startup, original) {
		t.Fatalf("startup mutated:\n got %#v\nwant %#v", startup, original)
	}
}

func TestBuildEffectiveConfigRecordsDynamicOwnerInAddedResources(t *testing.T) {
	now := testNow()
	startup := testRouter()
	part := testPart("cloudedge", 42, now.Add(time.Hour), []api.Resource{testInterface("wan", "ens18")}, nil)

	_, result, err := BuildEffectiveConfig(startup, []DynamicConfigPart{part}, nil, now)
	if err != nil {
		t.Fatalf("BuildEffectiveConfig: %v", err)
	}
	if len(result.AddedResources) != 1 {
		t.Fatalf("added = %#v", result.AddedResources)
	}
	got := result.AddedResources[0]
	if got.Source != "cloudedge" || got.Generation != 42 || got.APIVersion != api.NetAPIVersion || got.Kind != "Interface" || got.Name != "wan" {
		t.Fatalf("added = %#v", got)
	}
}

func testNow() time.Time {
	return time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)
}

func testRouter(resources ...api.Resource) api.Router {
	return api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec: api.RouterSpec{
			Resources: resources,
		},
	}
}

func testInterface(name, ifname string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.InterfaceSpec{
			IfName:  ifname,
			Managed: true,
			Owner:   "routerd",
		},
	}
}

func testPart(source string, generation int64, expiresAt time.Time, resources []api.Resource, directives []DynamicConfigDirective) DynamicConfigPart {
	return DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: source},
		Spec: DynamicConfigPartSpec{
			Source:     source,
			Generation: generation,
			ObservedAt: testNow().Add(-time.Minute),
			ExpiresAt:  expiresAt,
			Digest:     "sha256:test",
			Resources:  resources,
			Directives: directives,
		},
	}
}

func testMask(kind, name, reason string) DynamicConfigDirective {
	return DynamicConfigDirective{
		Op:     DirectiveOpMask,
		Target: testTarget(kind, name),
		Reason: reason,
	}
}

func testTarget(kind, name string) DirectiveTarget {
	return DirectiveTarget{APIVersion: api.NetAPIVersion, Kind: kind, Name: name}
}

func testPolicy(source string, targets ...DirectiveTarget) DynamicOverridePolicy {
	return DynamicOverridePolicy{
		TypeMeta: api.TypeMeta{APIVersion: ConfigAPIVersion, Kind: "DynamicOverridePolicy"},
		Metadata: api.ObjectMeta{Name: source},
		Spec: DynamicOverridePolicySpec{
			Allow: []OverrideAllowRule{{
				Source:     source,
				Operations: []string{DirectiveOpMask},
				Targets:    targets,
			}},
		},
	}
}

func hasResource(router api.Router, apiVersion, kind, name string) bool {
	_, ok := findResource(router, apiVersion, kind, name)
	return ok
}

func findResource(router api.Router, apiVersion, kind, name string) (api.Resource, bool) {
	for _, res := range router.Spec.Resources {
		if res.APIVersion == apiVersion && res.Kind == kind && res.Metadata.Name == name {
			return res, true
		}
	}
	return api.Resource{}, false
}
