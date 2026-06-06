// SPDX-License-Identifier: BSD-3-Clause

package hybrid

import (
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"net/netip"
	"regexp"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

type SAMTransportProfileLowering struct {
	ProfileName        string
	PeerName           string
	PeerNodeID         string
	TunnelInterface    string
	OverlayPeerName    string
	BGPPeerName        string
	LocalInnerAddress  string
	RemoteInnerAddress string
	WireGuardInterface string
	LocalWGAddress     string
	RemoteWGAddress    string
	GeneratedResources []api.OwnerRef
}

func HasSAMTransportProfiles(router *api.Router) bool {
	if router == nil {
		return false
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.HybridAPIVersion && resource.Kind == "SAMTransportProfile" {
			return true
		}
	}
	return false
}

func ExpandSAMTransportProfiles(router api.Router) (api.Router, []SAMTransportProfileLowering, error) {
	if !HasSAMTransportProfiles(&router) {
		return router, nil, nil
	}
	out := router
	out.Spec.Resources = append([]api.Resource(nil), router.Spec.Resources...)
	existing := resourceIndex(router.Spec.Resources)
	original := resourceIndex(router.Spec.Resources)
	var lowerings []SAMTransportProfileLowering
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.HybridAPIVersion || resource.Kind != "SAMTransportProfile" {
			continue
		}
		spec, err := resource.SAMTransportProfileSpec()
		if err != nil {
			return router, nil, err
		}
		profileLowerings, generated, err := expandSAMTransportProfile(resource, spec)
		if err != nil {
			return router, nil, err
		}
		for _, generatedResource := range generated {
			key := resourceKey(generatedResource)
			if current, ok := existing[key]; ok {
				if !ownedBy(current, api.HybridAPIVersion, "SAMTransportProfile", resource.Metadata.Name) {
					return router, nil, fmt.Errorf("%s synthetic %s collides with existing %s", resource.ID(), generatedResource.ID(), current.ID())
				}
				if _, ok := original[key]; !ok {
					return router, nil, fmt.Errorf("%s synthetic %s is duplicated within the generated profile resources", resource.ID(), generatedResource.ID())
				}
				continue
			}
			existing[key] = generatedResource
			out.Spec.Resources = append(out.Spec.Resources, generatedResource)
		}
		lowerings = append(lowerings, profileLowerings...)
	}
	return out, lowerings, nil
}

func StatusForSAMTransportProfile(resource api.Resource, lowerings []SAMTransportProfileLowering, store StatusReader) map[string]any {
	status := map[string]any{
		"phase":   "Ready",
		"message": "SAM transport profile lowered to tunnel resources",
	}
	var peers []map[string]any
	for _, lowering := range lowerings {
		if lowering.ProfileName != resource.Metadata.Name {
			continue
		}
		peer := map[string]any{
			"name":               lowering.PeerName,
			"nodeID":             lowering.PeerNodeID,
			"tunnelInterface":    lowering.TunnelInterface,
			"overlayPeer":        lowering.OverlayPeerName,
			"localInnerAddress":  lowering.LocalInnerAddress,
			"remoteInnerAddress": lowering.RemoteInnerAddress,
		}
		if lowering.BGPPeerName != "" {
			peer["bgpPeer"] = lowering.BGPPeerName
		}
		if lowering.WireGuardInterface != "" {
			peer["wireGuardInterface"] = lowering.WireGuardInterface
			peer["localWireGuardAddress"] = lowering.LocalWGAddress
			peer["remoteWireGuardAddress"] = lowering.RemoteWGAddress
		}
		var resources []map[string]any
		for _, ref := range lowering.GeneratedResources {
			item := map[string]any{"apiVersion": ref.APIVersion, "kind": ref.Kind, "name": ref.Name}
			if store != nil {
				objectStatus := store.ObjectStatus(ref.APIVersion, ref.Kind, ref.Name)
				if phase := strings.TrimSpace(fmt.Sprint(objectStatus["phase"])); phase != "" && phase != "<nil>" {
					item["phase"] = phase
				}
				if reason := strings.TrimSpace(fmt.Sprint(objectStatus["reason"])); reason != "" && reason != "<nil>" {
					item["reason"] = reason
				}
				if item["phase"] != nil {
					switch item["phase"] {
					case "Error", "Degraded", "Unsupported":
						status["phase"] = "Degraded"
						status["reason"] = "GeneratedResourceNotReady"
						status["message"] = "one or more generated resources are not ready"
					}
				}
			}
			resources = append(resources, item)
		}
		peer["resources"] = resources
		peers = append(peers, peer)
	}
	status["peers"] = peers
	if len(peers) == 0 {
		status["phase"] = "Degraded"
		status["reason"] = "NotLowered"
		status["message"] = "no peer lowerings found"
	}
	return status
}

