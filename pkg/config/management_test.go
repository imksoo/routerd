// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"routerd/pkg/api"
)

func TestCheckManagementPlaneNoManagementAccess(t *testing.T) {
	router := testManagementRouter()
	if findings := CheckManagementPlane(router); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
}

func TestCheckManagementPlaneMissingInterfaceFails(t *testing.T) {
	router := testManagementRouter(
		managementAccess("main", []string{"mgmt0"}, nil),
	)
	findings := CheckManagementPlane(router)
	if !hasManagementFinding(findings, ManagementPlaneFail, "Interface/mgmt0", "not declared") {
		t.Fatalf("findings = %#v, want missing Interface fail", findings)
	}
}

func TestCheckManagementPlaneFirewallZoneRole(t *testing.T) {
	router := testManagementRouter(
		netInterface("mgmt0"),
		managementAccess("main", []string{"mgmt0"}, nil),
		firewallZone("wan", "untrust", []string{"mgmt0"}),
	)
	findings := CheckManagementPlane(router)
	if !hasManagementFinding(findings, ManagementPlaneFail, "Interface/mgmt0", "not in a trust or mgmt FirewallZone") {
		t.Fatalf("findings = %#v, want untrust zone fail", findings)
	}

	router = testManagementRouter(
		netInterface("mgmt0"),
		managementAccess("main", []string{"Interface/mgmt0"}, nil),
		firewallZone("mgmt", "mgmt", []string{"Interface/mgmt0"}),
	)
	if findings := CheckManagementPlane(router); len(findings) != 0 {
		t.Fatalf("findings = %#v, want none", findings)
	}
}

func TestCheckManagementPlaneWebConsoleWildcardFailsByDefault(t *testing.T) {
	router := testManagementRouter(
		netInterface("mgmt0"),
		managementAccess("main", []string{"mgmt0"}, nil),
		webConsole("console", "0.0.0.0"),
	)
	findings := CheckManagementPlane(router)
	if !hasManagementFinding(findings, ManagementPlaneFail, "WebConsole/console", "all addresses") {
		t.Fatalf("findings = %#v, want wildcard WebConsole fail", findings)
	}
}

func TestCheckManagementPlaneWebConsoleWildcardWarnsWhenAllowed(t *testing.T) {
	router := testManagementRouter(
		netInterface("mgmt0"),
		managementAccess("main", []string{"mgmt0"}, boolPtr(false)),
		webConsole("console", "::"),
	)
	findings := CheckManagementPlane(router)
	if !hasManagementFinding(findings, ManagementPlaneWarn, "WebConsole/console", "all addresses") {
		t.Fatalf("findings = %#v, want wildcard WebConsole warning", findings)
	}
}

func testManagementRouter(resources ...api.Resource) *api.Router {
	return &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: "test"},
		Spec:     api.RouterSpec{Resources: resources},
	}
}

func netInterface(name string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.InterfaceSpec{IfName: name, Managed: true},
	}
}

func managementAccess(name string, interfaces []string, requireBound *bool) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "ManagementAccess"},
		Metadata: api.ObjectMeta{Name: name},
		Spec: api.ManagementAccessSpec{
			Interfaces:             interfaces,
			RequireWebConsoleBound: requireBound,
		},
	}
}

func firewallZone(name, role string, interfaces []string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.FirewallZoneSpec{Role: role, Interfaces: interfaces},
	}
}

func webConsole(name, listen string) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "WebConsole"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.WebConsoleSpec{ListenAddress: listen},
	}
}

func hasManagementFinding(findings []ManagementPlaneFinding, severity, resource, message string) bool {
	for _, finding := range findings {
		if finding.Severity == severity && finding.Resource == resource && strings.Contains(finding.Message, message) {
			return true
		}
	}
	return false
}
