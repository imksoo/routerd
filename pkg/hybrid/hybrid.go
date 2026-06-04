// SPDX-License-Identifier: BSD-3-Clause

package hybrid

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

const (
	WireGuardOverheadBytes = 80
	TailscaleOverheadBytes = 80
	IPsecOverheadBytes     = 74
	RouteOverheadBytes     = 0
	IPIPOverheadBytes      = 20
	GREOverheadBytes       = 24
	GREKeyOverheadBytes    = 4
	FOUOverheadBytes       = 28
	GUEOverheadBytes       = 32
	TunnelIPIPDefaultMTU   = 1480
	TunnelGREDefaultMTU    = 1476
	TunnelFOUDefaultMTU    = 1472
	TunnelGUEDefaultMTU    = 1468
	MinimumIPv6MTU         = 1280
)

type HybridLowering struct {
	HybridRouteName string
	PeerName        string
	DestinationCIDR string
	IPv4RouteName   string
	Device          string
	Gateway         string
	Metric          int
}

type StatusReader interface {
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

func ExpandHybridRoutes(router api.Router) (api.Router, []HybridLowering, error) {
	if !HasHybridRoutes(&router) {
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
	var lowerings []HybridLowering
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "HybridRoute" {
			continue
		}
		spec, err := resource.HybridRouteSpec()
		if err != nil {
			return router, nil, err
		}
		peerName := normalizeRefName(spec.PeerRef, "OverlayPeer")
		peer, ok := peers[peerName]
		if !ok {
			return router, nil, fmt.Errorf("%s spec.peerRef references missing OverlayPeer %q", resource.ID(), spec.PeerRef)
		}
		device, gateway, err := RouteTarget(peer)
		if err != nil {
			return router, nil, fmt.Errorf("%s: %w", resource.ID(), err)
		}
		for _, destination := range spec.DestinationCIDRs {
			cidr, err := normalizeHybridDestination(destination)
			if err != nil {
				return router, nil, fmt.Errorf("%s destination %q: %w", resource.ID(), destination, err)
			}
			if existing := userRouteDestinations[cidr]; existing != "" {
				return router, nil, fmt.Errorf("%s destination %s collides with user IPv4Route/%s", resource.ID(), cidr, existing)
			}
			name := resource.Metadata.Name + "-" + cidrSlug(cidr)
			if userRouteNames[name] {
				return router, nil, fmt.Errorf("%s synthetic IPv4Route name %q collides with a user-authored IPv4Route", resource.ID(), name)
			}
			if syntheticNames[name] {
				return router, nil, fmt.Errorf("%s synthetic IPv4Route name %q is not unique", resource.ID(), name)
			}
			syntheticNames[name] = true
			route := api.Resource{
				TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "IPv4Route"},
				Metadata: api.ObjectMeta{
					Name: name,
					OwnerRefs: []api.OwnerRef{{
						APIVersion: api.HybridAPIVersion,
						Kind:       "HybridRoute",
						Name:       resource.Metadata.Name,
					}},
				},
				Spec: api.IPv4RouteSpec{
					Destination: cidr,
					Type:        "unicast",
					Device:      device,
					Gateway:     gateway,
					Metric:      spec.Install.Metric,
				},
			}
			out.Spec.Resources = append(out.Spec.Resources, route)
			lowerings = append(lowerings, HybridLowering{
				HybridRouteName: resource.Metadata.Name,
				PeerName:        peerName,
				DestinationCIDR: cidr,
				IPv4RouteName:   name,
				Device:          device,
				Gateway:         gateway,
				Metric:          spec.Install.Metric,
			})
		}
	}
	return out, lowerings, nil
}

func HasHybridRoutes(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "HybridRoute" {
			return true
		}
	}
	return false
}

