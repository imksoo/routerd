// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	ManagementPlaneFail = "fail"
	ManagementPlaneWarn = "warn"
)

type ManagementPlaneFinding struct {
	Severity string
	Resource string
	Message  string
	Remedy   string
}

type managementResource struct {
	spec api.ManagementAccessSpec
}

func CheckManagementPlane(router *api.Router) []ManagementPlaneFinding {
	if router == nil {
		return nil
	}

	interfaces := map[string]bool{}
	var management []managementResource

	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Interface":
			if res.APIVersion == api.NetAPIVersion {
				interfaces[res.Metadata.Name] = true
			}
		case "ManagementAccess":
			spec, err := res.ManagementAccessSpec()
			if err != nil {
				return []ManagementPlaneFinding{{
					Severity: ManagementPlaneFail,
					Resource: res.ID(),
					Message:  err.Error(),
					Remedy:   "fix the ManagementAccess spec before applying",
				}}
			}
			management = append(management, managementResource{spec: spec})
		}
	}
	if len(management) == 0 {
		return nil
	}

	firewallEnabled, firewallMembership := managementFirewallMembership(router)
	requireWebConsoleBound := managementRequiresWebConsoleBound(management)
	var findings []ManagementPlaneFinding
	seenMissing := map[string]bool{}
	seenFirewall := map[string]bool{}

	for _, declared := range management {
		for _, ref := range declared.spec.Interfaces {
			_, name := splitFirewallInterfaceRef(ref)
			if !interfaces[name] {
				if !seenMissing[name] {
					findings = append(findings, ManagementPlaneFinding{
						Severity: ManagementPlaneFail,
						Resource: "Interface/" + name,
						Message:  fmt.Sprintf("management interface %q is not declared as an Interface resource", name),
						Remedy:   "declare the Interface resource or remove it from ManagementAccess",
					})
					seenMissing[name] = true
				}
				continue
			}
			if firewallEnabled && !seenFirewall[name] {
				zones := firewallMembership[name]
				if !managementFirewallAllowsSelf(zones) {
					findings = append(findings, ManagementPlaneFinding{
						Severity: ManagementPlaneFail,
						Resource: "Interface/" + name,
						Message:  fmt.Sprintf("management interface %q is not in a trust or mgmt FirewallZone; current zones: %s", name, managementZoneSummary(zones)),
						Remedy:   "add the interface to a FirewallZone with role mgmt or trust before applying firewall changes",
					})
					seenFirewall[name] = true
				}
			}
		}
	}

	findings = append(findings, checkManagementWebConsole(router, requireWebConsoleBound)...)
	return findings
}

type managementZone struct {
	name string
	role string
}

func managementFirewallMembership(router *api.Router) (bool, map[string][]managementZone) {
	members := map[string][]managementZone{}
	enabled := false
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallZone" {
			continue
		}
		enabled = true
		spec, err := res.FirewallZoneSpec()
		if err != nil {
			continue
		}
		for _, ref := range spec.Interfaces {
			kind, name := splitFirewallInterfaceRef(ref)
			if kind != "Interface" {
				continue
			}
			members[name] = append(members[name], managementZone{name: res.Metadata.Name, role: spec.Role})
		}
	}
	return enabled, members
}

func managementFirewallAllowsSelf(zones []managementZone) bool {
	for _, zone := range zones {
		if zone.role == "mgmt" || zone.role == "trust" {
			return true
		}
	}
	return false
}

func managementZoneSummary(zones []managementZone) string {
	if len(zones) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(zones))
	for _, zone := range zones {
		parts = append(parts, fmt.Sprintf("FirewallZone/%s role=%s", zone.name, zone.role))
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func managementRequiresWebConsoleBound(management []managementResource) bool {
	for _, declared := range management {
		if declared.spec.RequireWebConsoleBound == nil || *declared.spec.RequireWebConsoleBound {
			return true
		}
	}
	return false
}

func checkManagementWebConsole(router *api.Router, requireBound bool) []ManagementPlaneFinding {
	var findings []ManagementPlaneFinding
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.SystemAPIVersion || res.Kind != "WebConsole" {
			continue
		}
		spec, err := res.WebConsoleSpec()
		if err != nil {
			findings = append(findings, ManagementPlaneFinding{
				Severity: ManagementPlaneFail,
				Resource: resourceShortName(res),
				Message:  err.Error(),
				Remedy:   "fix the WebConsole spec before applying",
			})
			continue
		}
		if spec.Enabled != nil && !*spec.Enabled {
			continue
		}
		listen := strings.TrimSpace(spec.ListenAddress)
		if listen == "" {
			if strings.TrimSpace(spec.ListenAddressFrom.Resource) == "" {
				continue
			}
			findings = append(findings, ManagementPlaneFinding{
				Severity: ManagementPlaneWarn,
				Resource: resourceShortName(res),
				Message:  "WebConsole listen address is empty or derived; management binding cannot be checked from config alone",
				Remedy:   "set spec.listenAddress to a management or LAN address if WebConsole is enabled",
			})
			continue
		}
		if listen != "0.0.0.0" && listen != "::" {
			continue
		}
		severity := ManagementPlaneWarn
		remedy := "bind WebConsole to a management or LAN address"
		if requireBound {
			severity = ManagementPlaneFail
			remedy = "set spec.listenAddress to a management or LAN address, or set ManagementAccess spec.requireWebConsoleBound=false"
		}
		findings = append(findings, ManagementPlaneFinding{
			Severity: severity,
			Resource: resourceShortName(res),
			Message:  fmt.Sprintf("WebConsole listens on all addresses (%s)", listen),
			Remedy:   remedy,
		})
	}
	return findings
}

func resourceShortName(res api.Resource) string {
	return res.Kind + "/" + res.Metadata.Name
}
