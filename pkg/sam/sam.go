// SPDX-License-Identifier: BSD-3-Clause

package sam

import (
	"fmt"
	"hash/fnv"
	"math/bits"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
)

const DeliveryRouteMetricDefault = 120

const DeliveryPreferredSourceAnnotation = "mobility.routerd.net/delivery-preferred-source"

const (
	CaptureStatusCaptured = "Captured"
	CaptureStatusStandby  = "Standby"
	CaptureStatusBlocked  = "Blocked"
)

const (
	CloudClaimNotApplicable = "NotApplicable"
	CloudClaimPending       = "Pending"
	CloudClaimClaimed       = "Claimed"
	CloudClaimFailed        = "Failed"
	CloudClaimUnknown       = "Unknown"

	OSCaptureNotApplicable   = "NotApplicable"
	OSCaptureMissing         = "Missing"
	OSCaptureReflected       = "Reflected"
	OSCaptureForwardingReady = "ForwardingReady"
	OSCaptureUnexpected      = "Unexpected"
	OSCaptureUnknown         = "Unknown"

	ForwardingNotApplicable = "NotApplicable"
	ForwardingReady         = "Ready"
	ForwardingDisabled      = "Disabled"
	ForwardingUnknown       = "Unknown"

	FIBConvergenceReady        = "Ready"
	FIBConvergenceMissingRoute = "MissingRoute"
	FIBConvergenceUnknown      = "Unknown"

	AdvertisementGateAllowed = "Allowed"
	AdvertisementGateBlocked = "Blocked"

	SAMConvergenceReady    = "Ready"
	SAMConvergenceDegraded = "Degraded"
	SAMConvergenceFailed   = "Failed"
)

type SAMConvergenceStatus struct {
	CloudClaimPhase        string
	OSCapturePhase         string
	ForwardingPhase        string
	FIBConvergencePhase    string
	AdvertisementGatePhase string
	SAMConvergencePhase    string
	SplitBrainDetected     bool
	StaleEpochDetected     bool
	BlockingReasons        []string
	LastObservedAt         string
}

func (s SAMConvergenceStatus) StatusFields() map[string]any {
	return map[string]any{
		"cloudClaimPhase":        s.CloudClaimPhase,
		"osCapturePhase":         s.OSCapturePhase,
		"forwardingPhase":        s.ForwardingPhase,
		"fibConvergencePhase":    s.FIBConvergencePhase,
		"advertisementGatePhase": s.AdvertisementGatePhase,
		"samConvergencePhase":    s.SAMConvergencePhase,
		"splitBrainDetected":     s.SplitBrainDetected,
		"staleEpochDetected":     s.StaleEpochDetected,
		"blockingReasons":        append([]string(nil), s.BlockingReasons...),
		"lastObservedAt":         s.LastObservedAt,
	}
}

type DeliveryLowering struct {
	ClaimName       string
	AddressCIDR     string
	IPv4RouteName   string
	Device          string
	PreferredSource string
	Metric          int
	OwnerSide       string
	CaptureType     string
	DeliveryPeer    string
	DeliveryMode    string
	CaptureIface    string
	CaptureMessage  string
}

type CaptureAction struct {
	Kind          string
	ClaimName     string
	Address       string
	Destination   string
	Interface     string
	PeerInterface string
	Key           string
	Value         string
	Table         int
	Priority      int
	Metric        int

	GratuitousARP bool
}

type PlanOptions struct {
	StatusReader               StatusReader
	ProviderOwnershipConfirmed func(claimName string, capture api.AddressCapture, address string) bool
}

type CaptureGateStatus struct {
	Active             bool
	Type               string
	VirtualAddressRef  string
	VirtualAddressRole string
	Reason             string
	Message            string
}

func HasRemoteAddressClaims(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "RemoteAddressClaim" {
			return true
		}
	}
	return false
}

