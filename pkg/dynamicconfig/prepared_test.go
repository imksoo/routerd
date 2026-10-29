// SPDX-License-Identifier: BSD-3-Clause

package dynamicconfig

import (
	"fmt"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestPreparedStartupOwnsStaticInputAndReevaluatesDynamicExpiry(t *testing.T) {
	startup := testRouter(testInterface("wan", "wan0"))
	startup.Spec.Resources = append(startup.Spec.Resources, api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.ConfigAPIVersion, Kind: "DynamicOverridePolicy"}, Metadata: api.ObjectMeta{Name: "producer"},
		Spec: api.DynamicOverridePolicySpec{Allow: []api.DynamicOverrideAllowRule{{Source: "producer", Operations: []string{DirectiveOpMask}, Targets: []api.DynamicOverrideTarget{{APIVersion: api.NetAPIVersion, Kind: "Interface", Name: "wan"}}}}},
	})
	if err := config.ValidateForOS(&startup, platform.OSLinux); err != nil {
		t.Fatal(err)
	}
	prepared, err := PrepareValidatedStartupForOS(startup, platform.OSLinux)
	if err != nil {
		t.Fatal(err)
	}
	// The caller keeps its own mutable candidate; accepted preparation owns a
	// distinct static input and does not have to clone it at every observation.
	startup.Spec.Resources[0].Spec = api.InterfaceSpec{IfName: "changed0"}
	policy, _ := startup.Spec.Resources[1].DynamicOverridePolicySpec()
	policy.Allow[0].Source = "changed-producer"
	startup.Spec.Resources[1].Spec = policy
	now := testNow()
	part := testPart("producer", 1, now.Add(time.Minute), []api.Resource{testInterface("lan", "lan0")}, []DynamicConfigDirective{testMask("Interface", "wan", "current owner")})
	effective, _, err := prepared.BuildEffective([]DynamicConfigPart{part}, now)
	if err != nil {
		t.Fatal(err)
	}
	if hasResource(effective, api.NetAPIVersion, "Interface", "wan") || !hasResource(effective, api.NetAPIVersion, "Interface", "lan") {
		t.Fatalf("live dynamic ownership was not applied: %+v", effective.Spec.Resources)
	}
	effective, _, err = prepared.BuildEffective([]DynamicConfigPart{part}, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !hasResource(effective, api.NetAPIVersion, "Interface", "wan") || hasResource(effective, api.NetAPIVersion, "Interface", "lan") {
		t.Fatalf("expired ownership was retained: %+v", effective.Spec.Resources)
	}
	spec, err := effective.Spec.Resources[0].InterfaceSpec()
	if err != nil || spec.IfName != "wan0" {
		t.Fatalf("candidate mutation changed prepared input: %+v %v", spec, err)
	}
	invalid := testPart("producer", 2, now.Add(time.Minute), []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "missing-interface"}, Spec: api.IPv4StaticAddressSpec{Interface: "missing", Address: "192.0.2.1/24"}}}, nil)
	if _, _, err := prepared.BuildEffective([]DynamicConfigPart{invalid}, now); err == nil {
		t.Fatal("prepared input bypassed dynamic effective validation")
	}
}

func BenchmarkEffectiveStartupPreparation(b *testing.B) {
	startup := testRouter()
	for i := range 100 {
		startup.Spec.Resources = append(startup.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "Sysctl"}, Metadata: api.ObjectMeta{Name: fmt.Sprintf("tuning-%03d", i)}, Spec: api.SysctlSpec{Key: fmt.Sprintf("kernel.test.%03d", i), Value: "1"}})
	}
	now := testNow()
	parts := []DynamicConfigPart{testPart("producer", 1, now.Add(time.Minute), []api.Resource{testInterface("lan", "lan0")}, nil)}
	prepared, err := PrepareValidatedStartupForOS(startup, platform.OSLinux)
	if err != nil {
		b.Fatal(err)
	}
	b.Run("accepted-on-each-observation", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := BuildEffectiveConfigForOS(startup, parts, nil, now, platform.OSLinux); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("prepared-static-input", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, _, err := prepared.BuildEffective(parts, now); err != nil {
				b.Fatal(err)
			}
		}
	})
}
