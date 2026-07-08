// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/mobilityfib"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/resourcequery"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dynamicConfigPartLister interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
}

type dynamicRouteSAMView struct {
	EffectiveRouter *api.Router
	RouteRouter     *api.Router
	HybridLowerings []hybrid.HybridLowering
	SAMLowerings    []sam.DeliveryLowering
}

// BuildDynamicRouteSAMEffectiveRouter returns the route-facing effective router
// used by route and SAM controllers after dynamic config and SAM lowerings.
func BuildDynamicRouteSAMEffectiveRouter(startup *api.Router, store any, now time.Time, targetOS platform.OS) (*api.Router, error) {
	view, err := buildDynamicRouteSAMView(startup, store, now, targetOS)
	if err != nil {
		return nil, err
	}
	return view.RouteRouter, nil
}

// BuildDynamicRouteSAMObjectStatusRouters returns the effective resource views
// whose objects can legitimately own status rows. EffectiveRouter keeps
// dynamic config resources visible to their controllers; RouteRouter includes
// route-facing lowerings such as SAM/Hybrid IPv4Route resources.
func BuildDynamicRouteSAMObjectStatusRouters(startup *api.Router, store any, now time.Time, targetOS platform.OS) ([]*api.Router, error) {
	view, err := buildDynamicRouteSAMView(startup, store, now, targetOS)
	if err != nil {
		return nil, err
	}
	return []*api.Router{view.EffectiveRouter, view.RouteRouter}, nil
}

func buildDynamicRouteSAMView(startup *api.Router, store any, now time.Time, targetOS platform.OS) (dynamicRouteSAMView, error) {
	if startup == nil {
		return dynamicRouteSAMView{}, fmt.Errorf("startup router is required")
	}
	effective := *startup
	if lister, ok := store.(dynamicConfigPartLister); ok {
		records, err := lister.ListDynamicConfigParts()
		if err != nil {
			return dynamicRouteSAMView{}, fmt.Errorf("list dynamic config parts: %w", err)
		}
		if len(records) > 0 {
			parts, err := dynamicConfigPartsFromRecords(records)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*startup)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			merged, _, err := dynamicconfig.BuildEffectiveConfig(*startup, parts, policies, now.UTC())
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			effective = merged
		}
	}
	resolved, err := resolveWireGuardSAMResources(&effective)
	if err != nil {
		return dynamicRouteSAMView{}, err
	}
	effective = *resolved

	effective = appendBGPMobilityProviderSecondaryClaims(effective, store)
	effective = appendBGPMobilityProxyARPClaims(effective, store)
	effective = appendBGPMobilityLocalInventoryRoutes(effective, effective, store)

	routeRouter := effective
	routeRouter = appendBGPMobilityCapturePrefixRoutes(effective, routeRouter, store)
	hybridLowerings := []hybrid.HybridLowering(nil)
	if hybrid.HasHybridRoutes(&effective) {
		expanded, lowerings, err := hybrid.ExpandHybridRoutes(routeRouter)
		if err != nil {
			return dynamicRouteSAMView{}, err
		}
		routeRouter = expanded
		hybridLowerings = lowerings
	}

	routeRouter = suppressBGPModeMobilityClaims(effective, routeRouter)
	samLowerings := []sam.DeliveryLowering(nil)
	if targetOS == platform.OSLinux && sam.HasRemoteAddressClaims(&effective) {
		expanded, lowerings, err := sam.ExpandRemoteAddressClaimRoutesWithOptions(routeRouter, sam.PlanOptions{StatusReader: statusReaderFromStore(store)})
		if err != nil {
			return dynamicRouteSAMView{}, err
		}
		routeRouter = expanded
		samLowerings = lowerings
	}

	return dynamicRouteSAMView{
		EffectiveRouter: &effective,
		RouteRouter:     &routeRouter,
		HybridLowerings: hybridLowerings,
		SAMLowerings:    samLowerings,
	}, nil
}