func ExpandRemoteAddressClaimRoutes(router api.Router) (api.Router, []DeliveryLowering, error) {
	return ExpandRemoteAddressClaimRoutesWithOptions(router, PlanOptions{})
}

func ExpandRemoteAddressClaimRoutesWithOptions(router api.Router, opts PlanOptions) (api.Router, []DeliveryLowering, error) {
	if !HasRemoteAddressClaims(&router) {
		return router, nil, nil
	}
	peers := overlayPeers(router)
	userRouteNames := map[string]bool{}
	userRouteDestinations := map[string]string{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPv4Route" {
			continue
		}
		userRouteNames[resource.Metadata.Name] = true
		spec, err := resource.IPv4RouteSpec()
		if err != nil {
			return router, nil, err
		}
		if destination := strings.TrimSpace(spec.Destination); destination != "" {
			userRouteDestinations[destination] = resource.Metadata.Name
		}
	}

	out := router
	out.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	syntheticNames := map[string]bool{}
	var lowerings []DeliveryLowering
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := resource.RemoteAddressClaimSpec()
		if err != nil {
			return router, nil, err
		}
		cidr, err := normalizeClaimAddress(spec.Address)
		if err != nil {
			return router, nil, fmt.Errorf("%s spec.address: %w", resource.ID(), err)
		}
		if gate := EvaluateCaptureGate(spec.Capture, opts.StatusReader); !gate.Active {
			continue
		}
		if strings.TrimSpace(spec.Delivery.Mode) == "bgp" {
			continue
		}
		if existing := userRouteDestinations[cidr]; existing != "" {
			return router, nil, fmt.Errorf("%s destination %s collides with user IPv4Route/%s", resource.ID(), cidr, existing)
		}
		name := DeliveryRouteName(resource.Metadata.Name)
		if userRouteNames[name] {
			return router, nil, fmt.Errorf("%s synthetic IPv4Route name %q collides with a user-authored IPv4Route", resource.ID(), name)
		}
		if syntheticNames[name] {
			return router, nil, fmt.Errorf("%s synthetic IPv4Route name %q is not unique", resource.ID(), name)
		}
		peerName := normalizeRefName(spec.Delivery.PeerRef, "OverlayPeer")
		device := strings.TrimSpace(spec.Delivery.TunnelInterface)
		if device == "" {
			peer, ok := peers[peerName]
			if !ok {
				return router, nil, fmt.Errorf("%s spec.delivery.peerRef references missing OverlayPeer %q", resource.ID(), spec.Delivery.PeerRef)
			}
			resolvedDevice, _, err := hybrid.RouteTarget(peer)
			if err != nil {
				return router, nil, fmt.Errorf("%s: %w", resource.ID(), err)
			}
			device = resolvedDevice
		}
		syntheticNames[name] = true
		preferredSource := defaultDeliveryPreferredSource(resource, spec, cidr)
		route := api.Resource{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
			Metadata: api.ObjectMeta{
				Name: name,
				OwnerRefs: []api.OwnerRef{{
					APIVersion: api.HybridAPIVersion,
					Kind:       "RemoteAddressClaim",
					Name:       resource.Metadata.Name,
				}},
			},
			Spec: api.IPv4RouteSpec{
				Destination:     cidr,
				Type:            "unicast",
				Device:          device,
				PreferredSource: preferredSource,
				Metric:          DeliveryRouteMetricDefault,
			},
		}
		out.Spec.Resources = append(out.Spec.Resources, route)
		lowerings = append(lowerings, DeliveryLowering{
			ClaimName:       resource.Metadata.Name,
			AddressCIDR:     cidr,
			IPv4RouteName:   name,
			Device:          device,
			PreferredSource: preferredSource,
			Metric:          DeliveryRouteMetricDefault,
			OwnerSide:       strings.TrimSpace(spec.OwnerSide),
			CaptureType:     strings.TrimSpace(spec.Capture.Type),
			DeliveryPeer:    peerName,
			DeliveryMode:    strings.TrimSpace(spec.Delivery.Mode),
			CaptureIface:    strings.TrimSpace(spec.Capture.Interface),
		})
	}
	return out, lowerings, nil
}