func StatusForHybridRoute(router api.Router, resource api.Resource, lowerings []HybridLowering, store StatusReader) map[string]any {
	spec, err := resource.HybridRouteSpec()
	if err != nil {
		return map[string]any{"phase": "Degraded", "reason": "SpecInvalid", "message": err.Error(), "defaultRouteUntouched": true}
	}
	peerName := normalizeRefName(spec.PeerRef, "OverlayPeer")
	status := map[string]any{
		"phase":                 "Ready",
		"peerRef":               peerName,
		"defaultRouteUntouched": true,
		"message":               "hybrid routes lowered to IPv4Route resources",
	}
	routes := routeStatuses(resource.Metadata.Name, lowerings, store)
	status["routes"] = routes
	if len(routes) == 0 && len(spec.DestinationCIDRs) > 0 {
		status["phase"] = "Degraded"
		status["reason"] = "RoutesNotLowered"
		status["message"] = "no lowered IPv4Route resources found"
	} else {
		for _, route := range routes {
			if fmt.Sprint(route["phase"]) != "Installed" {
				status["phase"] = "Degraded"
				status["reason"] = "RouteNotInstalled"
				status["message"] = "one or more lowered IPv4Route resources are not installed"
				break
			}
		}
	}
	if spec.HealthCheckRef != "" {
		health := map[string]any{}
		if store != nil {
			health = store.ObjectStatus(api.NetAPIVersion, "HealthCheck", spec.HealthCheckRef)
		}
		status["healthCheckRef"] = spec.HealthCheckRef
		status["healthCheckPhase"] = fmt.Sprint(health["phase"])
		if healthUnhealthy(health) {
			status["phase"] = "Degraded"
			status["reason"] = "HealthCheckUnhealthy"
			status["message"] = "referenced HealthCheck is unhealthy"
		}
	}
	if estimate, ok := EstimateMTU(router, peerName); ok {
		status["underlayMTU"] = estimate.UnderlayMTU
		status["tunnelOverhead"] = estimate.Overhead
		status["estimatedMTU"] = estimate.EstimatedMTU
		if estimate.Warning != "" {
			status["mtuWarning"] = estimate.Warning
		}
	}
	return status
}

type MTUEstimate struct {
	UnderlayMTU  int
	Overhead     int
	EstimatedMTU int
	Warning      string
}

func EstimateMTU(router api.Router, peerName string) (MTUEstimate, bool) {
	peer, ok := overlayPeers(router)[peerName]
	if !ok {
		return MTUEstimate{}, false
	}
	underlayMTU := underlayMTU(router, peer)
	if underlayMTU == 0 {
		return MTUEstimate{Overhead: overheadForPeer(router, peer)}, true
	}
	overhead := overheadForPeer(router, peer)
	estimate := MTUEstimate{UnderlayMTU: underlayMTU, Overhead: overhead, EstimatedMTU: underlayMTU - overhead}
	if estimate.EstimatedMTU > 0 && estimate.EstimatedMTU < MinimumIPv6MTU {
		estimate.Warning = fmt.Sprintf("estimated MTU %d is below IPv6 minimum %d", estimate.EstimatedMTU, MinimumIPv6MTU)
	}
	return estimate, true
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

func RouteTarget(peer api.OverlayPeerSpec) (string, string, error) {
	device := strings.TrimSpace(peer.Underlay.Interface)
	if device == "" {
		return "", "", fmt.Errorf("spec.underlay.interface is required to lower HybridRoute to IPv4Route")
	}
	switch strings.TrimSpace(peer.Underlay.Type) {
	case "wireguard", "route", "tailscale", "ipip", "gre", "fou", "gue":
		return device, "", nil
	case "ipsec":
		gateway := ""
		if addr, err := netip.ParseAddr(strings.TrimSpace(peer.Remote.Address)); err == nil && addr.Is4() {
			gateway = addr.String()
		}
		return device, gateway, nil
	default:
		return "", "", fmt.Errorf("unsupported underlay type %q", peer.Underlay.Type)
	}
}

func normalizeHybridDestination(value string) (string, error) {
	value = strings.TrimSpace(value)
	if strings.EqualFold(value, "default") {
		return "", fmt.Errorf("default routes are not allowed")
	}
	prefix, err := netip.ParsePrefix(value)
	if err != nil {
		return "", err
	}
	prefix = prefix.Masked()
	if (prefix.Addr().Is4() && prefix.Bits() == 0) || (prefix.Addr().Is6() && prefix.Bits() == 0) {
		return "", fmt.Errorf("default routes are not allowed")
	}
	if !prefix.Addr().Is4() {
		return "", fmt.Errorf("only IPv4 CIDRs can be lowered to IPv4Route")
	}
	return prefix.String(), nil
}

func routeStatuses(hybridRouteName string, lowerings []HybridLowering, store StatusReader) []map[string]any {
	var out []map[string]any
	for _, lowering := range lowerings {
		if lowering.HybridRouteName != hybridRouteName {
			continue
		}
		item := map[string]any{
			"name":        lowering.IPv4RouteName,
			"destination": lowering.DestinationCIDR,
			"device":      lowering.Device,
			"gateway":     lowering.Gateway,
			"metric":      lowering.Metric,
		}
		if store != nil {
			routeStatus := store.ObjectStatus(api.NetAPIVersion, "IPv4Route", lowering.IPv4RouteName)
			if phase := strings.TrimSpace(fmt.Sprint(routeStatus["phase"])); phase != "" && phase != "<nil>" {
				item["phase"] = phase
			}
			if reason := strings.TrimSpace(fmt.Sprint(routeStatus["reason"])); reason != "" && reason != "<nil>" {
				item["reason"] = reason
			}
		}
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		return fmt.Sprint(out[i]["name"]) < fmt.Sprint(out[j]["name"])
	})
	return out
}

