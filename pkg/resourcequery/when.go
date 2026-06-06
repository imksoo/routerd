// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type StateStore interface {
	Get(name string) routerstate.Value
	Age(name string) time.Duration
	Now() time.Time
}

type WhenStore interface {
	StateStore
	Store
}

func FilterRouterByWhen(router *api.Router, store StateStore) *api.Router {
	if router == nil {
		return nil
	}
	declaredBFDs := bfdResourceNames(router.Spec.Resources)
	filtered := *router
	filtered.Spec.Resources = nil
	for _, res := range router.Spec.Resources {
		when := ResourceWhen(res)
		if ResourceWhenMatches(when, store) {
			if res.Kind == "EgressRoutePolicy" {
				res = filterEgressRoutePolicyCandidatesByWhen(res, store)
			}
			filtered.Spec.Resources = append(filtered.Spec.Resources, res)
		}
	}
	filtered.Spec.Resources = clearFilteredBFDRefs(filtered.Spec.Resources, declaredBFDs)
	filtered.Spec.Resources = clearFilteredDNSRefs(filtered.Spec.Resources)
	filtered.Spec.Resources = clearFilteredFirewallZoneRefs(filtered.Spec.Resources)
	return api.ExpandClusterNetworkRoutes(&filtered)
}

func ResourceWhen(res api.Resource) api.ResourceWhenSpec {
	switch res.Kind {
	case "ObservabilityPipeline":
		spec, _ := res.ObservabilityPipelineSpec()
		return spec.When
	case "RouterdCluster":
		spec, _ := res.RouterdClusterSpec()
		return spec.When
	case "VirtualAddress":
		spec, _ := res.VirtualAddressSpec()
		return spec.When
	case "BGPRouter":
		spec, _ := res.BGPRouterSpec()
		return spec.When
	case "BGPPeer":
		spec, _ := res.BGPPeerSpec()
		return spec.When
	case "BFD":
		spec, _ := res.BFDSpec()
		return spec.When
	case "TailscaleNode":
		spec, _ := res.TailscaleNodeSpec()
		return spec.When
	case "NTPServer":
		spec, _ := res.NTPServerSpec()
		return spec.When
	case "DHCPv4Client":
		spec, _ := res.DHCPv4ClientSpec()
		return spec.When
	case "ClusterNetworkRoute":
		spec, _ := res.ClusterNetworkRouteSpec()
		return spec.When
	case "DHCPv4Server":
		spec, _ := res.DHCPv4ServerSpec()
		return spec.When
	case "DHCPv4Reservation":
		spec, _ := res.DHCPv4ReservationSpec()
		return spec.When
	case "IPv6DelegatedAddress":
		spec, _ := res.IPv6DelegatedAddressSpec()
		return spec.When
	case "DHCPv6Server":
		spec, _ := res.DHCPv6ServerSpec()
		return spec.When
	case "DHCPv4ServerLeaseSync":
		spec, _ := res.DHCPv4ServerLeaseSyncSpec()
		return spec.When
	case "DHCPv6ServerLeaseSync":
		spec, _ := res.DHCPv6ServerLeaseSyncSpec()
		return spec.When
	case "DHCPv6PrefixDelegationLeaseSync":
		spec, _ := res.DHCPv6PrefixDelegationLeaseSyncSpec()
		return spec.When
	case "DHCPv6PrefixDelegation":
		spec, _ := res.DHCPv6PrefixDelegationSpec()
		return spec.When
	case "IPv6RouterAdvertisement":
		spec, _ := res.IPv6RouterAdvertisementSpec()
		return spec.When
	case "DSLiteTunnel":
		spec, _ := res.DSLiteTunnelSpec()
		return spec.When
	case "DNSForwarder":
		spec, _ := res.DNSForwarderSpec()
		return spec.When
	case "DNSResolver":
		spec, _ := res.DNSResolverSpec()
		return spec.When
	case "DNSUpstream":
		spec, _ := res.DNSUpstreamSpec()
		return spec.When
	case "EventGroup":
		spec, _ := res.EventGroupSpec()
		return spec.When
	case "HealthCheck":
		spec, _ := res.HealthCheckSpec()
		return spec.When
	case "NAT44Rule":
		spec, _ := res.NAT44RuleSpec()
		return spec.When
	case "NAT44SessionSync":
		spec, _ := res.NAT44SessionSyncSpec()
		return spec.When
	case "PortForward":
		spec, _ := res.PortForwardSpec()
		return spec.When
	case "IngressService":
		spec, _ := res.IngressServiceSpec()
		return spec.When
	case "IPAddressSet":
		spec, _ := res.IPAddressSetSpec()
		return spec.When
	case "LocalServiceRedirect":
		spec, _ := res.LocalServiceRedirectSpec()
		return spec.When
	case "EgressRoutePolicy":
		spec, _ := res.EgressRoutePolicySpec()
		return spec.When
	default:
		return api.ResourceWhenSpec{}
	}
}