func expandSAMTransportProfile(resource api.Resource, spec api.SAMTransportProfileSpec) ([]SAMTransportProfileLowering, []api.Resource, error) {
	mode := strings.TrimSpace(spec.Mode)
	encryption := strings.TrimSpace(spec.Encryption)
	if encryption == "" {
		encryption = "none"
	}
	innerCIDR, err := parseIPv4Prefix(spec.InnerCIDR)
	if err != nil {
		return nil, nil, fmt.Errorf("%s spec.innerCIDR: %w", resource.ID(), err)
	}
	var wgCIDR netip.Prefix
	if encryption == "wireguard" {
		wgCIDR, err = parseIPv4Prefix(spec.WireGuard.TransportCIDR)
		if err != nil {
			return nil, nil, fmt.Errorf("%s spec.wireGuard.transportCIDR: %w", resource.ID(), err)
		}
	}
	var lowerings []SAMTransportProfileLowering
	var generated []api.Resource
	usedInnerPairs := map[uint64]bool{}
	usedWGPairs := map[uint64]bool{}
	wgName := strings.TrimSpace(spec.WireGuard.Interface)
	if wgName == "" && encryption == "wireguard" {
		wgName = shortInterfaceName("swg", resource.Metadata.Name, "wg")
		generated = append(generated, ownedResource(resource, api.NetAPIVersion, "WireGuardInterface", wgName, api.WireGuardInterfaceSpec{
			PrivateKey:     spec.WireGuard.PrivateKey,
			PrivateKeyFile: spec.WireGuard.PrivateKeyFile,
			ListenPort:     spec.WireGuard.ListenPort,
			MTU:            spec.WireGuard.MTU,
		}))
	} else if encryption == "wireguard" {
		generated = append(generated, ownedResource(resource, api.NetAPIVersion, "WireGuardInterface", wgName, api.WireGuardInterfaceSpec{
			PrivateKey:     spec.WireGuard.PrivateKey,
			PrivateKeyFile: spec.WireGuard.PrivateKeyFile,
			ListenPort:     spec.WireGuard.ListenPort,
			MTU:            spec.WireGuard.MTU,
		}))
	}
	for _, peer := range spec.Peers {
		peerGeneratedStart := len(generated)
		peerName := strings.TrimSpace(peer.Name)
		if peerName == "" {
			return nil, nil, fmt.Errorf("%s spec.peers[].name is required", resource.ID())
		}
		peerNodeID := strings.TrimSpace(peer.NodeID)
		if peerNodeID == "" {
			return nil, nil, fmt.Errorf("%s spec.peers[%s].nodeID is required", resource.ID(), peerName)
		}
		tunnelName := strings.TrimSpace(peer.TunnelInterface)
		if tunnelName == "" {
			tunnelName = shortInterfaceName("stp", resource.Metadata.Name, peerName)
		}
		localInner, remoteInner, pairIndex, err := deterministicPair(innerCIDR, spec.LocalNodeID, peerNodeID, usedInnerPairs)
		if err != nil {
			return nil, nil, fmt.Errorf("%s peer %s inner pair: %w", resource.ID(), peerName, err)
		}
		usedInnerPairs[pairIndex] = true
		if override := strings.TrimSpace(peer.InnerAddress); override != "" {
			addr, err := parseIPv4AddressOrPrefix(override)
			if err != nil {
				return nil, nil, fmt.Errorf("%s peer %s innerAddress: %w", resource.ID(), peerName, err)
			}
			remoteInner = addr
		}
		localInnerPrefix := localInner.String() + "/31"
		tunnel := api.TunnelInterfaceSpec{
			Mode:              mode,
			UnderlayInterface: firstNonEmpty(strings.TrimSpace(peer.UnderlayInterface), strings.TrimSpace(spec.UnderlayInterface)),
			TrustedUnderlay:   true,
			PathMTU:           api.PathMTUOptions{ForceFragmentIPv4: true},
		}
		localWG := ""
		remoteWG := ""
		if encryption == "wireguard" {
			localWGAddr, remoteWGAddr, wgPairIndex, err := deterministicPair(wgCIDR, spec.LocalNodeID, peerNodeID, usedWGPairs)
			if err != nil {
				return nil, nil, fmt.Errorf("%s peer %s wireGuard pair: %w", resource.ID(), peerName, err)
			}
			usedWGPairs[wgPairIndex] = true
			localWG = localWGAddr.String()
			remoteWG = remoteWGAddr.String()
			if override := strings.TrimSpace(spec.WireGuard.LocalAddress); override != "" {
				addr, err := parseIPv4AddressOrPrefix(override)
				if err != nil {
					return nil, nil, fmt.Errorf("%s spec.wireGuard.localAddress: %w", resource.ID(), err)
				}
				localWG = addr.String()
			}
			if override := strings.TrimSpace(peer.WireGuard.TransportAddress); override != "" {
				addr, err := parseIPv4AddressOrPrefix(override)
				if err != nil {
					return nil, nil, fmt.Errorf("%s peer %s wireGuard.transportAddress: %w", resource.ID(), peerName, err)
				}
				remoteWG = addr.String()
			}
			tunnel.Local = localWG
			tunnel.Remote = remoteWG
			tunnel.UnderlayInterface = wgName
			generated = append(generated, ownedResource(resource, api.NetAPIVersion, "IPv4StaticAddress", shortResourceName(resource.Metadata.Name, peerName, "wgaddr"), api.IPv4StaticAddressSpec{
				Interface:          wgName,
				Address:            localWG + "/32",
				AllowOverlap:       true,
				AllowOverlapReason: "SAM transport profile generated WireGuard endpoint address",
			}))
			generated = append(generated, ownedResource(resource, api.NetAPIVersion, "WireGuardPeer", shortResourceName(resource.Metadata.Name, peerName, "wgpeer"), api.WireGuardPeerSpec{
				Interface:           wgName,
				PublicKey:           strings.TrimSpace(peer.WireGuard.PublicKey),
				AllowedIPs:          []string{remoteWG + "/32"},
				Endpoint:            firstNonEmpty(strings.TrimSpace(peer.WireGuard.Endpoint), strings.TrimSpace(peer.Endpoint)),
				PersistentKeepalive: firstNonZero(peer.WireGuard.PersistentKeepalive, spec.WireGuard.PersistentKeepalive),
				PresharedKey:        peer.WireGuard.PresharedKey,
				PresharedKeyFile:    peer.WireGuard.PresharedKeyFile,
			}))
		} else {
			tunnel.Local = strings.TrimSpace(spec.LocalEndpoint)
			tunnel.LocalFrom = spec.LocalEndpointFrom
			tunnel.Remote = strings.TrimSpace(peer.Endpoint)
			tunnel.RemoteFrom = peer.EndpointFrom
		}
		generated = append(generated, ownedResource(resource, api.HybridAPIVersion, "TunnelInterface", tunnelName, tunnel))
		generated = append(generated, ownedResource(resource, api.NetAPIVersion, "IPv4StaticAddress", shortResourceName(resource.Metadata.Name, peerName, "inner"), api.IPv4StaticAddressSpec{
			Interface:          tunnelName,
			Address:            localInnerPrefix,
			AllowOverlap:       true,
			AllowOverlapReason: "SAM transport profile generated tunnel endpoint address",
		}))
		generated = append(generated, ownedResource(resource, api.NetAPIVersion, "IPv4Route", shortResourceName(resource.Metadata.Name, peerName, "inner-route"), api.IPv4RouteSpec{
			Destination: remoteInner.String() + "/32",
			Type:        "unicast",
			Device:      tunnelName,
		}))
		overlayPeerName := shortResourceName(resource.Metadata.Name, peerName, "peer")
		role := firstNonEmpty(strings.TrimSpace(peer.Role), strings.TrimSpace(spec.PeerRole))
		generated = append(generated, ownedResource(resource, api.HybridAPIVersion, "OverlayPeer", overlayPeerName, api.OverlayPeerSpec{
			Role:   role,
			NodeID: peerNodeID,
			Underlay: api.OverlayUnderlay{
				Type:      mode,
				Interface: tunnelName,
			},
			Remote: api.OverlayRemote{
				NodeID:  peerNodeID,
				Address: remoteInner.String(),
			},
			PathMTU: api.PathMTUOptions{ForceFragmentIPv4: true},
		}))
		bgpPeerName := ""
		if strings.TrimSpace(spec.BGP.RouterRef) != "" {
			peerASN := peer.PeerASN
			if peerASN == 0 {
				peerASN = spec.BGP.PeerASN
			}
			bgpPeerName = shortResourceName(resource.Metadata.Name, peerName, "bgp")
			generated = append(generated, ownedResource(resource, api.NetAPIVersion, "BGPPeer", bgpPeerName, api.BGPPeerSpec{
				RouterRef:               spec.BGP.RouterRef,
				PeerASN:                 peerASN,
				Peers:                   []string{remoteInner.String()},
				RouteReflectorClient:    spec.BGP.RouteReflectorClient,
				RouteReflectorClusterID: spec.BGP.RouteReflectorClusterID,
				Timers:                  spec.BGP.Timers,
				ImportPolicy:            spec.BGP.ImportPolicy,
				ExportPolicy:            spec.BGP.ExportPolicy,
			}))
		}
		lowering := SAMTransportProfileLowering{
			ProfileName:        resource.Metadata.Name,
			PeerName:           peerName,
			PeerNodeID:         peerNodeID,
			TunnelInterface:    tunnelName,
			OverlayPeerName:    overlayPeerName,
			BGPPeerName:        bgpPeerName,
			LocalInnerAddress:  localInner.String(),
			RemoteInnerAddress: remoteInner.String(),
			LocalWGAddress:     localWG,
			RemoteWGAddress:    remoteWG,
		}
		if encryption == "wireguard" {
			lowering.WireGuardInterface = wgName
		}
		for _, generatedResource := range generated[peerGeneratedStart:] {
			if ownedBy(generatedResource, api.HybridAPIVersion, "SAMTransportProfile", resource.Metadata.Name) {
				lowering.GeneratedResources = append(lowering.GeneratedResources, api.OwnerRef{
					APIVersion: generatedResource.APIVersion,
					Kind:       generatedResource.Kind,
					Name:       generatedResource.Metadata.Name,
				})
			}
		}
		lowerings = append(lowerings, lowering)
	}
	return lowerings, generated, nil
}

