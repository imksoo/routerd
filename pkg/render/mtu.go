// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
)

type pathMTUPolicy struct {
	ResourceID string
	Spec       pathMTUPolicySpec
	MTU        int
}

type pathMTUPolicySpec struct {
	FromInterface string
	ToInterfaces  []string
	MTU           int
	IPv6RA        pathMTUPolicyIPv6RASpec
	TCPMSSClamp   pathMTUPolicyTCPMSSSpec
}

type pathMTUPolicyIPv6RASpec struct {
	Enabled bool
	Scope   string
}

type pathMTUPolicyTCPMSSSpec struct {
	Enabled  bool
	Families []string
}

type pathMTUTunnel struct {
	Name     string
	Underlay string
	MTU      int
}

type pathMTUForwardedPath struct {
	FromInterface string
	ToInterface   string
	MTU           int
}

func pathMTUPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	mtus, err := resourceMTUs(router)
	if err != nil {
		return nil, err
	}
	var policies []pathMTUPolicy
	for _, spec := range derivedPathMTUPolicySpecs(router, mtus) {
		if len(spec.ToInterfaces) == 0 {
			continue
		}
		sourceMTU := mtus[spec.FromInterface]
		if sourceMTU == 0 {
			return nil, fmt.Errorf("%s references fromInterface with unknown MTU %q", specResourceID(spec), spec.FromInterface)
		}
		toInterfacesByMTU := map[int][]string{}
		for _, name := range spec.ToInterfaces {
			candidate := mtus[name]
			if spec.MTU > 0 {
				candidate = spec.MTU
			}
			if candidate == 0 {
				return nil, fmt.Errorf("%s references interface with unknown MTU %q", specResourceID(spec), name)
			}
			mtu := candidate
			if sourceMTU < mtu {
				mtu = sourceMTU
			}
			if mtu < 1280 {
				return nil, fmt.Errorf("%s computed MTU %d is below the IPv6 minimum MTU 1280", specResourceID(spec), mtu)
			}
			toInterfacesByMTU[mtu] = append(toInterfacesByMTU[mtu], name)
		}
		var mtusForSpec []int
		for mtu := range toInterfacesByMTU {
			mtusForSpec = append(mtusForSpec, mtu)
		}
		sort.Ints(mtusForSpec)
		for _, mtu := range mtusForSpec {
			grouped := spec
			grouped.ToInterfaces = compactStrings(sortedStrings(toInterfacesByMTU[mtu]))
			policies = append(policies, pathMTUPolicy{ResourceID: specResourceID(spec), Spec: grouped, MTU: mtu})
		}
	}
	sort.Slice(policies, func(i, j int) bool {
		if policies[i].ResourceID == policies[j].ResourceID {
			return policies[i].MTU < policies[j].MTU
		}
		return policies[i].ResourceID < policies[j].ResourceID
	})
	return policies, nil
}

func resourceMTUs(router *api.Router) (map[string]int, error) {
	mtus := map[string]int{}
	for _, iface := range pathMTUResourceInterfaces(router) {
		mtus[iface.Name] = iface.MTU
	}
	for _, iface := range pathMTUForwardedPathInterfaces(router) {
		if mtus[iface] == 0 {
			mtus[iface] = 1500
		}
	}
	return mtus, nil
}