func ResourceWhenMatches(when api.ResourceWhenSpec, store StateStore) bool {
	if len(when.All) > 0 {
		for _, child := range when.All {
			if !ResourceWhenMatches(child, store) {
				return false
			}
		}
		return true
	}
	if len(when.Any) > 0 {
		for _, child := range when.Any {
			if ResourceWhenMatches(child, store) {
				return true
			}
		}
		return false
	}
	if len(when.State) == 0 {
		return true
	}
	for name, match := range when.State {
		if !StateMatch(store, name, match) {
			return false
		}
	}
	return true
}

func ResourceWhenPresent(when api.ResourceWhenSpec) bool {
	return len(when.All) > 0 || len(when.Any) > 0 || len(when.State) > 0
}

func StateMatch(store StateStore, name string, match api.StateMatchSpec) bool {
	if store == nil {
		return false
	}
	value, age, hasCustomAge := stateValue(store, name)
	ok := true
	if match.Status != "" {
		ok = ok && value.Status == match.Status
	}
	if match.Exists != nil {
		if *match.Exists {
			ok = ok && value.Status == routerstate.StatusSet
		} else {
			ok = ok && value.Status == routerstate.StatusUnset
		}
	}
	if match.Equals != "" {
		ok = ok && value.Status == routerstate.StatusSet && value.Value == match.Equals
	}
	if len(match.In) > 0 {
		ok = ok && value.Status == routerstate.StatusSet && stringIn(value.Value, match.In)
	}
	if match.Contains != "" {
		ok = ok && value.Status == routerstate.StatusSet && strings.Contains(value.Value, match.Contains)
	}
	if !ok {
		return false
	}
	if match.For != "" {
		duration, err := time.ParseDuration(match.For)
		if err != nil {
			return false
		}
		if hasCustomAge {
			return age >= duration
		}
		return store.Age(name) >= duration
	}
	return true
}

func filterEgressRoutePolicyCandidatesByWhen(res api.Resource, store StateStore) api.Resource {
	spec, err := res.EgressRoutePolicySpec()
	if err != nil {
		return res
	}
	var candidates []api.EgressRoutePolicyCandidate
	for _, candidate := range spec.Candidates {
		if !api.BoolDefault(candidate.Enabled, true) {
			continue
		}
		if ResourceWhenMatches(candidate.When, store) {
			candidates = append(candidates, candidate)
		}
	}
	spec.Candidates = candidates
	res.Spec = spec
	return res
}

func bfdResourceNames(resources []api.Resource) map[string]bool {
	bfds := map[string]bool{}
	for _, res := range resources {
		if res.APIVersion == api.NetAPIVersion && res.Kind == "BFD" {
			name := strings.TrimSpace(res.Metadata.Name)
			if name != "" {
				bfds[name] = true
			}
		}
	}
	return bfds
}

func clearFilteredBFDRefs(resources []api.Resource, declaredBFDs map[string]bool) []api.Resource {
	retainedBFDs := bfdResourceNames(resources)
	out := make([]api.Resource, 0, len(resources))
	for _, res := range resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "BGPPeer" {
			out = append(out, res)
			continue
		}
		spec, err := res.BGPPeerSpec()
		if err != nil || strings.TrimSpace(spec.BFD) == "" {
			out = append(out, res)
			continue
		}
		kind, name, ok := SplitResource(spec.BFD)
		if !ok || kind != "BFD" || !declaredBFDs[name] || retainedBFDs[name] {
			out = append(out, res)
			continue
		}
		spec.BFD = ""
		res.Spec = spec
		out = append(out, res)
	}
	return out
}

