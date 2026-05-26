// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func writeBGPShowTable(stdout io.Writer, router *api.Router, resources []routerstate.ObjectStatus) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	specs := bgpRouterSpecs(router)
	fmt.Fprintln(w, "ROUTER\tASN\tROUTER_ID\tPEERS_ESTABLISHED\tPREFIXES_ACCEPTED\tGR")
	var peers []map[string]any
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		spec := specs[resource.Name]
		totalPeers := len(statusMaps(resource.Status["peers"]))
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%s\n",
			resource.Name,
			spec.ASN,
			spec.RouterID,
			establishedSummary(resource.Status, totalPeers),
			statusInt(resource.Status["acceptedPrefixes"]),
			enabledString(api.BoolDefault(spec.GracefulRestart.Enabled, true)),
		)
		for _, peer := range statusMaps(resource.Status["peers"]) {
			peer["_router"] = resource.Name
			peers = append(peers, peer)
		}
	}
	if len(peers) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "PEER\tAS\tSTATE\tUP\tRCVD\tSENT\tPFX\tLAST_ERROR")
		for _, peer := range peers {
			state := defaultShowString(statusString(peer["state"]), "unknown")
			up := "-"
			if strings.EqualFold(state, "Established") {
				up = ageString(statusString(peer["lastEstablishedAt"]))
			}
			lastError := defaultShowString(statusString(peer["lastErrorReason"]), "-")
			if strings.EqualFold(state, "Established") {
				lastError = "-"
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%d\t%d\t%s\n",
				statusString(peer["address"]),
				statusInt(peer["asn"]),
				state,
				up,
				statusInt(peer["messagesReceived"]),
				statusInt(peer["messagesSent"]),
				statusInt(peer["prefixesReceived"]),
				lastError,
			)
		}
	}
	var prefixes []map[string]any
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		for _, prefix := range statusMaps(resource.Status["prefixes"]) {
			prefix["_router"] = resource.Name
			prefixes = append(prefixes, prefix)
		}
	}
	if len(prefixes) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "PREFIX\tROUTER\tBEST\tVALID\tINSTALLED\tSTATE\tREASON")
		for _, prefix := range prefixes {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				statusString(prefix["prefix"]),
				statusString(prefix["_router"]),
				boolShow(prefix["best"]),
				boolShow(prefix["valid"]),
				boolShow(prefix["installed"]),
				defaultShowString(statusString(prefix["selectionState"]), "-"),
				defaultShowString(statusString(prefix["selectionReason"]), "-"),
			)
		}
	}
	return w.Flush()
}

func withLiveBGPState(router *api.Router, resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	return resources
}

func liveBGPStatuses(_ *api.Router) map[string]map[string]any {
	// BGP is now embedded in routerd through GoBGP. routerctl must not probe
	// external BGP CLIs; live status is written by the daemon controller itself.
	return map[string]map[string]any{}
}

func mergeBGPStatus(current, live map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range current {
		out[key] = value
	}
	livePeers := statusMaps(live["peers"])
	currentPeers := statusMaps(current["peers"])
	if len(livePeers) > 0 || len(currentPeers) == 0 || statusInt(current["establishedPeers"]) == 0 {
		for key, value := range live {
			out[key] = value
		}
	}
	return out
}

func bgpPeersStatusMaps(peers []bgpstate.Peer) []map[string]any {
	out := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		out = append(out, map[string]any{
			"address":           peer.Address,
			"asn":               peer.ASN,
			"state":             peer.State,
			"established":       peer.Established,
			"prefixesReceived":  peer.PrefixesReceived,
			"messagesReceived":  peer.MessagesReceived,
			"messagesSent":      peer.MessagesSent,
			"lastEstablishedAt": peer.LastEstablishedAt,
			"lastErrorAt":       peer.LastErrorAt,
			"lastErrorReason":   peer.LastErrorReason,
		})
	}
	return out
}

