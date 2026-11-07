// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestConfigBoundariesRejectTrailingDocumentIntent(t *testing.T) {
	source := []byte("apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata: {name: first}\nspec: {resources: []}\n---\napiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata: {name: ignored}\nspec: {resources: []}\n")
	if _, err := LoadBytes(source, "operator"); err == nil {
		t.Fatal("load silently discarded second Router")
	}
	if _, err := CanonicalYAML(source); err == nil {
		t.Fatal("canonical write silently discarded second Router")
	}
	if _, err := NormalizedYAMLHash(source); err == nil {
		t.Fatal("hash silently ignored second Router")
	}
	if _, _, err := UpsertCandidateYAML(source, source, true); err == nil {
		t.Fatal("replace silently ignored second Router")
	}
}

func TestLoadRejectsUnknownResourceYAMLFieldsIncludingAliases(t *testing.T) {
	for _, test := range []struct{ field, resource string }{
		{"unknownResourceField", "    unknownResourceField: true\n    spec: {ifname: eth0}"},
		{"ifnmae", "    spec: {ifnmae: eth0}"},
	} {
		source := "apiVersion: routerd.net/v1alpha1\nkind: Router\nmetadata: {name: test}\nspec:\n  resources:\n  - apiVersion: net.routerd.net/v1alpha1\n    kind: Interface\n    metadata: {name: wan}\n" + test.resource + "\n"
		if _, err := LoadBytes([]byte(source), "operator"); err == nil || !strings.Contains(err.Error(), "field "+test.field+" not found") {
			t.Fatalf("unknown field check failed: %v\n%s", err, source)
		}
	}
	for _, typo := range []string{"password", "passwrod"} {
		source := fmt.Sprintf(`apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
  - apiVersion: net.routerd.net/v1alpha1
    kind: PPPoESession
    metadata: {name: first}
    spec: &credentials
      interface: wan
      username: user
      %s: 2026-01-01
  - apiVersion: net.routerd.net/v1alpha1
    kind: PPPoESession
    metadata: {name: second}
    spec: {<<: *credentials}
`, typo)
		router, err := LoadBytes([]byte(source), "operator")
		if typo != "password" {
			if err == nil {
				t.Fatal("unknown aliased spec field silently discarded")
			}
			continue
		}
		if err != nil {
			t.Fatalf("shared cross-resource anchor rejected: %v", err)
		}
		for _, resource := range router.Spec.Resources {
			spec, err := resource.PPPoESessionSpec()
			if err != nil || spec.Password != "2026-01-01" {
				t.Fatalf("anchor/scalar spelling changed: %#v, %v", spec, err)
			}
		}
	}
}

func TestLoadChecksNestedStructsAndAllowsExtensionMapKeys(t *testing.T) {
	const source = `apiVersion: routerd.net/v1alpha1
kind: Router
metadata: {name: test}
spec:
  resources:
  - apiVersion: observability.routerd.net/v1alpha1
    kind: ObservabilityPipeline
    metadata: {name: export, annotations: {my-arbitrary.annotation: retained}}
    spec:
      attributes: {my-custom-attribute: retained}
      otlp:
        headers: {My-Custom-Header: retained}
        tls: {%s: true}
`
	if _, err := LoadBytes([]byte(fmt.Sprintf(source, "insecureSkipVerify")), "operator"); err != nil {
		t.Fatalf("valid nested contract/extension keys rejected: %v", err)
	}
	if _, err := LoadBytes([]byte(fmt.Sprintf(source, "insecureSkipVerfiy")), "operator"); err == nil || !strings.Contains(err.Error(), "field insecureSkipVerfiy not found") {
		t.Fatalf("nested spec typo admitted: %v", err)
	}
}

func TestFreeBSDSAMTransportRejectsUnsupportedUDPEncapsulation(t *testing.T) {
	for _, mode := range []string{"fou", "gue"} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile"}, Metadata: api.ObjectMeta{Name: "transport"}, Spec: api.SAMTransportProfileSpec{Mode: mode}}}}}
		if err := ValidateForOS(router, platform.OSFreeBSD); err == nil || !strings.Contains(err.Error(), "unsupported on FreeBSD") {
			t.Fatalf("unsupported derived native tunnel admitted for mode %s: %v", mode, err)
		}
	}
}

