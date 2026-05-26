// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/api"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func writeIngressShowTable(stdout io.Writer, router *api.Router, resources []routerstate.ObjectStatus, verbose bool) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	specs := ingressServiceSpecs(router)
	var dataplane []ingressDataplaneRow
	fmt.Fprintln(w, "SERVICE\tHOSTNAME\tLISTEN\tACTIVE_BACKEND\tSELECTION\tHEALTHY/TOTAL")
	for _, resource := range resources {
		if resource.Kind != "IngressService" {
			continue
		}
		spec := specs[resource.Name]
		active := statusMap(resource.Status["activeBackend"])
		if verbose {
			dataplane = append(dataplane, observeIngressDataplane(resource.Name, spec, resource.Status))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d/%d\n",
			resource.Name,
			defaultShowString(statusString(resource.Status["hostname"]), "-"),
			ingressListenString(spec, resource.Status),
			activeBackendString(active),
			defaultShowString(statusString(resource.Status["selection"]), "failover"),
			statusInt(resource.Status["healthyBackends"]),
			statusInt(resource.Status["totalBackends"]),
		)
		backends := statusMaps(resource.Status["backends"])
		if len(backends) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "BACKEND\tADDRESS\tSTATE\tDRAINED_UNTIL\tLAST_HEALTHY\tLAST_UNHEALTHY")
			for _, backend := range backends {
				state := "Unhealthy"
				if statusBool(backend["healthy"]) {
					state = "Healthy"
				}
				if statusBool(backend["drained"]) {
					state = "Drained"
				}
				fmt.Fprintf(w, "%s\t%s\t%s(%d/%d)\t%s\t%s\t%s\n",
					statusString(backend["name"]),
					backendAddressString(backend),
					state,
					statusInt(backend["healthyCount"]),
					statusInt(backend["unhealthyCount"]),
					defaultShowString(statusString(backend["drainedUntil"]), "-"),
					ageString(statusString(backend["lastHealthyAt"])),
					ageString(statusString(backend["lastUnhealthyAt"])),
				)
			}
		}
	}
	if verbose && len(dataplane) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "DATAPLANE\tIPV4_FORWARD\tIPV6_FORWARD\tNFT_DNAT\tNFT_SNAT\tCONNTRACK\tDETAIL")
		for _, row := range dataplane {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				row.Service,
				row.IPv4Forward,
				row.IPv6Forward,
				row.NFTDNAT,
				row.NFTSNAT,
				row.Conntrack,
				defaultShowString(row.Detail, "-"),
			)
		}
	}
	return w.Flush()
}

type ingressDataplaneRow struct {
	Service     string
	IPv4Forward string
	IPv6Forward string
	NFTDNAT     int
	NFTSNAT     int
	Conntrack   string
	Detail      string
}

func observeIngressDataplane(name string, spec api.IngressServiceSpec, status map[string]any) ingressDataplaneRow {
	row := ingressDataplaneRow{
		Service:     name,
		IPv4Forward: dataplaneSysctlValue("net.ipv4.ip_forward"),
		IPv6Forward: dataplaneSysctlValue("net.ipv6.conf.all.forwarding"),
	}
	nftDNAT, nftSNAT, nftDetail := ingressNFTRuleCounts(name)
	row.NFTDNAT = nftDNAT
	row.NFTSNAT = nftSNAT
	if nftDetail != "" {
		row.appendDetail(nftDetail)
	}
	if hairpinDetail := ingressHairpinDataplaneDetail(spec, status, nftSNAT); hairpinDetail != "" {
		row.appendDetail(hairpinDetail)
	}
	count, detail := ingressConntrackCount(spec, status)
	row.Conntrack = count
	if detail != "" {
		row.appendDetail(detail)
	}
	return row
}

func (r *ingressDataplaneRow) appendDetail(detail string) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	if r.Detail != "" {
		r.Detail += "; "
	}
	r.Detail += detail
}

func dataplaneSysctlValue(key string) string {
	out, err := runDataplaneCommand("sysctl", "-n", key)
	if err != nil {
		return "unavailable"
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "unknown"
	}
	return value
}

func ingressNFTRuleCounts(name string) (int, int, string) {
	out, err := runDataplaneCommand("nft", "-a", "list", "table", "ip", "routerd_nat")
	if err != nil {
		return 0, 0, "nft unavailable"
	}
	needle := "routerd IngressService " + name
	var dnat, snat int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if strings.Contains(line, "dnat to") || strings.Contains(line, " vmap ") {
			dnat++
		}
		if strings.Contains(line, " masquerade") || strings.Contains(line, " snat ") {
			snat++
		}
	}
	return dnat, snat, ""
}

func ingressHairpinDataplaneDetail(spec api.IngressServiceSpec, status map[string]any, nftSNAT int) string {
	mode := strings.TrimSpace(spec.Hairpin.Mode)
	if mode == "" {
		mode = "auto"
	}
	required := false
	switch mode {
	case "off":
		required = false
	case "manual":
		required = spec.Hairpin.Enabled || len(spec.Hairpin.Interfaces) > 0
	case "auto":
		required = ingressShowAutoHairpinRequired(spec, status)
	default:
		return "hairpinMode=" + mode
	}
	state := "nft_snat=not-required"
	if required && nftSNAT == 0 {
		state = "nft_snat=missing"
	} else if required {
		state = "nft_snat=present"
	} else if nftSNAT > 0 {
		state = "nft_snat=present"
	}
	return fmt.Sprintf("hairpinMode=%s hairpinRequired=%t %s", mode, required, state)
}