func defaultDeliveryPreferredSource(resource api.Resource, spec api.RemoteAddressClaimSpec, cidr string) string {
	if source := strings.TrimSpace(resource.Metadata.Annotations[DeliveryPreferredSourceAnnotation]); source != "" {
		return source
	}
	if strings.TrimSpace(spec.Capture.Type) != "provider-secondary-ip" || !spec.Capture.ConfigureOSAddress {
		return ""
	}
	prefix, err := netip.ParsePrefix(cidr)
	if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return ""
	}
	return prefix.Addr().String()
}

func PlanCapture(router *api.Router, targetOS platform.OS) ([]CaptureAction, error) {
	return PlanCaptureWithOptions(router, targetOS, PlanOptions{})
}

func PlanCaptureWithOptions(router *api.Router, targetOS platform.OS, opts PlanOptions) ([]CaptureAction, error) {
	if router == nil || !HasRemoteAddressClaims(router) || targetOS != platform.OSLinux {
		return nil, nil
	}
	interfaces := map[string]bool{}
	interfaceAliases := CaptureInterfaceAliases(router)
	peers := overlayPeers(*router)
	domains := addressMobilityDomainPrefixes(router)
	forwardingAdded := false
	var actions []CaptureAction
	addForwarding := func() {
		if forwardingAdded {
			return
		}
		actions = append(actions, CaptureAction{Kind: "sysctl", Key: "net.ipv4.ip_forward", Value: "1"})
		forwardingAdded = true
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := resource.RemoteAddressClaimSpec()
		if err != nil {
			return nil, err
		}
		address, err := normalizeClaimAddress(spec.Address)
		if err != nil {
			return nil, err
		}
		captureType := strings.TrimSpace(spec.Capture.Type)
		if captureType == "proxy-arp" && CaptureExcludesAddress(spec.Capture, address) {
			continue
		}
		if gate := EvaluateCaptureGate(spec.Capture, opts.StatusReader); !gate.Active {
			continue
		}
		addForwarding()
		if captureType != "proxy-arp" {
			if captureType == "provider-secondary-ip" {
				if spec.Capture.ConfigureOSAddress {
					iface := ResolveCaptureInterface(strings.TrimSpace(spec.Capture.Interface), interfaceAliases)
					if iface == "" {
						return nil, fmt.Errorf("%s spec.capture.interface is required when provider-secondary-ip configureOSAddress is true", resource.ID())
					}
					if opts.ProviderOwnershipConfirmed != nil && !opts.ProviderOwnershipConfirmed(resource.Metadata.Name, spec.Capture, address) {
						actions = append(actions, CaptureAction{Kind: "provider-ownership-blocked", ClaimName: resource.Metadata.Name, Address: address, Interface: iface})
						continue
					}
					actions = append(actions, CaptureAction{Kind: "assign-os-address", ClaimName: resource.Metadata.Name, Address: address, Interface: iface})
					if action, ok, err := returnPolicyRouteAction(resource, spec, address, peers, domains); err != nil {
						return nil, err
					} else if ok {
						actions = append(actions, action)
						actions = append(actions, CaptureAction{Kind: "forward-path", ClaimName: resource.Metadata.Name, Interface: iface, PeerInterface: action.Interface})
					} else if strings.TrimSpace(spec.Delivery.Mode) == "bgp" {
						for _, tunnelIface := range bgpDeliveryForwardInterfaces(router) {
							actions = append(actions, CaptureAction{Kind: "forward-path", ClaimName: resource.Metadata.Name, Interface: iface, PeerInterface: tunnelIface})
						}
					}
				} else {
					actions = append(actions, CaptureAction{Kind: "deassign-os-address", ClaimName: resource.Metadata.Name, Address: address})
				}
			}
			continue
		}
		iface := ResolveCaptureInterface(strings.TrimSpace(spec.Capture.Interface), interfaceAliases)
		if iface == "" {
			return nil, fmt.Errorf("%s spec.capture.interface is required for proxy-arp", resource.ID())
		}
		if !interfaces[iface] {
			interfaces[iface] = true
			actions = append(actions, CaptureAction{Kind: "sysctl", Key: "net.ipv4.conf." + iface + ".proxy_arp", Value: "1", Interface: iface})
		}
		actions = append(actions, CaptureAction{Kind: "proxy-neighbor", ClaimName: resource.Metadata.Name, Address: address, Interface: iface, GratuitousARP: wantsGratuitousARP(spec.Capture)})
	}
	return actions, nil
}