func suppressBGPModeMobilityClaims(effective, routeRouter api.Router) api.Router {
	pools := bgpMobilityPools(effective)
	if len(pools) == 0 {
		return routeRouter
	}
	out := routeRouter
	out.Spec.Resources = make([]api.Resource, 0, len(routeRouter.Spec.Resources))
	for _, resource := range routeRouter.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "RemoteAddressClaim" {
			if pool := strings.TrimSpace(resource.Metadata.Annotations["mobility.routerd.net/pool"]); pool != "" && pools[pool] {
				continue
			}
		}
		out.Spec.Resources = append(out.Spec.Resources, resource)
	}
	return out
}

func appendBGPMobilityProxyARPClaims(router api.Router, store any) api.Router {
	reader := statusReaderFromStore(store)
	if reader == nil {
		return router
	}
	installed := bgpInstalledNextHopsFromRouterStatus(router, reader)
	if len(installed) == 0 {
		return router
	}
	selfByGroup := eventGroupSelfNodes(router)
	var claims []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		selfNode := strings.TrimSpace(selfByGroup[strings.TrimSpace(spec.GroupRef)])
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		self, ok := mobilityPoolMemberByNode(spec.Members, selfNode)
		if !ok || strings.TrimSpace(self.Capture.Type) != "proxy-arp" || strings.TrimSpace(self.Capture.Interface) == "" {
			continue
		}
		owned := mobilityStaticOwnedAddresses(self, prefix)
		for _, address := range bgpMobilityInstalledAddresses(installed, prefix) {
			if owned[address] {
				continue
			}
			if sam.CaptureExcludesAddress(addressCaptureFromMobilityCapture(self.Capture), address) {
				continue
			}
			claims = append(claims, bgpMobilityProxyARPClaim(resource.Metadata.Name, self, address))
		}
	}
	if len(claims) == 0 {
		return router
	}
	out := router
	out.Spec.Resources = append(append([]api.Resource(nil), router.Spec.Resources...), claims...)
	return out
}

func appendBGPMobilityProviderSecondaryClaims(router api.Router, store any) api.Router {
	reader := statusReaderFromStore(store)
	if reader == nil {
		return router
	}
	selfByGroup := eventGroupSelfNodes(router)
	var claims []api.Resource
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		selfNode := strings.TrimSpace(selfByGroup[strings.TrimSpace(spec.GroupRef)])
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		self, ok := mobilityPoolMemberByNode(spec.Members, selfNode)
		if !ok || strings.TrimSpace(self.Capture.Type) != "provider-secondary-ip" {
			continue
		}
		status := reader.ObjectStatus(api.MobilityAPIVersion, "MobilityPool", resource.Metadata.Name)
		for _, address := range bgpStatusStringSlice(status["discoverySelfCapturedAddresses"]) {
			normalized, ok := normalizeBGPMobilityHostPrefix(address, prefix)
			if !ok || sam.CaptureExcludesAddress(addressCaptureFromMobilityCapture(self.Capture), normalized) {
				continue
			}
			claims = append(claims, bgpMobilityProviderSecondaryClaim(resource.Metadata.Name, self, normalized))
		}
	}
	if len(claims) == 0 {
		return router
	}
	out := router
	out.Spec.Resources = append(append([]api.Resource(nil), router.Spec.Resources...), claims...)
	return out
}