func clearFilteredDNSRefs(resources []api.Resource) []api.Resource {
	resolvers := dnsResourceNames(resources, "DNSResolver")
	upstreams := dnsResourceNames(resources, "DNSUpstream")
	zones := dnsResourceNames(resources, "DNSZone")
	retained := resourceNamesByKind(resources)
	resources = clearFilteredDNSZoneStatusRefs(resources, retained)
	forwarders := map[string]bool{}
	prunedForwarders := make([]api.Resource, 0, len(resources))
	for _, res := range resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "DNSForwarder" {
			prunedForwarders = append(prunedForwarders, res)
			continue
		}
		spec, err := res.DNSForwarderSpec()
		if err != nil || !dnsForwarderRefsRetained(spec, resolvers, upstreams, zones) {
			continue
		}
		forwarders[res.Metadata.Name] = true
		prunedForwarders = append(prunedForwarders, res)
	}
	out := make([]api.Resource, 0, len(prunedForwarders))
	for _, res := range prunedForwarders {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "DNSResolver" {
			out = append(out, res)
			continue
		}
		spec, err := res.DNSResolverSpec()
		if err != nil {
			out = append(out, res)
			continue
		}
		spec.Listen = filterDNSResolverListenSources(spec.Listen, forwarders)
		res.Spec = spec
		out = append(out, res)
	}
	return out
}

func clearFilteredDNSZoneStatusRefs(resources []api.Resource, retained map[string]map[string]bool) []api.Resource {
	out := make([]api.Resource, 0, len(resources))
	for _, res := range resources {
		if res.APIVersion != api.NetAPIVersion || res.Kind != "DNSZone" {
			out = append(out, res)
			continue
		}
		spec, err := res.DNSZoneSpec()
		if err != nil {
			out = append(out, res)
			continue
		}
		for i := range spec.Records {
			if !statusValueSourceRetained(spec.Records[i].IPv4From, retained) {
				spec.Records[i].IPv4From = api.StatusValueSourceSpec{}
			}
			if !statusValueSourceRetained(spec.Records[i].IPv6From, retained) {
				spec.Records[i].IPv6From = api.StatusValueSourceSpec{}
			}
		}
		res.Spec = spec
		out = append(out, res)
	}
	return out
}

func dnsResourceNames(resources []api.Resource, kind string) map[string]bool {
	out := map[string]bool{}
	for _, res := range resources {
		if res.APIVersion == api.NetAPIVersion && res.Kind == kind && strings.TrimSpace(res.Metadata.Name) != "" {
			out[res.Metadata.Name] = true
		}
	}
	return out
}

func resourceNamesByKind(resources []api.Resource) map[string]map[string]bool {
	out := map[string]map[string]bool{}
	for _, res := range resources {
		name := strings.TrimSpace(res.Metadata.Name)
		if name == "" {
			continue
		}
		if out[res.Kind] == nil {
			out[res.Kind] = map[string]bool{}
		}
		out[res.Kind][name] = true
	}
	return out
}

func statusValueSourceRetained(source api.StatusValueSourceSpec, retained map[string]map[string]bool) bool {
	ref := strings.TrimSpace(source.Resource)
	if ref == "" {
		return true
	}
	kind, name, ok := SplitResource(ref)
	if !ok {
		return true
	}
	return retained[kind][name]
}

func clearFilteredFirewallZoneRefs(resources []api.Resource) []api.Resource {
	retained := resourceNamesByKind(resources)
	out := make([]api.Resource, 0, len(resources))
	for _, res := range resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallZone" {
			out = append(out, res)
			continue
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil {
			out = append(out, res)
			continue
		}
		spec.Interfaces = filterRetainedFirewallInterfaceRefs(spec.Interfaces, retained)
		res.Spec = spec
		out = append(out, res)
	}
	return out
}