func bgpDeliveryForwardInterfaces(router *api.Router) []string {
	all := map[string]bool{}
	samOwned := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" {
			continue
		}
		name := strings.TrimSpace(resource.Metadata.Name)
		if name != "" {
			all[name] = true
			for _, owner := range resource.Metadata.OwnerRefs {
				if owner.APIVersion == api.MobilityAPIVersion && owner.Kind == "SAMTransportProfile" {
					samOwned[name] = true
				}
			}
		}
	}
	if len(samOwned) > 0 {
		return sortedKeys(samOwned)
	}
	return sortedKeys(all)
}

func returnPolicyRouteAction(resource api.Resource, spec api.RemoteAddressClaimSpec, address string, peers map[string]api.OverlayPeerSpec, domains map[string]string) (CaptureAction, bool, error) {
	if strings.TrimSpace(spec.Capture.Type) != "provider-secondary-ip" || !spec.Capture.ConfigureOSAddress {
		return CaptureAction{}, false, nil
	}
	if strings.TrimSpace(spec.Delivery.Mode) == "bgp" {
		return CaptureAction{}, false, nil
	}
	domainName := normalizeRefName(spec.DomainRef, "AddressMobilityDomain")
	destination := strings.TrimSpace(domains[domainName])
	if destination == "" {
		return CaptureAction{}, false, nil
	}
	destinationPrefix, err := netip.ParsePrefix(destination)
	if err != nil || !destinationPrefix.Addr().Is4() {
		return CaptureAction{}, false, fmt.Errorf("%s AddressMobilityDomain/%s prefix must be an IPv4 CIDR", resource.ID(), domainName)
	}
	device := strings.TrimSpace(spec.Delivery.TunnelInterface)
	if device == "" {
		peerName := normalizeRefName(spec.Delivery.PeerRef, "OverlayPeer")
		peer, ok := peers[peerName]
		if !ok {
			return CaptureAction{}, false, fmt.Errorf("%s spec.delivery.peerRef references missing OverlayPeer %q", resource.ID(), spec.Delivery.PeerRef)
		}
		resolvedDevice, _, err := hybrid.RouteTarget(peer)
		if err != nil {
			return CaptureAction{}, false, fmt.Errorf("%s: %w", resource.ID(), err)
		}
		device = resolvedDevice
	}
	table, priority := ReturnPolicyRouteIDs(resource.Metadata.Name, address)
	return CaptureAction{
		Kind:        "return-policy-route",
		ClaimName:   resource.Metadata.Name,
		Address:     address,
		Destination: destinationPrefix.Masked().String(),
		Interface:   device,
		Table:       table,
		Priority:    priority,
		Metric:      DeliveryRouteMetricDefault,
	}, true, nil
}

func ReturnPolicyRouteIDs(claimName, address string) (int, int) {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.TrimSpace(claimName)))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(strings.TrimSpace(address)))
	slot := int(h.Sum32() % 1000)
	return 42000 + slot, 14200 + slot
}