func TestIPv4NativeResourcesRejectIPv6IntentAtAdmission(t *testing.T) {
	for _, resource := range []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "wrong-family"}, Spec: api.IPv4StaticAddressSpec{Interface: "wan", Address: "2001:db8::1/64"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"}, Metadata: api.ObjectMeta{Name: "wrong-family"}, Spec: api.IPv4RouteSpec{Destination: "2001:db8::/64", Device: "eth0"}},
	} {
		for _, target := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
			router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0"}}, resource}}}
			if err := ValidateForOS(router, target); err == nil || !strings.Contains(err.Error(), "IPv4 CIDR") {
				t.Fatalf("%s %s admitted IPv6 native intent: %v", target, resource.Kind, err)
			}
		}
	}
}

func TestControlAPIRejectsUnmaskedUniversalAllowCIDRs(t *testing.T) {
	for _, cidr := range []string{"203.0.113.42/0", "2001:db8::42/0"} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "ControlAPI"}, Metadata: api.ObjectMeta{Name: "admin"}, Spec: api.ControlAPISpec{AllowCIDRs: []string{cidr}}}}}}
		if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "must not be") {
			t.Fatalf("universal allow prefix with host bits %s admitted: %v", cidr, err)
		}
	}
}

func TestStaticAddressOwnershipUsesHostIdentity(t *testing.T) {
	base := api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "alias"}, Spec: api.InterfaceSpec{IfName: "eth0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "primary"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/24"}},
	}}}
	for _, test := range []struct {
		resource api.Resource
		allowed  bool
	}{
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "secondary"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.2/24"}}, true},
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"}, Metadata: api.ObjectMeta{Name: "duplicate"}, Spec: api.IPv4StaticAddressSpec{Interface: "lan", Address: "192.0.2.1/32"}}, false},
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "ha"}, Spec: api.VirtualAddressSpec{Interface: "lan", Family: "ipv4", Address: "192.0.2.1/32"}}, false},
		{api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "alias-ha"}, Spec: api.VirtualAddressSpec{Interface: "alias", Family: "ipv4", Address: "192.0.2.1/32"}}, false},
	} {
		router := base.Clone()
		router.Spec.Resources = append(router.Spec.Resources, test.resource)
		for _, target := range []platform.OS{platform.OSLinux, platform.OSFreeBSD} {
			if err := ValidateForOS(&router, target); (err == nil) != test.allowed {
				t.Fatalf("%s %s host ownership allowed=%v: %v", target, test.resource.Metadata.Name, test.allowed, err)
			}
		}
	}
}

func TestLogRetentionRejectsNoOpAndOverflowAtAdmission(t *testing.T) {
	for _, value := range []string{"-1d", "-1h", "200000d", "0", "0d"} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "LogRetention"}, Metadata: api.ObjectMeta{Name: "logs"}, Spec: api.LogRetentionSpec{Retention: value}}}}}
		if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "spec.retention") {
			t.Fatalf("nonfunctioning retention %s admitted: %v", value, err)
		}
	}
}

func TestConfigRejectsResourcePathEscapeAndInvalidNativeIfname(t *testing.T) {
	for _, name := range []string{"../outside", "..", "one/two", "bad\x00name", "bad\nname", " ", " wan", "wan "} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegation"}, Metadata: api.ObjectMeta{Name: name}, Spec: api.DHCPv6PrefixDelegationSpec{Interface: "wan"}}}}}
		if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "metadata.name") {
			t.Fatalf("unsafe resource path name %q admitted: %v", name, err)
		}
	}
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "test"}, Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.InterfaceSpec{IfName: "eth0\n[Network]\nDHCP=yes"}}}}}
	if err := ValidateForOS(router, platform.OSLinux); err == nil || !strings.Contains(err.Error(), "spec.ifname") {
		t.Fatalf("native config line injection admitted: %v", err)
	}
}