func bgpPrefixesStatusMaps(prefixes []bgpstate.Prefix) []map[string]any {
	out := make([]map[string]any, 0, len(prefixes))
	for _, prefix := range prefixes {
		out = append(out, map[string]any{
			"prefix":          prefix.Prefix,
			"best":            prefix.Best,
			"valid":           prefix.Valid,
			"installed":       prefix.Installed,
			"selected":        prefix.Selected,
			"stale":           prefix.Stale,
			"selectDeferred":  prefix.SelectDeferred,
			"selectionState":  prefix.SelectionState,
			"selectionReason": prefix.SelectionReason,
			"communities":     prefix.Communities,
		})
	}
	return out
}

func hasBGPResources(router *api.Router) bool {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && (resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer") {
			return true
		}
	}
	return false
}

func writeVRRPShowTable(stdout io.Writer, router *api.Router, resources []routerstate.ObjectStatus) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	specs := virtualAddressShowSpecs(router)
	fmt.Fprintln(w, "VIP\tHOSTNAME\tROLE\tPRIORITY\tBASE\tIFACE\tVRID\tPEERS\tLAST_TRANSITION")
	for _, resource := range resources {
		if resource.Kind != "VirtualAddress" {
			continue
		}
		spec := specs[resource.Name]
		if defaultShowString(spec.Mode, "static") != "vrrp" && statusString(resource.Status["virtualRouterID"]) == "" {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\n",
			statusString(resource.Status["address"]),
			defaultShowString(statusString(resource.Status["hostname"]), "-"),
			defaultShowString(statusString(resource.Status["role"]), "unknown"),
			statusInt(resource.Status["priority"]),
			statusInt(resource.Status["basePriority"]),
			defaultShowString(statusString(resource.Status["interface"]), spec.Interface),
			statusInt(resource.Status["virtualRouterID"]),
			strings.Join(spec.Peers, ","),
			ageString(statusString(resource.Status["lastRoleTransitionAt"])),
		)
		tracks := statusMaps(resource.Status["track"])
		if len(tracks) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "TRACK\tSTATE\tPENALTY\tDETAIL")
			for _, track := range tracks {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s/%s unhealthy=%d\n",
					statusString(track["resource"]),
					statusString(track["state"]),
					statusInt(track["penalty"]),
					statusString(track["unhealthyCount"]),
					statusString(track["confirmConsecutiveUnhealthy"]),
					statusInt(track["unhealthyConsecutive"]),
				)
			}
		}
	}
	return w.Flush()
}

func bgpRouterSpecs(router *api.Router) map[string]api.BGPRouterSpec {
	out := map[string]api.BGPRouterSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err == nil {
			out[resource.Metadata.Name] = spec
		}
	}
	return out
}

type virtualAddressShowSpec struct {
	Interface string
	Address   string
	Family    string
	Mode      string
	Peers     []string
}

func virtualAddressShowSpecs(router *api.Router) map[string]virtualAddressShowSpec {
	out := map[string]virtualAddressShowSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "VirtualAddress":
			spec, err := resource.VirtualAddressSpec()
			if err == nil {
				out[resource.Metadata.Name] = virtualAddressShowSpec{Interface: spec.Interface, Address: spec.Address, Family: spec.Family, Mode: spec.Mode, Peers: spec.VRRP.Peers}
			}
		}
	}
	return out
}

func routerctlPeersForRouter(router *api.Router, routerName string, state bgpstate.State) []bgpstate.Peer {
	byAddress := map[string]bgpstate.Peer{}
	for _, peer := range state.Peers {
		byAddress[peer.Address] = peer
	}
	var out []bgpstate.Peer
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !ok || name != routerName {
			continue
		}
		for _, address := range spec.Peers {
			peer, ok := byAddress[strings.TrimSpace(address)]
			if !ok {
				peer = bgpstate.Peer{Address: strings.TrimSpace(address), ASN: spec.PeerASN, State: "Missing"}
			} else if peer.ASN == 0 {
				peer.ASN = spec.PeerASN
			}
			out = append(out, peer)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func routerctlBGPVRFNames(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VRF" {
			continue
		}
		spec, err := resource.VRFSpec()
		if err != nil {
			continue
		}
		out[resource.Metadata.Name] = defaultString(spec.IfName, resource.Metadata.Name)
	}
	return out
}