func CaptureExcludesAddress(capture api.AddressCapture, address string) bool {
	addr, ok := normalizeIPv4Addr(address)
	if !ok {
		return false
	}
	for _, raw := range capture.ExcludeAddresses {
		prefix, ok := normalizeIPv4ExcludePrefix(raw)
		if ok && prefix.Contains(addr) {
			return true
		}
	}
	return false
}

func IPv4PrefixesExcluding(pool netip.Prefix, excludes []string) []netip.Prefix {
	pool = pool.Masked()
	if !pool.Addr().Is4() {
		return nil
	}
	start, end := ipv4PrefixRange(pool)
	ranges := []ipv4Range{{start: start, end: end}}
	for _, raw := range excludes {
		exclude, ok := normalizeIPv4ExcludePrefix(raw)
		if !ok {
			continue
		}
		exclude = exclude.Masked()
		if !pool.Overlaps(exclude) {
			continue
		}
		excludeStart, excludeEnd := ipv4PrefixRange(exclude)
		var next []ipv4Range
		for _, current := range ranges {
			if excludeEnd < current.start || excludeStart > current.end {
				next = append(next, current)
				continue
			}
			if excludeStart > current.start {
				next = append(next, ipv4Range{start: current.start, end: excludeStart - 1})
			}
			if excludeEnd < current.end {
				next = append(next, ipv4Range{start: excludeEnd + 1, end: current.end})
			}
		}
		ranges = next
	}
	var out []netip.Prefix
	for _, r := range ranges {
		out = append(out, ipv4RangePrefixes(r.start, r.end)...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Addr() == out[j].Addr() {
			return out[i].Bits() < out[j].Bits()
		}
		return out[i].Addr().Compare(out[j].Addr()) < 0
	})
	return out
}

type ipv4Range struct {
	start uint32
	end   uint32
}

func normalizeIPv4Addr(value string) (netip.Addr, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Addr{}, false
	}
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
		return prefix.Masked().Addr(), true
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return netip.Addr{}, false
	}
	return addr, true
}

func normalizeIPv4ExcludePrefix(value string) (netip.Prefix, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return netip.Prefix{}, false
	}
	if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Addr().Is4() {
		return prefix.Masked(), true
	}
	addr, err := netip.ParseAddr(value)
	if err != nil || !addr.Is4() {
		return netip.Prefix{}, false
	}
	return netip.PrefixFrom(addr, 32), true
}

func ipv4PrefixRange(prefix netip.Prefix) (uint32, uint32) {
	addr := ipv4ToUint32(prefix.Masked().Addr())
	size := uint64(1) << uint(32-prefix.Bits())
	return addr, addr + uint32(size-1)
}

func ipv4RangePrefixes(start, end uint32) []netip.Prefix {
	var out []netip.Prefix
	for uint64(start) <= uint64(end) {
		zeroBits := bits.TrailingZeros32(start)
		if start == 0 {
			zeroBits = 32
		}
		blockSize := uint64(1) << uint(zeroBits)
		remaining := uint64(end) - uint64(start) + 1
		for blockSize > remaining {
			zeroBits--
			blockSize >>= 1
		}
		bitsLen := 32 - zeroBits
		out = append(out, netip.PrefixFrom(uint32ToIPv4(start), bitsLen))
		if blockSize > uint64(^uint32(0))-uint64(start) {
			break
		}
		start += uint32(blockSize)
	}
	return out
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	raw := addr.As4()
	return uint32(raw[0])<<24 | uint32(raw[1])<<16 | uint32(raw[2])<<8 | uint32(raw[3])
}

func uint32ToIPv4(value uint32) netip.Addr {
	return netip.AddrFrom4([4]byte{byte(value >> 24), byte(value >> 16), byte(value >> 8), byte(value)})
}

// CaptureInterfaceAliases maps Interface resource names to their Linux ifname.
func CaptureInterfaceAliases(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Interface" {
			continue
		}
		spec, err := resource.InterfaceSpec()
		if err != nil {
			continue
		}
		name := strings.TrimSpace(resource.Metadata.Name)
		ifname := strings.TrimSpace(spec.IfName)
		if name != "" && ifname != "" {
			out[name] = ifname
		}
	}
	return out
}