func filterRetainedFirewallInterfaceRefs(refs []string, retained map[string]map[string]bool) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		kind, name := firewallInterfaceRefKindName(ref)
		switch kind {
		case "Interface", "PPPoESession", "WireGuardInterface", "DSLiteTunnel":
			if retained[kind][name] {
				out = append(out, ref)
			}
		default:
			out = append(out, ref)
		}
	}
	return out
}

func firewallInterfaceRefKindName(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	if kind, name, ok := SplitResource(ref); ok {
		return kind, name
	}
	return "Interface", ref
}

func dnsForwarderRefsRetained(spec api.DNSForwarderSpec, resolvers, upstreams, zones map[string]bool) bool {
	if strings.TrimSpace(spec.Resolver) != "" && !resolvers[dnsRefName(spec.Resolver)] {
		return false
	}
	for _, ref := range spec.Upstreams {
		if !upstreams[dnsRefName(ref)] {
			return false
		}
	}
	for _, ref := range spec.ZoneRefs {
		if !zones[dnsRefName(ref)] {
			return false
		}
	}
	return true
}

func filterDNSResolverListenSources(listens []api.DNSResolverListenSpec, forwarders map[string]bool) []api.DNSResolverListenSpec {
	out := make([]api.DNSResolverListenSpec, 0, len(listens))
	for _, listen := range listens {
		if len(listen.Sources) == 0 {
			out = append(out, listen)
			continue
		}
		sources := make([]string, 0, len(listen.Sources))
		for _, source := range listen.Sources {
			if forwarders[dnsRefName(source)] {
				sources = append(sources, source)
			}
		}
		listen.Sources = sources
		out = append(out, listen)
	}
	return out
}

func dnsRefName(ref string) string {
	ref = strings.TrimSpace(ref)
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func stateValue(store StateStore, name string) (routerstate.Value, time.Duration, bool) {
	if value, age, ok := objectStatusStateValue(store, name); ok {
		return value, age, true
	}
	return store.Get(name), 0, false
}

func objectStatusStateValue(store StateStore, name string) (routerstate.Value, time.Duration, bool) {
	statusStore, ok := store.(Store)
	if !ok {
		return routerstate.Value{}, 0, false
	}
	kind, resourceName, field, ok := splitStatusStateRef(name)
	if !ok {
		return routerstate.Value{}, 0, false
	}
	status := statusStore.ObjectStatus(APIVersionForKind(kind), kind, resourceName)
	if len(status) == 0 {
		return routerstate.Value{}, 0, false
	}
	values := normalizeValues(status[field])
	if len(values) == 0 {
		return routerstate.Value{}, 0, false
	}
	now := store.Now().UTC()
	since := objectStatusFieldSince(status, field, now)
	return routerstate.Value{
		Status:    routerstate.StatusSet,
		Value:     values[0],
		Reason:    fmt.Sprintf("%s/%s.status.%s", kind, resourceName, field),
		Since:     since,
		UpdatedAt: now,
	}, now.Sub(since), true
}

func splitStatusStateRef(ref string) (string, string, string, bool) {
	ref = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(ref, "${"), "}"))
	if left, field, ok := strings.Cut(ref, ".status."); ok {
		kind, name, ok := SplitResource(left)
		return kind, name, strings.TrimSpace(field), ok && strings.TrimSpace(field) != ""
	}
	left, field, ok := strings.Cut(ref, ".")
	if !ok || strings.TrimSpace(field) == "" {
		return "", "", "", false
	}
	kind, name, ok := SplitResource(left)
	return kind, name, strings.TrimSpace(field), ok
}

func objectStatusFieldSince(status map[string]any, field string, now time.Time) time.Time {
	transitionKey := "last" + strings.ToUpper(field[:1]) + field[1:] + "TransitionAt"
	for _, key := range []string{transitionKey, "lastTransitionAt", "observedAt", "updatedAt"} {
		if parsed, ok := parseStatusTime(status[key]); ok {
			return parsed
		}
	}
	return now
}

func parseStatusTime(value any) (time.Time, bool) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" || text == "<nil>" {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, false
	}
	return parsed.UTC(), true
}

func stringIn(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