func appendBGPMobilityCapturePrefixRoutes(effective, routeRouter api.Router, store any) api.Router {
	selfByGroup := eventGroupSelfNodes(effective)
	if len(selfByGroup) == 0 {
		return routeRouter
	}
	aliases := interfaceIfNames(effective)
	reader := statusReaderFromStore(store)
	var resources []api.Resource
	for _, resource := range effective.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		selfNode := strings.TrimSpace(selfByGroup[strings.TrimSpace(spec.GroupRef)])
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		self, ok := mobilityPoolMemberByNode(spec.Members, selfNode)
		if !ok || strings.TrimSpace(self.Capture.Type) != "proxy-arp" {
			continue
		}
		if !sam.EvaluateCaptureGate(addressCaptureFromMobilityCapture(self.Capture), reader).Active {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(spec.Prefix))
		if err != nil || !prefix.Addr().Is4() {
			continue
		}
		device := resolveInterfaceIfName(strings.TrimSpace(self.Capture.Interface), aliases)
		if device == "" {
			continue
		}
		captureInterface := strings.TrimSpace(self.Capture.Interface)
		preferredSource := strings.TrimSpace(self.Capture.SourceAddress)
		if cidr, source, ok := bgpMobilityCaptureSourceAddress(resource.Metadata.Name, captureInterface, preferredSource, prefix.Masked()); ok {
			resources = append(resources, cidr)
			preferredSource = source
		} else if source, ok := bgpMobilityCaptureSourceAddressFrom(reader, self.Capture.SourceAddressFrom, prefix.Masked()); ok {
			preferredSource = source
		} else {
			preferredSource = capturePrefixPreferredSource(effective, reader, captureInterface, device, prefix.Masked())
		}
		routePrefixes := sam.IPv4PrefixesExcluding(prefix.Masked(), self.Capture.ExcludeAddresses)
		splitRoutes := len(routePrefixes) != 1 || routePrefixes[0] != prefix.Masked()
		for _, routePrefix := range routePrefixes {
			resources = append(resources, bgpMobilityCapturePrefixRoute(resource.Metadata.Name, routePrefix.String(), device, preferredSource, splitRoutes))
		}
	}
	if len(resources) == 0 {
		return routeRouter
	}
	out := routeRouter
	out.Spec.Resources = append(append([]api.Resource(nil), routeRouter.Spec.Resources...), resources...)
	return out
}

func bgpMobilityCaptureSourceAddress(poolName, captureInterface, sourceAddress string, pool netip.Prefix) (api.Resource, string, bool) {
	cidr, source, ok := normalizeBGPMobilityCaptureSourceAddress(sourceAddress, pool)
	if !ok || strings.TrimSpace(captureInterface) == "" {
		return api.Resource{}, "", false
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4StaticAddress"},
		Metadata: api.ObjectMeta{
			Name: "sam-" + safeResourceName(poolName) + "-capture-source",
			Annotations: map[string]string{
				"mobility.routerd.net/pool":   poolName,
				"mobility.routerd.net/source": "bgp-capture-source",
			},
		},
		Spec: api.IPv4StaticAddressSpec{
			Interface: strings.TrimSpace(captureInterface),
			Address:   cidr,
		},
	}, source, true
}

func normalizeBGPMobilityCaptureSourceAddress(sourceAddress string, pool netip.Prefix) (string, string, bool) {
	sourceAddress = strings.TrimSpace(sourceAddress)
	if sourceAddress == "" || !pool.Addr().Is4() {
		return "", "", false
	}
	var addr netip.Addr
	if strings.Contains(sourceAddress, "/") {
		prefix, err := netip.ParsePrefix(sourceAddress)
		if err != nil || !prefix.Addr().Is4() {
			return "", "", false
		}
		addr = prefix.Addr()
	} else {
		parsed, err := netip.ParseAddr(sourceAddress)
		if err != nil || !parsed.Is4() {
			return "", "", false
		}
		addr = parsed
	}
	if !pool.Contains(addr) {
		return "", "", false
	}
	return netip.PrefixFrom(addr, 32).String(), addr.String(), true
}

func bgpMobilityCaptureSourceAddressFrom(reader sam.StatusReader, source api.StatusValueSourceSpec, pool netip.Prefix) (string, bool) {
	for _, value := range resourcequery.Values(reader, source) {
		_, addr, ok := normalizeBGPMobilityCaptureSourceAddress(value, pool)
		if ok {
			return addr, true
		}
	}
	return "", false
}

func bgpMobilityCapturePrefixRoute(poolName, prefix, device, preferredSource string, split bool) api.Resource {
	name := "sam-" + safeResourceName(poolName) + "-capture-prefix"
	if split {
		name = "sam-" + safeResourceName(poolName) + "-capture-" + safeResourceName(prefix)
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"mobility.routerd.net/pool":   poolName,
				"mobility.routerd.net/source": "bgp-capture-prefix",
			},
		},
		Spec: api.IPv4RouteSpec{
			Destination:     prefix,
			Device:          device,
			PreferredSource: preferredSource,
			Metric:          90,
		},
	}
}

