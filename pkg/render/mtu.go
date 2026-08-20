// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/hybrid"
)

type pathMTUPolicy struct {
	ResourceID string
	Spec       pathMTUPolicySpec
	MTU        int
}

type pathMTUPolicySpec struct {
	FromInterface     string
	ToInterfaces      []string
	MTU               int
	IPv6RA            pathMTUPolicyIPv6RASpec
	TCPMSSClamp       pathMTUPolicyTCPMSSSpec
	ForceFragmentIPv4 bool
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
	FromInterface     string
	ToInterface       string
	MTU               int
	ForceFragmentIPv4 bool
}

type pathMTUForwardingTunnel struct {
	Name              string
	MTU               int
	ForceFragmentIPv4 bool
}

func pathMTUPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	return pathMTUPoliciesWithLocalCaptureIntents(router, nil)
}

// pathMTUPoliciesWithLocalCaptureIntents adds forwarded-path policies from the
// already-decided mobility dataplane plan. It deliberately accepts no raw
// planner resource: placement, capture interface selection, and forwarding
// targets cross this boundary as LocalCaptureIntents.
func pathMTUPoliciesWithLocalCaptureIntents(router *api.Router, intents []dynamicconfig.LocalCaptureIntent) ([]pathMTUPolicy, error) {
	mtus, err := resourceMTUsWithLocalCaptureIntents(router, intents)
	if err != nil {
		return nil, err
	}
	var policies []pathMTUPolicy
	for _, spec := range derivedPathMTUPolicySpecsWithLocalCaptureIntents(router, mtus, intents) {
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
	return resourceMTUsWithLocalCaptureIntents(router, nil)
}

func resourceMTUsWithLocalCaptureIntents(router *api.Router, intents []dynamicconfig.LocalCaptureIntent) (map[string]int, error) {
	mtus := map[string]int{}
	for _, iface := range pathMTUResourceInterfaces(router) {
		mtus[iface.Name] = iface.MTU
	}
	for _, iface := range pathMTUForwardedPathInterfacesWithLocalCaptureIntents(router, intents) {
		if mtus[iface] == 0 {
			mtus[iface] = 1500
		}
	}
	return mtus, nil
}

func derivedPathMTUPolicySpecs(router *api.Router, mtus map[string]int) []pathMTUPolicySpec {
	return derivedPathMTUPolicySpecsWithLocalCaptureIntents(router, mtus, nil)
}

func derivedPathMTUPolicySpecsWithLocalCaptureIntents(router *api.Router, mtus map[string]int, intents []dynamicconfig.LocalCaptureIntent) []pathMTUPolicySpec {
	tunnels := pathMTUTunnels(router)
	forwardedPathPolicies := derivedForwardedPathMTUPolicySpecsWithLocalCaptureIntents(router, mtus, intents)
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
	return derivedForwardedPathMTUPolicySpecsWithLocalCaptureIntents(router, mtus, nil)
}

func derivedForwardedPathMTUPolicySpecsWithLocalCaptureIntents(router *api.Router, mtus map[string]int, intents []dynamicconfig.LocalCaptureIntent) []pathMTUPolicySpec {
	var policies []pathMTUPolicySpec
	for _, path := range pathMTUForwardedPathsWithLocalCaptureIntents(router, intents) {
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
		policies = append(policies, pathMTUPolicySpec{
			FromInterface:     path.FromInterface,
			ToInterfaces:      []string{path.ToInterface},
			MTU:               toMTU,
			TCPMSSClamp:       clamp,
			ForceFragmentIPv4: path.ForceFragmentIPv4,
		})
	}
	return compactPathMTUPolicySpecs(policies)
}

func pathMTUForwardedPaths(router *api.Router) []pathMTUForwardedPath {
	return pathMTUForwardedPathsWithLocalCaptureIntents(router, nil)
}

func pathMTUForwardedPathsWithLocalCaptureIntents(router *api.Router, intents []dynamicconfig.LocalCaptureIntent) []pathMTUForwardedPath {
	if router == nil {
		return nil
	}
	peers := pathMTUOverlayPeers(router)
	paths := pathMTULocalCaptureForwardedPaths(router, intents, peers)
	return compactForwardedPaths(paths)
}

func pathMTULocalCaptureForwardedPaths(router *api.Router, intents []dynamicconfig.LocalCaptureIntent, peers map[string]api.OverlayPeerSpec) []pathMTUForwardedPath {
	if router == nil {
		return nil
	}
	tunnels := pathMTUForwardingTunnels(router)
	var paths []pathMTUForwardedPath
	for _, intent := range intents {
		// Release is an explicit teardown operation. It must clear an earlier
		// clamp rather than keep forwarding policy alive. A held capture stays
		// here because SAM deliberately retains its previously-applied effect.
		if intent.Disposition == dynamicconfig.CaptureRelease || intent.Disposition == dynamicconfig.CaptureProhibited {
			continue
		}
		source := strings.TrimSpace(intent.CaptureInterface)
		if source == "" {
			continue
		}
		for peerName, peer := range peers {
			tunnel := strings.TrimSpace(peer.Underlay.Interface)
			if tunnel == "" || tunnel == source {
				continue
			}
			paths = append(paths, pathMTUForwardedPath{
				FromInterface:     source,
				ToInterface:       tunnel,
				MTU:               pathMTUOverlayPeerEffectiveMTU(router, peerName),
				ForceFragmentIPv4: pathMTUOverlayPeerForceFragmentIPv4(router, peerName, peer),
			})
		}
		var forwardingTunnels []pathMTUForwardingTunnel
		for _, name := range compactStrings(sortedStrings(intent.TunnelInterfaces)) {
			tunnel, ok := tunnels[name]
			if !ok || tunnel.Name == "" || tunnel.Name == source {
				continue
			}
			forwardingTunnels = append(forwardingTunnels, tunnel)
			paths = append(paths, pathMTUForwardedPath{
				FromInterface:     source,
				ToInterface:       tunnel.Name,
				MTU:               tunnel.MTU,
				ForceFragmentIPv4: tunnel.ForceFragmentIPv4,
			})
		}
		for _, from := range forwardingTunnels {
			for _, to := range forwardingTunnels {
				if from.Name == to.Name {
					continue
				}
				mtu := to.MTU
				if from.MTU > 0 && (mtu == 0 || from.MTU < mtu) {
					mtu = from.MTU
				}
				paths = append(paths, pathMTUForwardedPath{
					FromInterface:     from.Name,
					ToInterface:       to.Name,
					MTU:               mtu,
					ForceFragmentIPv4: from.ForceFragmentIPv4 || to.ForceFragmentIPv4,
				})
			}
		}
	}
	return compactForwardedPaths(paths)
}

func pathMTUForwardingTunnels(router *api.Router) map[string]pathMTUForwardingTunnel {
	out := map[string]pathMTUForwardingTunnel{}
	if router == nil {
		return out
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion || res.Kind != "TunnelInterface" || strings.TrimSpace(res.Metadata.Name) == "" {
			continue
		}
		spec, err := res.TunnelInterfaceSpec()
		if err != nil {
			continue
		}
		mtu := pathMTUForwardingTunnelMTU(router, spec)
		if mtu == 0 {
			continue
		}
		out[res.Metadata.Name] = pathMTUForwardingTunnel{
			Name:              strings.TrimSpace(res.Metadata.Name),
			MTU:               mtu,
			ForceFragmentIPv4: spec.PathMTU.ForceFragmentIPv4,
		}
	}
	return out
}

func pathMTUForwardingTunnelMTU(router *api.Router, spec api.TunnelInterfaceSpec) int {
	if router == nil {
		return 0
	}
	mtu := hybrid.TunnelInterfaceEffectiveMTU(*router, spec)
	if mtu == 0 {
		return 0
	}
	mtu -= pathMTUUnderlayOverlayOverhead(router, spec.UnderlayInterface)
	if mtu <= 0 {
		return 0
	}
	return mtu
}

func pathMTUUnderlayOverlayOverhead(router *api.Router, interfaceName string) int {
	return pathMTUUnderlayOverlayOverheadSeen(router, strings.TrimSpace(interfaceName), map[string]bool{})
}

func pathMTUUnderlayOverlayOverheadSeen(router *api.Router, interfaceName string, seen map[string]bool) int {
	if router == nil || interfaceName == "" || seen[interfaceName] {
		return 0
	}
	seen[interfaceName] = true
	for _, res := range router.Spec.Resources {
		if strings.TrimSpace(res.Metadata.Name) != interfaceName {
			continue
		}
		switch res.Kind {
		case "WireGuardInterface":
			return hybrid.WireGuardOverheadBytes
		case "TunnelInterface":
			spec, err := res.TunnelInterfaceSpec()
			if err != nil {
				return 0
			}
			return hybrid.TunnelInterfaceOverhead(spec) + pathMTUUnderlayOverlayOverheadSeen(router, spec.UnderlayInterface, seen)
		default:
			return 0
		}
	}
	return 0
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

func pathMTUOverlayPeerForceFragmentIPv4(router *api.Router, peerName string, peer api.OverlayPeerSpec) bool {
	if peer.PathMTU.ForceFragmentIPv4 {
		return true
	}
	if strings.TrimSpace(peerName) == "" {
		return false
	}
	return pathMTUTunnelForceFragmentIPv4(router, peer.Underlay.Interface)
}

func pathMTUTunnelForceFragmentIPv4(router *api.Router, interfaceName string) bool {
	if router == nil || strings.TrimSpace(interfaceName) == "" {
		return false
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion || res.Kind != "TunnelInterface" || res.Metadata.Name != strings.TrimSpace(interfaceName) {
			continue
		}
		spec, err := res.TunnelInterfaceSpec()
		return err == nil && spec.PathMTU.ForceFragmentIPv4
	}
	return false
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

func pathMTUForwardedPathInterfaces(router *api.Router) []string {
	return pathMTUForwardedPathInterfacesWithLocalCaptureIntents(router, nil)
}

func pathMTUForwardedPathInterfacesWithLocalCaptureIntents(router *api.Router, intents []dynamicconfig.LocalCaptureIntent) []string {
	if router == nil {
		return nil
	}
	var out []string
	for _, path := range pathMTUForwardedPathsWithLocalCaptureIntents(router, intents) {
		out = append(out, path.FromInterface, path.ToInterface)
	}
	return compactStrings(sortedStrings(out))
}

func compactForwardedPaths(paths []pathMTUForwardedPath) []pathMTUForwardedPath {
	byKey := map[string]pathMTUForwardedPath{}
	for _, path := range paths {
		key := path.FromInterface + ">" + path.ToInterface + ">" + strconv.FormatBool(path.ForceFragmentIPv4)
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
		item, ok := pathMTUResourceInterface(router, res)
		if ok {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func pathMTUResourceInterface(router *api.Router, res api.Resource) (pathMTUTunnel, bool) {
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
	mtu := pathMTUResourceMTU(router, res, value)
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

func pathMTUResourceMTU(router *api.Router, res api.Resource, value reflect.Value) int {
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
		spec, err := res.TunnelInterfaceSpec()
		if err != nil || router == nil {
			return 0
		}
		return hybrid.TunnelInterfaceEffectiveMTU(*router, spec)
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
		key := spec.FromInterface + "|" + strings.Join(spec.ToInterfaces, ",") + "|" + strconv.Itoa(spec.MTU) + "|" + strings.Join(spec.TCPMSSClamp.Families, ",") + "|" + strconv.FormatBool(spec.ForceFragmentIPv4) + "|" + strconv.FormatBool(spec.IPv6RA.Enabled) + "|" + spec.IPv6RA.Scope
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
	if len(policies) > 0 {
		return true, nil
	}
	// LocalCaptureIntents are persisted independently from Router resources.
	// A declared transport can therefore cause the path-MTU controller to
	// create routerd_mss after this static artifact ownership check. Retain
	// ownership based on transport capability without reopening MobilityPool or
	// status as a desired-state channel.
	capabilities, err := pathMTUDynamicTransportCapabilities(router)
	if err != nil {
		return false, err
	}
	return capabilities.LocalCapture, nil
}

func pathMTUMSSPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	return pathMTUMSSPoliciesWithLocalCaptureIntents(router, nil)
}

func pathMTUMSSPoliciesWithLocalCaptureIntents(router *api.Router, intents []dynamicconfig.LocalCaptureIntent) ([]pathMTUPolicy, error) {
	policies, err := pathMTUPoliciesWithLocalCaptureIntents(router, intents)
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

func RouterWantsIPv4ForceFragment(router *api.Router) (bool, error) {
	policies, err := pathMTUForceFragmentPolicies(router)
	if err != nil {
		return false, err
	}
	if len(policies) > 0 {
		return true, nil
	}
	// LocalCaptureIntents cross the dynamic-config persistence boundary rather
	// than being reconstructed from MobilityPool or status here. The path-MTU
	// controller can therefore create routerd_forcefrag after this static
	// ownership check has run. Keep the table router-owned whenever a declared
	// transport can request IPv4 force-fragment handling, so the artifact
	// lifecycle does not mistake that controller-owned table for an orphan.
	capabilities, err := pathMTUDynamicTransportCapabilities(router)
	if err != nil {
		return false, err
	}
	return capabilities.ForceFragmentIPv4, nil
}

type pathMTUDynamicTransportState struct {
	LocalCapture      bool
	ForceFragmentIPv4 bool
}

func pathMTUDynamicTransportCapabilities(router *api.Router) (pathMTUDynamicTransportState, error) {
	var capabilities pathMTUDynamicTransportState
	if router == nil {
		return capabilities, nil
	}
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.HybridAPIVersion {
			continue
		}
		switch res.Kind {
		case "OverlayPeer":
			spec, err := res.OverlayPeerSpec()
			if err != nil {
				return capabilities, err
			}
			capabilities.LocalCapture = capabilities.LocalCapture || strings.TrimSpace(spec.Underlay.Interface) != ""
			if spec.PathMTU.ForceFragmentIPv4 {
				capabilities.ForceFragmentIPv4 = true
			}
		case "TunnelInterface":
			spec, err := res.TunnelInterfaceSpec()
			if err != nil {
				return capabilities, err
			}
			capabilities.LocalCapture = capabilities.LocalCapture || strings.TrimSpace(res.Metadata.Name) != ""
			if spec.PathMTU.ForceFragmentIPv4 {
				capabilities.ForceFragmentIPv4 = true
			}
		}
	}
	return capabilities, nil
}

func pathMTUForceFragmentPolicies(router *api.Router) ([]pathMTUPolicy, error) {
	return pathMTUForceFragmentPoliciesWithLocalCaptureIntents(router, nil)
}

func pathMTUForceFragmentPoliciesWithLocalCaptureIntents(router *api.Router, intents []dynamicconfig.LocalCaptureIntent) ([]pathMTUPolicy, error) {
	policies, err := pathMTUPoliciesWithLocalCaptureIntents(router, intents)
	if err != nil {
		return nil, err
	}
	var result []pathMTUPolicy
	for _, policy := range policies {
		if policy.Spec.ForceFragmentIPv4 {
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