// ResolveCaptureInterface resolves a capture interface resource name to ifname.
func ResolveCaptureInterface(value string, aliases map[string]string) string {
	value = strings.TrimSpace(value)
	if aliases == nil {
		return value
	}
	if ifname := strings.TrimSpace(aliases[value]); ifname != "" {
		return ifname
	}
	return value
}

func EvaluateCaptureGate(capture api.AddressCapture, store StatusReader) CaptureGateStatus {
	activeWhen := capture.ActiveWhen
	gateType := strings.TrimSpace(activeWhen.Type)
	ref := normalizeRefName(activeWhen.VirtualAddressRef, "VirtualAddress")
	if gateType == "" && ref == "" {
		return CaptureGateStatus{Active: true, Reason: "AlwaysActive"}
	}
	if gateType == "single-router" && ref == "" {
		return CaptureGateStatus{Active: true, Type: gateType, Reason: "SingleRouter"}
	}
	status := CaptureGateStatus{
		Type:              gateType,
		VirtualAddressRef: ref,
		Reason:            "CaptureGateInactive",
	}
	if gateType == "single-router" {
		status.Message = "capture activeWhen virtualAddressRef must be empty when type is single-router"
		return status
	}
	if gateType != "vrrp-master" {
		status.Message = fmt.Sprintf("unsupported capture activeWhen type %q", gateType)
		return status
	}
	if ref == "" {
		status.Message = "capture activeWhen virtualAddressRef is empty"
		return status
	}
	if store == nil {
		status.Message = fmt.Sprintf("VirtualAddress/%s status is unavailable", ref)
		return status
	}
	role := strings.TrimSpace(fmt.Sprint(store.ObjectStatus(api.NetAPIVersion, "VirtualAddress", ref)["role"]))
	if role == "<nil>" {
		role = ""
	}
	status.VirtualAddressRole = role
	if strings.EqualFold(role, "master") {
		status.Active = true
		status.Reason = "CaptureGateActive"
		status.Message = fmt.Sprintf("VirtualAddress/%s role is master", ref)
		return status
	}
	status.Message = fmt.Sprintf("VirtualAddress/%s role is %s", ref, firstNonEmpty(role, "unknown"))
	return status
}

func wantsGratuitousARP(capture api.AddressCapture) bool {
	if capture.GratuitousARP {
		return true
	}
	return strings.TrimSpace(capture.ActiveWhen.Type) == "vrrp-master"
}

func ProxyARPInterfaces(router *api.Router) []string {
	if router == nil {
		return nil
	}
	interfaces := map[string]bool{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "RemoteAddressClaim" {
			continue
		}
		spec, err := resource.RemoteAddressClaimSpec()
		if err != nil || strings.TrimSpace(spec.Capture.Type) != "proxy-arp" {
			continue
		}
		if strings.TrimSpace(spec.Capture.ActiveWhen.Type) != "" || strings.TrimSpace(spec.Capture.ActiveWhen.VirtualAddressRef) != "" {
			continue
		}
		if iface := strings.TrimSpace(spec.Capture.Interface); iface != "" {
			interfaces[iface] = true
		}
	}
	return sortedKeys(interfaces)
}

func DeliveryRouteName(claimName string) string {
	return "sam-" + safeName(claimName) + "-delivery"
}