func derivedPathMTUPolicySpecs(router *api.Router, mtus map[string]int) []pathMTUPolicySpec {
	tunnels := pathMTUTunnels(router)
	forwardedPathPolicies := derivedForwardedPathMTUPolicySpecs(router, mtus)
	if len(tunnels) == 0 {
		return forwardedPathPolicies
	}
	sources := pathMTUSourceInterfaces(router)
	if len(sources) == 0 {
		return forwardedPathPolicies
	}
	untrust := pathMTUUntrustInterfaces(router)
	var tunnelTargets []string
	for _, tunnel := range tunnels {
		if len(untrust) > 0 && !untrust[tunnel.Name] {
			continue
		}
		tunnelTargets = append(tunnelTargets, tunnel.Name)
		if tunnel.Underlay != "" && (len(untrust) == 0 || untrust[tunnel.Underlay]) {
			tunnelTargets = append(tunnelTargets, tunnel.Underlay)
		}
	}
	tunnelTargets = compactStrings(sortedStrings(tunnelTargets))
	if len(tunnelTargets) == 0 {
		return forwardedPathPolicies
	}
	raScopes := pathMTURAScopesByInterface(router)
	var policies []pathMTUPolicySpec
	for _, source := range sources {
		spec := pathMTUPolicySpec{
			FromInterface: source,
			ToInterfaces:  tunnelTargets,
			TCPMSSClamp: pathMTUPolicyTCPMSSSpec{
				Enabled:  true,
				Families: []string{"ipv4", "ipv6"},
			},
		}
		if scope := raScopes[source]; scope != "" {
			spec.IPv6RA = pathMTUPolicyIPv6RASpec{Enabled: true, Scope: scope}
		}
		policies = append(policies, spec)
	}
	policies = append(policies, forwardedPathPolicies...)
	return compactPathMTUPolicySpecs(policies)
}

func derivedForwardedPathMTUPolicySpecs(router *api.Router, mtus map[string]int) []pathMTUPolicySpec {
	var policies []pathMTUPolicySpec
	for _, path := range pathMTUForwardedPaths(router) {
		if path.FromInterface == "" || path.ToInterface == "" || path.FromInterface == path.ToInterface {
			continue
		}
		fromMTU := mtus[path.FromInterface]
		toMTU := path.MTU
		if toMTU == 0 {
			toMTU = mtus[path.ToInterface]
		}
		if fromMTU == 0 || toMTU == 0 || toMTU >= fromMTU {
			continue
		}
		clamp := pathMTUPolicyTCPMSSSpec{Enabled: true, Families: []string{"ipv4"}}
		policies = append(policies, pathMTUPolicySpec{FromInterface: path.FromInterface, ToInterfaces: []string{path.ToInterface}, MTU: toMTU, TCPMSSClamp: clamp})
	}
	return compactPathMTUPolicySpecs(policies)
}

func pathMTUForwardedPaths(router *api.Router) []pathMTUForwardedPath {
	if router == nil {
		return nil
	}
	peers := pathMTUOverlayPeers(router)
	defaultSources := pathMTUDefaultForwardedPathSourceInterfaces(router)
	paths := pathMTUBGPMobilityForwardedPaths(router, peers)
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion || res.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := res.RemoteAddressClaimSpec()
		if err != nil {
			continue
		}
		peer := peers[refName(spec.Delivery.PeerRef)]
		tunnel := firstNonEmpty(strings.TrimSpace(spec.Delivery.TunnelInterface), strings.TrimSpace(peer.Underlay.Interface))
		if tunnel == "" {
			continue
		}
		tunnelMTU := pathMTUOverlayPeerEffectiveMTU(router, refName(spec.Delivery.PeerRef))
		for _, source := range pathMTUClaimSourceInterfaces(spec, defaultSources) {
			if source == "" || source == tunnel {
				continue
			}
			paths = append(paths, pathMTUForwardedPath{FromInterface: source, ToInterface: tunnel, MTU: tunnelMTU})
		}
	}
	return compactForwardedPaths(paths)
}

