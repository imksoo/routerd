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
	case "DHCPv4Client":
		spec, _ := res.DHCPv4ClientSpec()
		return spec.When
	case "ClusterNetworkRoute":
		spec, _ := res.ClusterNetworkRouteSpec()
		return spec.When
	case "DHCPv4Server":
		spec, _ := res.DHCPv4ServerSpec()
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