func StatusForRemoteAddressClaim(resource api.Resource, lowerings []DeliveryLowering, store StatusReader, targetOS platform.OS) map[string]any {
	spec, err := resource.RemoteAddressClaimSpec()
	if err != nil {
		return map[string]any{"phase": "Degraded", "reason": "SpecInvalid", "message": err.Error(), "captureStatus": CaptureStatusBlocked}
	}
	status := map[string]any{
		"phase":        "Ready",
		"domainRef":    normalizeRefName(spec.DomainRef, "AddressMobilityDomain"),
		"address":      strings.TrimSpace(spec.Address),
		"ownerSide":    strings.TrimSpace(spec.OwnerSide),
		"captureType":  strings.TrimSpace(spec.Capture.Type),
		"peerRef":      normalizeRefName(spec.Delivery.PeerRef, "OverlayPeer"),
		"deliveryMode": strings.TrimSpace(spec.Delivery.Mode),
	}
	if targetOS != platform.OSLinux {
		status["phase"] = "Degraded"
		status["reason"] = "CaptureUnsupported"
		status["message"] = "SAM capture not implemented on this OS"
		status["captureStatus"] = CaptureStatusBlocked
		return status
	}
	if strings.TrimSpace(spec.Capture.Type) == "proxy-arp" && CaptureExcludesAddress(spec.Capture, spec.Address) {
		status["phase"] = "Gated"
		status["reason"] = "CaptureExcluded"
		status["message"] = "proxy-ARP capture is excluded by capture.excludeAddresses"
		status["captureActive"] = false
		status["captureStatus"] = CaptureStatusStandby
		return status
	}
	if gate := EvaluateCaptureGate(spec.Capture, store); !gate.Active {
		status["phase"] = "Gated"
		status["reason"] = gate.Reason
		status["message"] = gate.Message
		status["captureActive"] = false
		status["captureStatus"] = captureStatusForInactiveGate(gate)
		status["activeWhenType"] = gate.Type
		status["activeWhenVirtualAddressRef"] = gate.VirtualAddressRef
		status["activeWhenVirtualAddressRole"] = gate.VirtualAddressRole
		return status
	} else if gate.Type != "" || gate.VirtualAddressRef != "" {
		status["captureActive"] = true
		status["captureStatus"] = CaptureStatusCaptured
		status["activeWhenType"] = gate.Type
		status["activeWhenVirtualAddressRef"] = gate.VirtualAddressRef
		status["activeWhenVirtualAddressRole"] = gate.VirtualAddressRole
	}
	if strings.TrimSpace(spec.Delivery.Mode) == "bgp" {
		if strings.TrimSpace(spec.Capture.Interface) != "" {
			status["captureInterface"] = strings.TrimSpace(spec.Capture.Interface)
		}
		if strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
			if _, exists := status["captureStatus"]; !exists {
				status["captureStatus"] = CaptureStatusCaptured
			}
		}
		return status
	}
	lowering, ok := deliveryLoweringForClaim(resource.Metadata.Name, lowerings)
	if !ok {
		status["phase"] = "Degraded"
		status["reason"] = "RouteNotLowered"
		status["message"] = "delivery route was not lowered to an IPv4Route"
		status["captureStatus"] = CaptureStatusBlocked
		return status
	}
	status["deliveryRouteName"] = lowering.IPv4RouteName
	status["deliveryDevice"] = lowering.Device
	if strings.TrimSpace(lowering.PreferredSource) != "" {
		status["deliveryPreferredSource"] = strings.TrimSpace(lowering.PreferredSource)
	}
	status["deliveryMetric"] = lowering.Metric
	if strings.TrimSpace(spec.Capture.Interface) != "" {
		status["captureInterface"] = strings.TrimSpace(spec.Capture.Interface)
	}
	if store != nil {
		routeStatus := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", lowering.IPv4RouteName)
		if phase := strings.TrimSpace(fmt.Sprint(routeStatus["phase"])); phase != "" && phase != "<nil>" {
			status["deliveryRoutePhase"] = phase
			if phase != "Installed" {
				status["phase"] = "Degraded"
				status["reason"] = "RouteNotInstalled"
				status["message"] = "lowered delivery route is not installed"
				status["captureStatus"] = CaptureStatusBlocked
			}
		}
	}
	if strings.TrimSpace(spec.Capture.Type) == "proxy-arp" {
		if _, exists := status["captureStatus"]; !exists {
			status["captureStatus"] = CaptureStatusCaptured
		}
	}
	return status
}

