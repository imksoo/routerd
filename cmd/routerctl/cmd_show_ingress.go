// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
)

type ingressDataplaneRow struct {
	Service     string
	IPv4Forward string
	IPv6Forward string
	NFTDNAT     int
	NFTSNAT     int
	Conntrack   string
	Detail      string
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
	listen := statusvalue.Text(status["listenAddress"])
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
		add(statusvalue.Text(backend["resolvedAddress"]))
		if address := statusvalue.Text(backend["address"]); net.ParseIP(address) != nil {
			add(address)
		}
	}
	if active := statusMap(status["activeBackend"]); len(active) > 0 {
		add(statusvalue.Text(active["resolvedAddress"]))
		if address := statusvalue.Text(active["address"]); net.ParseIP(address) != nil {
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
	if listen := statusvalue.Text(status["listenAddress"]); listen != "" {
		add("dst=" + listen)
		add("reply_src=" + listen)
	} else if spec.Listen.Address != "" {
		add("dst=" + spec.Listen.Address)
		add("reply_src=" + spec.Listen.Address)
	}
	for _, backend := range statusMaps(status["backends"]) {
		if resolved := statusvalue.Text(backend["resolvedAddress"]); resolved != "" {
			add("dst=" + resolved)
			add("src=" + resolved)
			continue
		}
		if address := statusvalue.Text(backend["address"]); net.ParseIP(address) != nil {
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

func defaultShowString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