func resourceIndex(resources []api.Resource) map[string]api.Resource {
	out := map[string]api.Resource{}
	for _, resource := range resources {
		out[resourceKey(resource)] = resource
	}
	return out
}

func resourceKey(resource api.Resource) string {
	return resource.APIVersion + "/" + resource.Kind + "/" + resource.Metadata.Name
}

func ownedResource(owner api.Resource, apiVersion, kind, name string, spec any) api.Resource {
	return api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: apiVersion, Kind: kind},
		Metadata: api.ObjectMeta{
			Name: name,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.HybridAPIVersion,
				Kind:       "SAMTransportProfile",
				Name:       owner.Metadata.Name,
			}},
		},
		Spec: spec,
	}
}

func ownedBy(resource api.Resource, apiVersion, kind, name string) bool {
	for _, ref := range resource.Metadata.OwnerRefs {
		if ref.APIVersion == apiVersion && ref.Kind == kind && ref.Name == name {
			return true
		}
	}
	return false
}

func deterministicPair(prefix netip.Prefix, localNodeID, remoteNodeID string, used map[uint64]bool) (netip.Addr, netip.Addr, uint64, error) {
	localNodeID = strings.TrimSpace(localNodeID)
	remoteNodeID = strings.TrimSpace(remoteNodeID)
	if localNodeID == "" {
		return netip.Addr{}, netip.Addr{}, 0, fmt.Errorf("localNodeID is required")
	}
	if remoteNodeID == "" {
		return netip.Addr{}, netip.Addr{}, 0, fmt.Errorf("remote nodeID is required")
	}
	bits := prefix.Bits()
	if bits > 30 {
		return netip.Addr{}, netip.Addr{}, 0, fmt.Errorf("prefix %s must contain at least one /31 pair", prefix)
	}
	pairCount := uint64(1) << uint(31-bits)
	seedA, seedB := localNodeID, remoteNodeID
	if seedB < seedA {
		seedA, seedB = seedB, seedA
	}
	start := hash64(seedA+"\x00"+seedB) % pairCount
	for offset := uint64(0); offset < pairCount; offset++ {
		index := (start + offset) % pairCount
		if used[index] {
			continue
		}
		base, err := ipv4PrefixBase(prefix)
		if err != nil {
			return netip.Addr{}, netip.Addr{}, 0, err
		}
		pairBase := base + uint32(index*2)
		first := addrFromUint32(pairBase)
		second := addrFromUint32(pairBase + 1)
		if localNodeID <= remoteNodeID {
			return first, second, index, nil
		}
		return second, first, index, nil
	}
	return netip.Addr{}, netip.Addr{}, 0, fmt.Errorf("no free /31 pair remains in %s", prefix)
}