func appendBGPMobilityLocalInventoryRoutes(effective, routeRouter api.Router, store any) api.Router {
	selfByGroup := eventGroupSelfNodes(effective)
	if len(selfByGroup) == 0 {
		return routeRouter
	}
	aliases := interfaceIfNames(effective)
	reader := statusReaderFromStore(store)
	if reader == nil {
		return routeRouter
	}
	snapshot := mobilityfib.NewSnapshot(&effective, reader)
	var resources []api.Resource
	for _, resource := range effective.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil || mobilityDeliveryMode(spec) != "bgp" {
			continue
		}
		selfNode := strings.TrimSpace(selfByGroup[strings.TrimSpace(spec.GroupRef)])
		if selfNode == "" {
			continue
		}
		spec, _, err = mobilityconfig.NormalizeMobilityPool(spec, selfNode)
		if err != nil {
			continue
		}
		self, ok := mobilityPoolMemberByNode(spec.Members, selfNode)
		if !ok || strings.TrimSpace(self.Capture.Type) != "provider-secondary-ip" {
			continue
		}
		device := resolveInterfaceIfName(strings.TrimSpace(self.Capture.Interface), aliases)
		if device == "" {
			continue
		}
		for _, verdict := range snapshot.LocalRouteVerdictsForPool(resource.Metadata.Name) {
			resources = append(resources, bgpMobilityLocalInventoryRoute(resource.Metadata.Name, verdict.Address, device, verdict))
		}
	}
	if len(resources) == 0 {
		return routeRouter
	}
	out := routeRouter
	out.Spec.Resources = append(append([]api.Resource(nil), routeRouter.Spec.Resources...), resources...)
	return out
}

func bgpMobilityLocalInventoryRoute(poolName, address, device string, verdict mobilityfib.Verdict) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
		Metadata: api.ObjectMeta{
			Name: "sam-" + safeResourceName(poolName) + "-local-" + safeResourceName(strings.TrimSuffix(address, "/32")),
			Annotations: map[string]string{
				"mobility.routerd.net/pool":       poolName,
				"mobility.routerd.net/source":     "bgp-local-inventory",
				"mobility.routerd.net/fibClass":   strings.TrimSpace(verdict.Class),
				"mobility.routerd.net/fibOwner":   strings.TrimSpace(verdict.OwnerNode),
				"mobility.routerd.net/fibReason":  strings.TrimSpace(verdict.Reason),
				"mobility.routerd.net/fibVerdict": strings.TrimSpace(verdict.Action),
			},
		},
		Spec: api.IPv4RouteSpec{
			Destination: address,
			Device:      device,
			Metric:      1,
		},
	}
}

func addressCaptureFromMobilityCapture(capture api.MobilityMemberCapture) api.AddressCapture {
	return api.AddressCapture{
		Type:               capture.Type,
		ProviderRef:        capture.ProviderRef,
		ProviderMode:       capture.ProviderMode,
		CaptureStrategy:    capture.CaptureStrategy,
		Strategy:           capture.Strategy,
		NICRef:             capture.NICRef,
		ConfigureOSAddress: capture.ConfigureOSAddress,
		Interface:          capture.Interface,
		ExcludeAddresses:   append([]string(nil), capture.ExcludeAddresses...),
		GratuitousARP:      capture.GratuitousARP,
		ActiveWhen:         capture.ActiveWhen,
	}
}

func interfaceIfNames(router api.Router) map[string]string {
	out := map[string]string{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion {
			continue
		}
		switch resource.Kind {
		case "Interface", "Bridge", "VXLANSegment", "WireGuardInterface", "PPPoESession", "DSLiteTunnel":
		default:
			continue
		}
		name := strings.TrimSpace(resource.Metadata.Name)
		ifname := strings.TrimSpace(interfaceIfName(&router, name))
		if name != "" && ifname != "" {
			out[name] = ifname
		}
	}
	return out
}

func resolveInterfaceIfName(value string, aliases map[string]string) string {
	value = strings.TrimSpace(value)
	if aliases == nil {
		return value
	}
	if ifname := strings.TrimSpace(aliases[value]); ifname != "" {
		return ifname
	}
	return value
}

