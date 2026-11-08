// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"io"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
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
			state := defaultShowString(statusvalue.Text(peer["state"]), "unknown")
			up := "-"
			if strings.EqualFold(state, "Established") {
				up = ageString(statusvalue.Text(peer["lastEstablishedAt"]))
			}
			lastError := defaultShowString(statusvalue.Text(peer["lastErrorReason"]), "-")
			if strings.EqualFold(state, "Established") {
				lastError = "-"
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%d\t%d\t%s\n",
				statusvalue.Text(peer["address"]),
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
				statusvalue.Text(prefix["prefix"]),
				statusvalue.Text(prefix["_router"]),
				boolShow(prefix["best"]),
				boolShow(prefix["valid"]),
				boolShow(prefix["installed"]),
				defaultShowString(statusvalue.Text(prefix["selectionState"]), "-"),
				defaultShowString(statusvalue.Text(prefix["selectionReason"]), "-"),
			)
		}
	}
	return w.Flush()
}

func withLiveBGPState(router *api.Router, resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	return resources
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
		if defaultShowString(spec.Mode, "static") != "vrrp" && statusvalue.Text(resource.Status["virtualRouterID"]) == "" {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\n",
			statusvalue.Text(resource.Status["address"]),
			defaultShowString(statusvalue.Text(resource.Status["hostname"]), "-"),
			defaultShowString(statusvalue.Text(resource.Status["role"]), "unknown"),
			statusInt(resource.Status["priority"]),
			statusInt(resource.Status["basePriority"]),
			defaultShowString(statusvalue.Text(resource.Status["interface"]), spec.Interface),
			statusInt(resource.Status["virtualRouterID"]),
			strings.Join(spec.Peers, ","),
			ageString(statusvalue.Text(resource.Status["lastRoleTransitionAt"])),
		)
		tracks := statusMaps(resource.Status["track"])
		if len(tracks) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "TRACK\tSTATE\tPENALTY\tDETAIL")
			for _, track := range tracks {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s/%s unhealthy=%d\n",
					statusvalue.Text(track["resource"]),
					statusvalue.Text(track["state"]),
					statusInt(track["penalty"]),
					statusvalue.Text(track["unhealthyCount"]),
					statusvalue.Text(track["confirmConsecutiveUnhealthy"]),
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
		if previous := statusvalue.Text(status["role"]); previous != role {
			status["role"] = role
			status["lastRoleTransitionAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		out[i].Status = status
	}
	return out
}

func liveVRRPRole(status map[string]any, spec virtualAddressShowSpec, aliases map[string]string) (string, bool) {
	ifname := statusvalue.Text(status["ifname"])
	if ifname == "" {
		ifname = aliases[spec.Interface]
	}
	address := statusvalue.Text(status["address"])
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