func parseIPv4Prefix(value string) (netip.Prefix, error) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return netip.Prefix{}, err
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return netip.Prefix{}, fmt.Errorf("must be an IPv4 prefix")
	}
	return prefix, nil
}

func parseIPv4AddressOrPrefix(value string) (netip.Addr, error) {
	if prefix, err := netip.ParsePrefix(strings.TrimSpace(value)); err == nil {
		if !prefix.Addr().Is4() {
			return netip.Addr{}, fmt.Errorf("must be an IPv4 address or prefix")
		}
		return prefix.Addr(), nil
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(value))
	if err != nil {
		return netip.Addr{}, err
	}
	if !addr.Is4() {
		return netip.Addr{}, fmt.Errorf("must be an IPv4 address")
	}
	return addr, nil
}

func ipv4PrefixBase(prefix netip.Prefix) (uint32, error) {
	addr := prefix.Masked().Addr()
	if !addr.Is4() {
		return 0, fmt.Errorf("must be an IPv4 prefix")
	}
	raw := addr.As4()
	return binary.BigEndian.Uint32(raw[:]), nil
}

func addrFromUint32(value uint32) netip.Addr {
	var raw [4]byte
	binary.BigEndian.PutUint32(raw[:], value)
	return netip.AddrFrom4(raw)
}