func healthUnhealthy(status map[string]any) bool {
	phase := strings.ToLower(strings.TrimSpace(fmt.Sprint(status["phase"])))
	health := strings.ToLower(strings.TrimSpace(fmt.Sprint(status["health"])))
	switch phase {
	case "unhealthy", "failed", "error":
		return true
	}
	switch health {
	case "unhealthy", "failed", "fail", "healthfail", "error":
		return true
	default:
		return false
	}
}

func underlayMTU(router api.Router, peer api.OverlayPeerSpec) int {
	iface := strings.TrimSpace(peer.Underlay.Interface)
	if iface == "" {
		return 0
	}
	for _, resource := range router.Spec.Resources {
		if resource.Metadata.Name != iface {
			continue
		}
		switch resource.Kind {
		case "WireGuardInterface":
			spec, err := resource.WireGuardInterfaceSpec()
			if err == nil {
				return spec.MTU
			}
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil {
				return spec.MTU
			}
		case "TunnelInterface":
			spec, err := resource.TunnelInterfaceSpec()
			if err == nil {
				return tunnelInterfaceMTU(spec)
			}
		}
	}
	return 0
}

func tunnelInterfaceMTU(spec api.TunnelInterfaceSpec) int {
	if spec.MTU > 0 {
		return spec.MTU
	}
	switch strings.TrimSpace(spec.Mode) {
	case "ipip":
		return TunnelIPIPDefaultMTU
	case "gre":
		return TunnelGREDefaultMTU
	case "fou":
		return TunnelFOUDefaultMTU
	case "gue":
		return TunnelGUEDefaultMTU
	default:
		return 0
	}
}

func overheadForPeer(router api.Router, peer api.OverlayPeerSpec) int {
	overhead := overheadFor(peer.Underlay.Type)
	if strings.TrimSpace(peer.Underlay.Type) != "gre" {
		return overhead
	}
	iface := strings.TrimSpace(peer.Underlay.Interface)
	if iface == "" {
		return overhead
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "TunnelInterface" || resource.Metadata.Name != iface {
			continue
		}
		spec, err := resource.TunnelInterfaceSpec()
		if err == nil && spec.Key != 0 {
			return overhead + GREKeyOverheadBytes
		}
	}
	return overhead
}

func overheadFor(underlayType string) int {
	switch strings.TrimSpace(underlayType) {
	case "wireguard":
		return WireGuardOverheadBytes
	case "tailscale":
		return TailscaleOverheadBytes
	case "ipsec":
		return IPsecOverheadBytes
	case "route":
		return RouteOverheadBytes
	case "ipip":
		return IPIPOverheadBytes
	case "gre":
		return GREOverheadBytes
	case "fou":
		return FOUOverheadBytes
	case "gue":
		return GUEOverheadBytes
	default:
		return 0
	}
}

func normalizeRefName(ref, kind string) string {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, kind+"/") {
		return strings.TrimPrefix(ref, kind+"/")
	}
	return ref
}

var cidrSlugPattern = regexp.MustCompile(`[^A-Za-z0-9]+`)

func cidrSlug(cidr string) string {
	slug := cidrSlugPattern.ReplaceAllString(cidr, "-")
	return strings.Trim(slug, "-")
}