func pathMTUBGPMobilityForwardedPaths(router *api.Router, peers map[string]api.OverlayPeerSpec) []pathMTUForwardedPath {
	if router == nil {
		return nil
	}
	var paths []pathMTUForwardedPath
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.MobilityAPIVersion || res.Kind != "MobilityPool" {
			continue
		}
		spec, err := res.MobilityPoolSpec()
		if err != nil || strings.TrimSpace(spec.DeliveryPolicy.Mode) != "bgp" {
			continue
		}
		selfNode := pathMTUSelfNode(router, spec.GroupRef)
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		for _, member := range spec.Members {
			if strings.TrimSpace(member.NodeRef) != selfNode {
				continue
			}
			source := strings.TrimSpace(member.Capture.Interface)
			if source == "" {
				break
			}
			deliveries := pathMTUMemberDeliveries(member)
			if len(deliveries) == 0 {
				for peerName, peer := range peers {
					tunnel := strings.TrimSpace(peer.Underlay.Interface)
					if tunnel == "" || tunnel == source {
						continue
					}
					paths = append(paths, pathMTUForwardedPath{FromInterface: source, ToInterface: tunnel, MTU: pathMTUOverlayPeerEffectiveMTU(router, peerName)})
				}
				break
			}
			for _, delivery := range deliveries {
				tunnel := firstNonEmpty(strings.TrimSpace(delivery.TunnelInterface), strings.TrimSpace(peers[refName(delivery.PeerRef)].Underlay.Interface))
				if tunnel == "" || tunnel == source {
					continue
				}
				tunnelMTU := 0
				if strings.TrimSpace(delivery.PeerRef) != "" {
					tunnelMTU = pathMTUOverlayPeerEffectiveMTU(router, refName(delivery.PeerRef))
				}
				paths = append(paths, pathMTUForwardedPath{FromInterface: source, ToInterface: tunnel, MTU: tunnelMTU})
			}
			break
		}
	}
	return compactForwardedPaths(paths)
}

func pathMTUSelfNode(router *api.Router, groupRef string) string {
	if router == nil || strings.TrimSpace(groupRef) == "" {
		return ""
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FederationAPIVersion || res.Kind != "EventGroup" || res.Metadata.Name != strings.TrimSpace(groupRef) {
			continue
		}
		spec, err := res.EventGroupSpec()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(spec.NodeName)
	}
	return ""
}

func pathMTUMemberDeliveries(member api.MobilityPoolMember) []api.MobilityMemberDelivery {
	var out []api.MobilityMemberDelivery
	if strings.TrimSpace(member.Delivery.PeerRef) != "" || strings.TrimSpace(member.Delivery.TunnelInterface) != "" {
		out = append(out, member.Delivery)
	}
	for _, target := range member.DeliveryTo {
		delivery := api.MobilityMemberDelivery{
			PeerRef:         target.PeerRef,
			Mode:            target.Mode,
			TunnelInterface: target.TunnelInterface,
		}
		if strings.TrimSpace(delivery.PeerRef) != "" || strings.TrimSpace(delivery.TunnelInterface) != "" {
			out = append(out, delivery)
		}
	}
	return out
}

func pathMTUOverlayPeerEffectiveMTU(router *api.Router, peerName string) int {
	if router == nil || strings.TrimSpace(peerName) == "" {
		return 0
	}
	estimate, ok := hybrid.EstimateMTU(*router, peerName)
	if !ok || estimate.EstimatedMTU <= 0 {
		return 0
	}
	return estimate.EstimatedMTU
}

func pathMTUOverlayPeers(router *api.Router) map[string]api.OverlayPeerSpec {
	out := map[string]api.OverlayPeerSpec{}
	if router == nil {
		return out
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion || res.Kind != "OverlayPeer" {
			continue
		}
		spec, err := res.OverlayPeerSpec()
		if err == nil {
			out[res.Metadata.Name] = spec
		}
	}
	return out
}

func pathMTUClaimSourceInterfaces(spec api.RemoteAddressClaimSpec, defaults []string) []string {
	if iface := strings.TrimSpace(spec.Capture.Interface); iface != "" {
		return []string{iface}
	}
	if strings.TrimSpace(spec.Capture.Type) == "provider-secondary-ip" {
		return defaults
	}
	return nil
}

func pathMTUForwardedPathInterfaces(router *api.Router) []string {
	if router == nil {
		return nil
	}
	var out []string
	for _, path := range pathMTUForwardedPaths(router) {
		out = append(out, path.FromInterface, path.ToInterface)
	}
	return compactStrings(sortedStrings(out))
}

func pathMTUDefaultForwardedPathSourceInterfaces(router *api.Router) []string {
	if router == nil {
		return nil
	}
	var out []string
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.NetAPIVersion && res.Kind == "Interface" {
			out = append(out, res.Metadata.Name)
		}
	}
	return compactStrings(sortedStrings(out))
}

