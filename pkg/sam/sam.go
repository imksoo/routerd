// SPDX-License-Identifier: BSD-3-Clause

package sam

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
)

const DeliveryRouteMetricDefault = 120

type DeliveryLowering struct {
	ClaimName      string
	AddressCIDR    string
	IPv4RouteName  string
	Device         string
	Metric         int
	OwnerSide      string
	CaptureType    string
	DeliveryPeer   string
	DeliveryMode   string
	CaptureIface   string
	CaptureMessage string
}

type CaptureAction struct {
	Kind      string
	ClaimName string
	Address   string
	Interface string
	Key       string
	Value     string
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
				Destination: cidr,
				Type:        "unicast",
				Device:      device,
				Metric:      DeliveryRouteMetricDefault,
			},
		}
		out.Spec.Resources = append(out.Spec.Resources, route)
		lowerings = append(lowerings, DeliveryLowering{
			ClaimName:     resource.Metadata.Name,
			AddressCIDR:   cidr,
			IPv4RouteName: name,
			Device:        device,
			Metric:        DeliveryRouteMetricDefault,
			OwnerSide:     strings.TrimSpace(spec.OwnerSide),
			CaptureType:   strings.TrimSpace(spec.Capture.Type),
			DeliveryPeer:  peerName,
			DeliveryMode:  strings.TrimSpace(spec.Delivery.Mode),
			CaptureIface:  strings.TrimSpace(spec.Capture.Interface),
		})
	}
	return out, lowerings, nil
}

func PlanCapture(router *api.Router, targetOS platform.OS) ([]CaptureAction, error) {
	if router == nil || !HasRemoteAddressClaims(router) || targetOS != platform.OSLinux {
		return nil, nil
	}
	interfaces := map[string]bool{}
	actions := []CaptureAction{{Kind: "sysctl", Key: "net.ipv4.ip_forward", Value: "1"}}
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
		if strings.TrimSpace(spec.Capture.Type) != "proxy-arp" {
			continue
		}
		iface := strings.TrimSpace(spec.Capture.Interface)
		if iface == "" {
			return nil, fmt.Errorf("%s spec.capture.interface is required for proxy-arp", resource.ID())
		}
		if !interfaces[iface] {
			interfaces[iface] = true
			actions = append(actions, CaptureAction{Kind: "sysctl", Key: "net.ipv4.conf." + iface + ".proxy_arp", Value: "1", Interface: iface})
		}
		actions = append(actions, CaptureAction{Kind: "proxy-neighbor", ClaimName: resource.Metadata.Name, Address: address, Interface: iface})
	}
	return actions, nil
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
		return map[string]any{"phase": "Degraded", "reason": "SpecInvalid", "message": err.Error()}
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
		return status
	}
	lowering, ok := deliveryLoweringForClaim(resource.Metadata.Name, lowerings)
	if !ok {
		status["phase"] = "Degraded"
		status["reason"] = "RouteNotLowered"
		status["message"] = "delivery route was not lowered to an IPv4Route"
		return status
	}
	status["deliveryRouteName"] = lowering.IPv4RouteName
	status["deliveryDevice"] = lowering.Device
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
			}
		}
	}
	return status
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