func capturePrefixPreferredSource(router api.Router, reader sam.StatusReader, captureInterface, device string, pool netip.Prefix) string {
	if reader == nil || !pool.Addr().Is4() {
		return ""
	}
	for _, name := range captureInterfaceStatusNames(router, captureInterface, device) {
		status := reader.ObjectStatus(api.NetAPIVersion, "Interface", name)
		for _, raw := range append(statusStringSlice(status["ipv4Addresses"]), statusStringSlice(status["addresses"])...) {
			address := strings.TrimSpace(raw)
			if address == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(address)
			if err != nil {
				addr, err := netip.ParseAddr(address)
				if err != nil {
					continue
				}
				prefix = netip.PrefixFrom(addr, addr.BitLen())
			}
			prefix = prefix.Masked()
			if prefix.Addr().Is4() && pool.Contains(prefix.Addr()) {
				return prefix.Addr().String()
			}
		}
	}
	return ""
}

func captureInterfaceStatusNames(router api.Router, captureInterface, device string) []string {
	seen := map[string]bool{}
	var names []string
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	add(captureInterface)
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(resource.Metadata.Name) == captureInterface || strings.TrimSpace(spec.IfName) == device {
			add(resource.Metadata.Name)
		}
	}
	return names
}

func bgpInstalledNextHopsFromRouterStatus(router api.Router, reader sam.StatusReader) map[string][]string {
	out := map[string][]string{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		status := reader.ObjectStatus(api.NetAPIVersion, "BGPRouter", resource.Metadata.Name)
		for prefix, nextHops := range bgpInstalledNextHopsStatusValue(status["installedNextHops"]) {
			out[prefix] = bgpMergeStrings(out[prefix], nextHops)
		}
	}
	return out
}

func bgpInstalledNextHopsStatusValue(value any) map[string][]string {
	out := map[string][]string{}
	switch typed := value.(type) {
	case map[string][]string:
		for prefix, nextHops := range typed {
			out[strings.TrimSpace(prefix)] = bgpCleanStrings(nextHops)
		}
	case map[string]any:
		for prefix, raw := range typed {
			out[strings.TrimSpace(prefix)] = bgpStatusStringSlice(raw)
		}
	}
	return out
}

func eventGroupSelfNodes(router api.Router) map[string]string {
	out := map[string]string{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.FederationAPIVersion || resource.Kind != "EventGroup" {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err == nil {
			out[resource.Metadata.Name] = strings.TrimSpace(spec.NodeName)
		}
	}
	return out
}

func mobilityPoolMemberByNode(members []api.MobilityPoolMember, node string) (api.MobilityPoolMember, bool) {
	for _, member := range members {
		if strings.TrimSpace(member.NodeRef) == strings.TrimSpace(node) {
			return member, true
		}
	}
	return api.MobilityPoolMember{}, false
}

func mobilityStaticOwnedAddresses(member api.MobilityPoolMember, pool netip.Prefix) map[string]bool {
	out := map[string]bool{}
	for _, raw := range member.StaticOwnedAddresses {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		if prefix.Addr().Is4() && prefix.Bits() == 32 && pool.Contains(prefix.Addr()) {
			out[prefix.String()] = true
		}
	}
	return out
}

func normalizeBGPMobilityHostPrefix(value string, pool netip.Prefix) (string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		addr, addrErr := netip.ParseAddr(strings.TrimSpace(value))
		if addrErr != nil || !addr.Is4() {
			return "", false
		}
		prefix = netip.PrefixFrom(addr, 32)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 || !pool.Contains(prefix.Addr()) {
		return "", false
	}
	return prefix.String(), true
}

func bgpMobilityInstalledAddresses(installed map[string][]string, pool netip.Prefix) []string {
	seen := map[string]bool{}
	for raw, nextHops := range installed {
		if len(bgpCleanStrings(nextHops)) == 0 {
			continue
		}
		prefix, err := netip.ParsePrefix(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		if prefix.Addr().Is4() && prefix.Bits() == 32 && pool.Contains(prefix.Addr()) {
			seen[prefix.String()] = true
		}
	}
	return bgpSortedStringKeys(seen)
}

func bgpMobilityProxyARPClaim(poolName string, member api.MobilityPoolMember, address string) api.Resource {
	name := "bgp-" + safeResourceName(poolName) + "-" + safeResourceName(strings.TrimSuffix(address, "/32"))
	annotations := map[string]string{
		"mobility.routerd.net/pool":   poolName,
		"mobility.routerd.net/source": "bgp-best-path",
	}
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{
			Name:        name,
			Annotations: annotations,
		},
		Spec: api.RemoteAddressClaimSpec{
			DomainRef: "bgp-" + poolName,
			Address:   address,
			OwnerSide: "remote",
			Capture: api.AddressCapture{
				Type:             member.Capture.Type,
				CaptureStrategy:  member.Capture.CaptureStrategy,
				Strategy:         member.Capture.Strategy,
				Interface:        member.Capture.Interface,
				ExcludeAddresses: append([]string(nil), member.Capture.ExcludeAddresses...),
				ActiveWhen:       member.Capture.ActiveWhen,
			},
			Delivery: api.AddressDelivery{Mode: "bgp"},
		},
	}
}

func bgpMobilityProviderSecondaryClaim(poolName string, member api.MobilityPoolMember, address string) api.Resource {
	name := "bgp-provider-" + safeResourceName(poolName) + "-" + safeResourceName(strings.TrimSuffix(address, "/32"))
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "RemoteAddressClaim"},
		Metadata: api.ObjectMeta{
			Name: name,
			Annotations: map[string]string{
				"mobility.routerd.net/pool":   poolName,
				"mobility.routerd.net/source": "bgp-provider-capture",
			},
		},
		Spec: api.RemoteAddressClaimSpec{
			DomainRef: "bgp-" + poolName,
			Address:   address,
			OwnerSide: "remote",
			Capture:   addressCaptureFromMobilityCapture(member.Capture),
			Delivery:  api.AddressDelivery{Mode: "bgp"},
		},
	}
}

func bgpStatusStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return bgpCleanStrings(typed)
	case []any:
		var out []string
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" && value != "<nil>" {
				out = append(out, value)
			}
		}
		return bgpCleanStrings(out)
	default:
		if value := strings.TrimSpace(fmt.Sprint(value)); value != "" && value != "<nil>" {
			return []string{value}
		}
	}
	return nil
}

func bgpMergeStrings(base, extra []string) []string {
	seen := map[string]bool{}
	for _, value := range append(base, extra...) {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	return bgpSortedStringKeys(seen)
}

func bgpCleanStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	return bgpSortedStringKeys(seen)
}

func bgpSortedStringKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func safeResourceName(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "resource"
	}
	return out
}

func bgpMobilityPools(router api.Router) map[string]bool {
	out := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "MobilityPool" {
			continue
		}
		spec, err := resource.MobilityPoolSpec()
		if err != nil {
			continue
		}
		if mobilityDeliveryMode(spec) == "bgp" {
			out[resource.Metadata.Name] = true
		}
	}
	return out
}

func mobilityDeliveryMode(spec api.MobilityPoolSpec) string {
	mode := strings.TrimSpace(spec.DeliveryPolicy.Mode)
	if mode == "" {
		return "bgp"
	}
	return mode
}

func statusReaderFromStore(store any) sam.StatusReader {
	reader, _ := store.(sam.StatusReader)
	return reader
}

func dynamicConfigPartsFromRecords(records []routerstate.DynamicConfigPartRecord) ([]dynamicconfig.DynamicConfigPart, error) {
	parts := make([]dynamicconfig.DynamicConfigPart, 0, len(records))
	for _, record := range records {
		resources, err := decodeDynamicConfigResources(record.ResourcesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d resources: %w", record.Source, record.Generation, err)
		}
		directives, err := decodeDynamicConfigDirectives(record.DirectivesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d directives: %w", record.Source, record.Generation, err)
		}
		parts = append(parts, dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Metadata: api.ObjectMeta{
				Name: fmt.Sprintf("%s-%d", record.Source, record.Generation),
			},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     record.Source,
				Generation: record.Generation,
				ObservedAt: record.ObservedAt,
				ExpiresAt:  record.ExpiresAt,
				Digest:     record.Digest,
				Resources:  resources,
				Directives: directives,
			},
		})
	}
	return parts, nil
}

func decodeDynamicConfigResources(raw string) ([]api.Resource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func decodeDynamicConfigDirectives(raw string) ([]dynamicconfig.DynamicConfigDirective, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var directives []dynamicconfig.DynamicConfigDirective
	if err := json.Unmarshal([]byte(raw), &directives); err != nil {
		return nil, err
	}
	return directives, nil
}