func compactForwardedPaths(paths []pathMTUForwardedPath) []pathMTUForwardedPath {
	byKey := map[string]pathMTUForwardedPath{}
	for _, path := range paths {
		key := path.FromInterface + ">" + path.ToInterface
		existing, ok := byKey[key]
		if ok && (existing.MTU == 0 || (path.MTU != 0 && existing.MTU <= path.MTU)) {
			continue
		}
		byKey[key] = path
	}
	var out []pathMTUForwardedPath
	for _, path := range byKey {
		out = append(out, path)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromInterface == out[j].FromInterface {
			return out[i].ToInterface < out[j].ToInterface
		}
		return out[i].FromInterface < out[j].FromInterface
	})
	return out
}

func pathMTUResourceInterfaces(router *api.Router) []pathMTUTunnel {
	if router == nil {
		return nil
	}
	var out []pathMTUTunnel
	for _, res := range router.Spec.Resources {
		item, ok := pathMTUResourceInterface(res)
		if ok {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func pathMTUResourceInterface(res api.Resource) (pathMTUTunnel, bool) {
	if strings.TrimSpace(res.Metadata.Name) == "" {
		return pathMTUTunnel{}, false
	}
	value := reflect.ValueOf(res.Spec)
	if !value.IsValid() {
		return pathMTUTunnel{}, false
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return pathMTUTunnel{}, false
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct || !pathMTUResourceEnabled(value) || !pathMTUResourceLooksLikeInterface(res, value) {
		return pathMTUTunnel{}, false
	}
	mtu := pathMTUResourceMTU(res, value)
	if mtu == 0 {
		return pathMTUTunnel{}, false
	}
	return pathMTUTunnel{Name: res.Metadata.Name, Underlay: pathMTUResourceUnderlay(value), MTU: mtu}, true
}

func pathMTUResourceEnabled(value reflect.Value) bool {
	field := value.FieldByName("Enabled")
	if !field.IsValid() {
		return true
	}
	switch field.Kind() {
	case reflect.Bool:
		return field.Bool()
	case reflect.Pointer:
		if field.IsNil() {
			return true
		}
		if field.Elem().Kind() == reflect.Bool {
			return field.Elem().Bool()
		}
	}
	return true
}

func pathMTUResourceLooksLikeInterface(res api.Resource, value reflect.Value) bool {
	if value.FieldByName("IfName").IsValid() || value.FieldByName("TunnelName").IsValid() || value.FieldByName("UnderlayInterface").IsValid() {
		return true
	}
	return strings.HasSuffix(res.Kind, "Interface")
}

func pathMTUResourceMTU(res api.Resource, value reflect.Value) int {
	if field := value.FieldByName("MTU"); field.IsValid() && field.Kind() == reflect.Int && field.Int() > 0 {
		return int(field.Int())
	}
	// Keep zero-value compatibility for resources whose existing renderers
	// already imply a non-1500 tunnel MTU. New tunnel-like resources participate
	// automatically when they expose an explicit spec.mtu.
	switch res.Kind {
	case "Interface":
		return 1500
	case "PPPoESession", "DSLiteTunnel":
		return 1454
	case "WireGuardInterface":
		return 1420
	case "TunnelInterface":
		mode := ""
		if field := value.FieldByName("Mode"); field.IsValid() && field.Kind() == reflect.String {
			mode = strings.TrimSpace(field.String())
		}
		if mode == "ipip" {
			return 1480
		}
		if mode == "gre" {
			return 1476
		}
		return 0
	default:
		return 0
	}
}

func pathMTUResourceUnderlay(value reflect.Value) string {
	for _, name := range []string{"UnderlayInterface", "Interface"} {
		field := value.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return strings.TrimSpace(field.String())
		}
	}
	return ""
}

func compactPathMTUPolicySpecs(specs []pathMTUPolicySpec) []pathMTUPolicySpec {
	seen := map[string]bool{}
	var out []pathMTUPolicySpec
	for _, spec := range specs {
		spec.ToInterfaces = compactStrings(sortedStrings(spec.ToInterfaces))
		key := spec.FromInterface + "|" + strings.Join(spec.ToInterfaces, ",") + "|" + strconv.Itoa(spec.MTU) + "|" + strings.Join(spec.TCPMSSClamp.Families, ",") + "|" + strconv.FormatBool(spec.IPv6RA.Enabled) + "|" + spec.IPv6RA.Scope
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, spec)
	}
	return out
}

func pathMTUTunnels(router *api.Router) []pathMTUTunnel {
	var tunnels []pathMTUTunnel
	for _, iface := range pathMTUResourceInterfaces(router) {
		if iface.Underlay != "" || iface.MTU < 1500 {
			tunnels = append(tunnels, iface)
		}
	}
	sort.Slice(tunnels, func(i, j int) bool { return tunnels[i].Name < tunnels[j].Name })
	return tunnels
}

func pathMTUSourceInterfaces(router *api.Router) []string {
	var sources []string
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallZone" {
			continue
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil || spec.Role != "trust" {
			continue
		}
		for _, ref := range spec.Interfaces {
			_, name := splitResourceRef(ref)
			sources = append(sources, name)
		}
	}
	return compactStrings(sortedStrings(sources))
}

func pathMTUUntrustInterfaces(router *api.Router) map[string]bool {
	out := map[string]bool{}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallZone" {
			continue
		}
		spec, err := res.FirewallZoneSpec()
		if err != nil || spec.Role != "untrust" {
			continue
		}
		for _, ref := range spec.Interfaces {
			_, name := splitResourceRef(ref)
			out[name] = true
		}
	}
	return out
}