func hash64(value string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(value))
	return h.Sum64()
}

var nameSanitizer = regexp.MustCompile(`[^a-zA-Z0-9-]+`)

func shortInterfaceName(prefix, profile, peer string) string {
	base := strings.ToLower(strings.Trim(nameSanitizer.ReplaceAllString(profile+"-"+peer, "-"), "-"))
	if base == "" {
		base = "profile"
	}
	name := prefix + "-" + base
	if len(name) <= 15 {
		return name
	}
	hash := fmt.Sprintf("%08x", uint32(hash64(profile+"\x00"+peer)))
	return prefix + "-" + hash
}

func shortResourceName(profile, peer, suffix string) string {
	parts := []string{profile, peer, suffix}
	var cleaned []string
	for _, part := range parts {
		part = strings.ToLower(strings.Trim(nameSanitizer.ReplaceAllString(part, "-"), "-"))
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	if len(cleaned) == 0 {
		return fmt.Sprintf("sam-%08x", uint32(hash64(profile+"\x00"+peer+"\x00"+suffix)))
	}
	return strings.Join(cleaned, "-")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func SortSAMTransportProfileLowerings(lowerings []SAMTransportProfileLowering) {
	sort.Slice(lowerings, func(i, j int) bool {
		if lowerings[i].ProfileName != lowerings[j].ProfileName {
			return lowerings[i].ProfileName < lowerings[j].ProfileName
		}
		return lowerings[i].PeerName < lowerings[j].PeerName
	})
}
