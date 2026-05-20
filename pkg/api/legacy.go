// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"
	"sort"
)

var removedLegacyKindMessages = map[string]string{
	"SystemdUnit":            "%s is not supported; declare router intent and let routerd generate service units",
	"KernelModule":           "%s is not supported; routerd derives required kernel modules from declared resources",
	"NetworkAdoption":        "%s is not supported; routerd derives networkd/resolved adoption from Interface and service resources",
	"NixOSHost":              "%s is not supported; use router resources and platform renderers instead of host implementation resources",
	"Link":                   "%s is not supported; use Interface resources as link status providers",
	"StatePolicy":            "%s is not supported; use spec.when any/all predicates on the dependent resources",
	"DHCPv4Lease":            "%s is not supported; use DHCPv4Client for routerd-managed DHCPv4 client intent",
	"PPPoEInterface":         "%s is not supported; use PPPoESession for routerd-managed PPPoE session intent",
	"IPv4SourceNAT":          "%s is not supported; use NAT44Rule for IPv4 source NAT intent",
	"VirtualIPv4Address":     "%s is not supported; use VirtualAddress with spec.family: ipv4",
	"VirtualIPv6Address":     "%s is not supported; use VirtualAddress with spec.family: ipv6",
	"DHCPv4Scope":            "%s is not supported; put the DHCPv4 address pool directly on DHCPv4Server",
	"DHCPv6Scope":            "%s is not supported; put the DHCPv6 delegatedAddress and options directly on DHCPv6Server",
	"IPv4DefaultRoutePolicy": "%s is not supported; use EgressRoutePolicy with candidates directly",
	"IPv4PolicyRoute":        "%s is not supported; use EgressRoutePolicy with one marked candidate",
	"IPv4PolicyRouteSet":     "%s is not supported; put hashFields and targets under EgressRoutePolicy candidates",
	"FirewallLog":            "%s is not supported; use FirewallEventLog for firewall event logging intent",
	"IPv4ReversePathFilter":  "%s is not supported; routerd derives reverse path filter sysctls automatically",
	"PathMTUPolicy":          "%s is not supported; routerd derives path MTU and TCP MSS handling from tunnel and interface resources",
}

func RemovedLegacyKinds() []string {
	kinds := make([]string, 0, len(removedLegacyKindMessages))
	for kind := range removedLegacyKindMessages {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	return kinds
}

func IsRemovedLegacyKind(kind string) bool {
	_, ok := removedLegacyKindMessages[kind]
	return ok
}

func RemovedLegacyKindError(kind, id string) error {
	format, ok := removedLegacyKindMessages[kind]
	if !ok {
		return fmt.Errorf("unsupported resource kind %s in %s", kind, id)
	}
	return fmt.Errorf(format, id)
}