func pathMTURAScopesByInterface(router *api.Router) map[string]string {
	out := map[string]string{}
	delegatedInterface := map[string]string{}
	for _, res := range router.Spec.Resources {
		if res.Kind != "IPv6DelegatedAddress" {
			continue
		}
		spec, err := res.IPv6DelegatedAddressSpec()
		if err == nil {
			delegatedInterface[res.Metadata.Name] = spec.Interface
		}
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "DHCPv6Server":
			spec, err := res.DHCPv6ServerSpec()
			if err != nil {
				continue
			}
			if iface := delegatedInterface[spec.DelegatedAddress]; iface != "" && out[iface] == "" {
				out[iface] = res.Metadata.Name
			}
		case "IPv6RouterAdvertisement":
			spec, err := res.IPv6RouterAdvertisementSpec()
			if err != nil {
				continue
			}
			if out[spec.Interface] == "" {
				out[spec.Interface] = res.Metadata.Name
			}
		}
	}
	return out
}

func pathMTURAByScope(router *api.Router) (map[string]int, error) {
	policies, err := pathMTUPolicies(router)
	if err != nil {
		return nil, err
	}
	result := map[string]int{}
	for _, policy := range policies {
		if !policy.Spec.IPv6RA.Enabled {
			continue
		}
		scope := policy.Spec.IPv6RA.Scope
		if scope == "" {
			continue
		}
		if existing := result[scope]; existing == 0 || policy.MTU < existing {
			result[scope] = policy.MTU
		}
	}
	return result, nil
}

func PathMTURAByScope(router *api.Router) (map[string]int, error) {
	return pathMTURAByScope(router)
}

func RouterWantsTCPMSSClamp(router *api.Router) (bool, error) {
	policies, err := pathMTUMSSPolicies(router)
	if err != nil {
		return false, err
	}
	return len(policies) > 0, nil
}

func pathMTUMSSPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	policies, err := pathMTUPolicies(router)
	if err != nil {
		return nil, err
	}
	var result []pathMTUPolicy
	for _, policy := range policies {
		if policy.Spec.TCPMSSClamp.Enabled {
			result = append(result, policy)
		}
	}
	return result, nil
}

func pathMTUFamilyEnabled(families []string, family string) bool {
	if len(families) == 0 {
		return true
	}
	for _, candidate := range families {
		if candidate == family {
			return true
		}
	}
	return false
}

func specResourceID(spec pathMTUPolicySpec) string {
	return "routerd.net/v1alpha1/Router/derived-path-mtu-" + spec.FromInterface
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}

func defaultInt(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func firstNonZero(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}