func ingressShowAutoHairpinRequired(spec api.IngressServiceSpec, status map[string]any) bool {
	listen := statusString(status["listenAddress"])
	if listen == "" {
		listen = spec.Listen.Address
	}
	listenAddr, err := netip.ParseAddr(strings.TrimSpace(listen))
	if err != nil || !listenAddr.Is4() {
		return false
	}
	for _, backend := range ingressShowBackendAddresses(spec, status) {
		addr, err := netip.ParseAddr(backend)
		if err == nil && addr.Is4() && ingressShowSamePrivateIPv4Slash24(listenAddr, addr) {
			return true
		}
	}
	return false
}

func ingressShowBackendAddresses(spec api.IngressServiceSpec, status map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, backend := range statusMaps(status["backends"]) {
		add(statusString(backend["resolvedAddress"]))
		if address := statusString(backend["address"]); net.ParseIP(address) != nil {
			add(address)
		}
	}
	if active := statusMap(status["activeBackend"]); len(active) > 0 {
		add(statusString(active["resolvedAddress"]))
		if address := statusString(active["address"]); net.ParseIP(address) != nil {
			add(address)
		}
	}
	for _, backend := range spec.Backends {
		if net.ParseIP(backend.Address) != nil {
			add(backend.Address)
		}
	}
	return out
}

func ingressShowSamePrivateIPv4Slash24(a, b netip.Addr) bool {
	if !a.Is4() || !b.Is4() || !a.IsPrivate() || !b.IsPrivate() {
		return false
	}
	return netip.PrefixFrom(a, 24).Contains(b)
}

func ingressConntrackCount(spec api.IngressServiceSpec, status map[string]any) (string, string) {
	out, err := runDataplaneCommand("conntrack", "-L")
	if err != nil {
		return "unavailable", "conntrack unavailable"
	}
	needles := ingressConntrackNeedles(spec, status)
	if len(needles) == 0 {
		return "unknown", "conntrack no match keys"
	}
	var count int
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				count++
				break
			}
		}
	}
	return strconv.Itoa(count), ""
}

func ingressConntrackNeedles(spec api.IngressServiceSpec, status map[string]any) []string {
	seen := map[string]bool{}
	var needles []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		needles = append(needles, value)
	}
	if listen := statusString(status["listenAddress"]); listen != "" {
		add("dst=" + listen)
		add("reply_src=" + listen)
	} else if spec.Listen.Address != "" {
		add("dst=" + spec.Listen.Address)
		add("reply_src=" + spec.Listen.Address)
	}
	for _, backend := range statusMaps(status["backends"]) {
		if resolved := statusString(backend["resolvedAddress"]); resolved != "" {
			add("dst=" + resolved)
			add("src=" + resolved)
			continue
		}
		if address := statusString(backend["address"]); net.ParseIP(address) != nil {
			add("dst=" + address)
			add("src=" + address)
		}
	}
	for _, backend := range spec.Backends {
		if net.ParseIP(backend.Address) != nil {
			add("dst=" + backend.Address)
			add("src=" + backend.Address)
		}
	}
	return needles
}

func runDataplaneCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, hostcmd.Resolve(name), args...).CombinedOutput()
}

func ingressServiceSpecs(router *api.Router) map[string]api.IngressServiceSpec {
	out := map[string]api.IngressServiceSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IngressService" {
			continue
		}
		spec, err := resource.IngressServiceSpec()
		if err == nil {
			out[resource.Metadata.Name] = spec
		}
	}
	return out
}

func establishedSummary(status map[string]any, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", statusInt(status["establishedPeers"]), total)
}

func enabledString(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func boolShow(value any) string {
	if value == nil {
		return "-"
	}
	if value == true {
		return "yes"
	}
	if value == false {
		return "no"
	}
	if text := statusString(value); text != "" {
		return text
	}
	return "-"
}

func ingressListenString(spec api.IngressServiceSpec, status map[string]any) string {
	address := statusString(status["listenAddress"])
	if address == "" {
		address = spec.Listen.Address
	}
	if address == "" && spec.Listen.AddressFrom.Resource != "" {
		address = spec.Listen.AddressFrom.Resource
	}
	return fmt.Sprintf("%s:%s:%d", spec.Listen.Interface, defaultShowString(address, "*"), spec.Listen.Port)
}

func activeBackendString(active map[string]any) string {
	name := statusString(active["name"])
	address := statusString(active["address"])
	port := statusInt(active["port"])
	if name == "" && address == "" {
		return "-"
	}
	if port > 0 {
		return fmt.Sprintf("%s/%s:%d", defaultShowString(name, "-"), address, port)
	}
	return fmt.Sprintf("%s/%s", defaultShowString(name, "-"), address)
}

func backendAddressString(backend map[string]any) string {
	address := statusString(backend["address"])
	resolved := statusString(backend["resolvedAddress"])
	port := statusInt(backend["port"])
	if resolved != "" && resolved != address {
		return fmt.Sprintf("%s -> %s:%d", address, resolved, port)
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", defaultShowString(resolved, address), port)
	}
	return defaultShowString(resolved, address)
}

func ageString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return humanDuration(time.Since(ts))
	}
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil && ts > 0 {
		return humanDuration(time.Since(time.Unix(ts, 0)))
	}
	return value
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm%ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

func statusMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func statusMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func statusString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case uint:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func statusBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultShowString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