func routerctlBGPVRFRefName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if kind, name, ok := strings.Cut(value, "/"); ok && kind == "VRF" {
		return strings.TrimSpace(name)
	}
	return value
}

func routerctlBGPRouterUsesIPv6(router *api.Router, routerName string, spec api.BGPRouterSpec) bool {
	prefixes := append([]string{}, spec.ImportPolicy.AllowedPrefixes...)
	prefixes = append(prefixes, spec.ExportPolicy.AllowedPrefixes...)
	prefixes = append(prefixes, spec.Redistribute.Connected.AllowedPrefixes...)
	prefixes = append(prefixes, spec.Redistribute.Static.AllowedPrefixes...)
	for _, prefix := range prefixes {
		if parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err == nil && parsed.Addr().Is6() {
			return true
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		peerSpec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(peerSpec.RouterRef), "/")
		if !ok || name != routerName {
			continue
		}
		for _, address := range peerSpec.Peers {
			if parsed, err := netip.ParseAddr(strings.TrimSpace(address)); err == nil && parsed.Is6() {
				return true
			}
		}
	}
	return false
}

func routerctlBGPShowCommands(vrfName string) (string, string) {
	if strings.TrimSpace(vrfName) == "" {
		return "show bgp summary json", "show bgp ipv4 unicast json"
	}
	vrfName = strings.TrimSpace(vrfName)
	return "show bgp vrf " + vrfName + " summary json", "show bgp vrf " + vrfName + " ipv4 unicast json"
}

func routerctlBGPShowIPv6RoutesCommand(vrfName string) string {
	if strings.TrimSpace(vrfName) == "" {
		return "show bgp ipv6 unicast json"
	}
	return "show bgp vrf " + strings.TrimSpace(vrfName) + " ipv6 unicast json"
}

func withLiveVRRPRoles(router *api.Router, resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	if router == nil {
		return resources
	}
	specs := virtualAddressShowSpecs(router)
	aliases := interfaceAliases(router.Spec.Resources)
	out := make([]routerstate.ObjectStatus, len(resources))
	copy(out, resources)
	for i := range out {
		if out[i].Kind != "VirtualAddress" {
			continue
		}
		spec := specs[out[i].Name]
		if defaultShowString(spec.Mode, "static") != "vrrp" {
			continue
		}
		role, ok := liveVRRPRole(out[i].Status, spec, aliases)
		if !ok {
			continue
		}
		status := map[string]any{}
		for key, value := range out[i].Status {
			status[key] = value
		}
		if previous := statusString(status["role"]); previous != role {
			status["role"] = role
			status["lastRoleTransitionAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		out[i].Status = status
	}
	return out
}

func liveVRRPRole(status map[string]any, spec virtualAddressShowSpec, aliases map[string]string) (string, bool) {
	ifname := statusString(status["ifname"])
	if ifname == "" {
		ifname = aliases[spec.Interface]
	}
	address := statusString(status["address"])
	if address == "" {
		address = spec.Address
	}
	if strings.TrimSpace(ifname) == "" || strings.TrimSpace(address) == "" {
		return "", false
	}
	family := spec.Family
	if family == "" {
		family = "ipv4"
		if strings.Contains(address, ":") {
			family = "ipv6"
		}
	}
	ipFamily := "-4"
	if family == "ipv6" {
		ipFamily = "-6"
	}
	out, err := exec.Command("ip", ipFamily, "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", false
	}
	if ipOutputHasAddress(string(out), address, family) {
		return "master", true
	}
	return "backup", true
}

func ipOutputHasAddress(output, address, family string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		addr, addrErr := netip.ParseAddr(strings.TrimSpace(address))
		if addrErr != nil {
			return false
		}
		bits := 32
		if family == "ipv6" {
			bits = 128
		}
		prefix = netip.PrefixFrom(addr, bits)
	}
	token := "inet "
	if family == "ipv6" {
		token = "inet6 "
	}
	needle := token + prefix.Addr().String() + "/" + strconv.Itoa(prefix.Bits())
	return strings.Contains(output, needle)
}