func captureStatusForInactiveGate(gate CaptureGateStatus) string {
	if gate.Type != "vrrp-master" || gate.VirtualAddressRef == "" {
		return CaptureStatusBlocked
	}
	role := strings.TrimSpace(gate.VirtualAddressRole)
	if role == "" {
		return CaptureStatusBlocked
	}
	return CaptureStatusStandby
}

func StatusForAddressMobilityDomain(domain api.Resource, claims []api.Resource, store StatusReader) map[string]any {
	spec, err := domain.AddressMobilityDomainSpec()
	if err != nil {
		return map[string]any{"phase": "Degraded", "reason": "SpecInvalid", "message": err.Error()}
	}
	status := map[string]any{
		"phase":   "Ready",
		"prefix":  strings.TrimSpace(spec.Prefix),
		"mode":    strings.TrimSpace(spec.Mode),
		"peerRef": normalizeRefName(spec.PeerRef, "OverlayPeer"),
	}
	var members []map[string]any
	degraded := false
	for _, claim := range claims {
		claimSpec, err := claim.RemoteAddressClaimSpec()
		if err != nil || normalizeRefName(claimSpec.DomainRef, "AddressMobilityDomain") != domain.Metadata.Name {
			continue
		}
		item := map[string]any{"name": claim.Metadata.Name, "address": strings.TrimSpace(claimSpec.Address)}
		if store != nil {
			claimStatus := store.ObjectStatus(api.HybridAPIVersion, "RemoteAddressClaim", claim.Metadata.Name)
			if phase := strings.TrimSpace(fmt.Sprint(claimStatus["phase"])); phase != "" && phase != "<nil>" {
				item["phase"] = phase
				if phase != "Ready" {
					degraded = true
				}
			}
		}
		members = append(members, item)
	}
	sort.Slice(members, func(i, j int) bool { return fmt.Sprint(members[i]["name"]) < fmt.Sprint(members[j]["name"]) })
	status["claims"] = members
	status["claimCount"] = len(members)
	if degraded {
		status["phase"] = "Degraded"
		status["reason"] = "ClaimDegraded"
		status["message"] = "one or more RemoteAddressClaim members are degraded"
	}
	return status
}

type StatusReader interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

func deliveryLoweringForClaim(name string, lowerings []DeliveryLowering) (DeliveryLowering, bool) {
	for _, lowering := range lowerings {
		if lowering.ClaimName == name {
			return lowering, true
		}
	}
	return DeliveryLowering{}, false
}

func overlayPeers(router api.Router) map[string]api.OverlayPeerSpec {
	out := map[string]api.OverlayPeerSpec{}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "OverlayPeer" {
			continue
		}
		spec, err := resource.OverlayPeerSpec()
		if err == nil {
			out[resource.Metadata.Name] = spec
		}
	}
	return out
}

func addressMobilityDomainPrefixes(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "AddressMobilityDomain" {
			continue
		}
		spec, err := resource.AddressMobilityDomainSpec()
		if err == nil {
			out[resource.Metadata.Name] = strings.TrimSpace(spec.Prefix)
		}
	}
	return out
}

func normalizeClaimAddress(value string) (string, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return "", fmt.Errorf("must be an IPv4 /32 CIDR")
	}
	return prefix.String(), nil
}

func normalizeRefName(ref, kind string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, kind+"/") {
		return strings.TrimPrefix(ref, kind+"/")
	}
	return ref
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var safeNamePattern = regexp.MustCompile(`[^A-Za-z0-9.-]+`)

func safeName(name string) string {
	name = strings.Trim(safeNamePattern.ReplaceAllString(name, "-"), "-.")
	if name == "" {
		return "claim"
	}
	return name
}

func sortedKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
