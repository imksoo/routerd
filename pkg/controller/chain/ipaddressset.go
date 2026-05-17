// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/miekg/dns"

	"routerd/pkg/api"
	"routerd/pkg/render"
)

type IPAddressSetController struct {
	Router         *api.Router
	Store          Store
	DryRun         bool
	DryRunNAT      bool
	DryRunRoute    bool
	DryRunFirewall bool
	NftCommand     string
	RuntimeDir     string
	Command        outputCommandFunc
	Resolver       ipAddressSetResolver
	Now            func() time.Time
}

type ipAddressSetResolver interface {
	ResolveIP(context.Context, string) ([]ipAddressSetRecord, error)
}

type ipAddressSetRecord struct {
	Address string
	TTL     time.Duration
}

func (c IPAddressSetController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	now := time.Now().UTC()
	if c.Now != nil {
		now = c.Now().UTC()
	}
	resolver := c.Resolver
	if resolver == nil {
		resolver = systemDNSResolver{}
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	referenced := referencedIPAddressSetTargets(c.Router)
resources:
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "IPAddressSet" {
			continue
		}
		spec, err := resource.IPAddressSetSpec()
		if err != nil {
			return err
		}
		setName := render.NftIPAddressSetName(resource.Metadata.Name)
		targets := referenced[resource.Metadata.Name]
		activeTargets := c.activeIPAddressSetTargets(targets)
		if len(targets) == 0 {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
				"phase":      "Observed",
				"referenced": false,
			}); err != nil {
				return err
			}
			continue
		}
		existingTargets := map[string]ipAddressSetTarget{}
		existingOutputs := map[string]string{}
		if len(activeTargets) > 0 {
			nft := firstNonEmpty(c.NftCommand, "nft")
			for _, target := range activeTargets {
				if exists, output := nftSetSnapshot(ctx, command, nft, target.TableFamily, target.Table, target.SetName); exists {
					existingTargets[target.key()] = target
					existingOutputs[target.key()] = output
				}
			}
		}
		current := c.Store.ObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name)
		if len(activeTargets) > 0 && len(existingTargets) > 0 && !ipAddressSetRefreshDue(current, now) {
			if cached, ok := cachedIPAddressSetResult(current); ok && ipAddressSetTargetsCurrent(cached, activeTargets, existingOutputs) {
				continue
			}
		}
		result, err := resolveIPAddressSet(ctx, resolver, spec)
		if err != nil {
			if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
				"phase":      "Pending",
				"reason":     "ResolveFailed",
				"error":      err.Error(),
				"referenced": true,
				"setName":    setName,
				"dryRun":     len(activeTargets) == 0,
			}); saveErr != nil {
				return saveErr
			}
			continue
		}
		nextRefresh := now.Add(nextIPAddressSetRefresh(result.MinTTL, spec.RefreshInterval))
		if len(activeTargets) > 0 {
			missingTargets := missingIPAddressSetTargets(result, activeTargets, existingTargets)
			if len(missingTargets) > 0 {
				if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
					"phase":          "Pending",
					"reason":         "SetMissing",
					"message":        "nftables set is not present yet; renderer must create it before runtime refresh",
					"referenced":     true,
					"setName":        setName,
					"addresses":      result.Addresses,
					"ipv4Addresses":  result.IPv4Addresses,
					"ipv6Addresses":  result.IPv6Addresses,
					"missingTargets": missingTargets,
					"names":          result.Names,
					"resolveErrors":  result.Errors,
					"nextRefreshAt":  nextRefresh.Format(time.RFC3339Nano),
					"dryRun":         false,
				}); saveErr != nil {
					return saveErr
				}
				continue
			}
			for _, target := range sortedIPAddressSetTargets(existingTargets) {
				if err := c.applyNftSet(ctx, command, target.TableFamily, target.Table, target.SetName, result.addressesForFamily(target.AddressFamily)); err != nil {
					if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
						"phase":      "Error",
						"reason":     "ApplyFailed",
						"error":      err.Error(),
						"referenced": true,
						"setName":    setName,
						"target":     target.String(),
						"addresses":  result.Addresses,
						"dryRun":     false,
					}); saveErr != nil {
						return saveErr
					}
					continue resources
				}
			}
			if len(existingTargets) == 0 {
				if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
					"phase":      "Error",
					"reason":     "ApplyFailed",
					"error":      "no nftables address set is present",
					"referenced": true,
					"setName":    setName,
					"addresses":  result.Addresses,
					"dryRun":     false,
				}); saveErr != nil {
					return saveErr
				}
				continue
			}
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "IPAddressSet", resource.Metadata.Name, map[string]any{
			"phase":             "Applied",
			"referenced":        true,
			"setName":           setName,
			"addresses":         result.Addresses,
			"ipv4Addresses":     result.IPv4Addresses,
			"ipv6Addresses":     result.IPv6Addresses,
			"targets":           appliedIPAddressSetTargets(existingTargets, targets, len(activeTargets) == 0),
			"dryRunTargets":     dryRunIPAddressSetTargets(targets, c),
			"names":             result.Names,
			"minTTLSeconds":     int(result.MinTTL.Seconds()),
			"nextRefreshAt":     nextRefresh.Format(time.RFC3339Nano),
			"refreshInterval":   spec.RefreshInterval,
			"resolveErrors":     result.Errors,
			"resolvedAt":        now.Format(time.RFC3339Nano),
			"dryRun":            len(activeTargets) == 0,
			"runtimeController": len(activeTargets) > 0,
		}); err != nil {
			return err
		}
	}
	return nil
}

type ipAddressSetResolveResult struct {
	Addresses     []string
	IPv4Addresses []string
	IPv6Addresses []string
	Names         []string
	MinTTL        time.Duration
	Errors        []string
}

func (r ipAddressSetResolveResult) addressesForFamily(family string) []string {
	switch family {
	case "ip":
		return r.IPv4Addresses
	case "ip6":
		return r.IPv6Addresses
	default:
		return nil
	}
}

func resolveIPAddressSet(ctx context.Context, resolver ipAddressSetResolver, spec api.IPAddressSetSpec) (ipAddressSetResolveResult, error) {
	seen := map[string]bool{}
	var addresses []string
	var ipv4Addresses []string
	var ipv6Addresses []string
	for _, value := range spec.Addresses {
		addr, err := netip.ParseAddr(strings.TrimSpace(value))
		if err != nil {
			return ipAddressSetResolveResult{}, fmt.Errorf("invalid IP address %q", value)
		}
		addr = addr.Unmap()
		key := addr.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		addresses = append(addresses, key)
		if addr.Is4() {
			ipv4Addresses = append(ipv4Addresses, key)
		} else if addr.Is6() {
			ipv6Addresses = append(ipv6Addresses, key)
		}
	}
	var names []string
	var minTTL time.Duration
	var errors []string
	for _, name := range spec.Names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		names = append(names, name)
		records, err := resolver.ResolveIP(ctx, name)
		if err != nil {
			errors = append(errors, err.Error())
			continue
		}
		for _, record := range records {
			addr, err := netip.ParseAddr(record.Address)
			if err != nil {
				continue
			}
			addr = addr.Unmap()
			key := addr.String()
			if !seen[key] {
				seen[key] = true
				addresses = append(addresses, key)
				if addr.Is4() {
					ipv4Addresses = append(ipv4Addresses, key)
				} else if addr.Is6() {
					ipv6Addresses = append(ipv6Addresses, key)
				}
			}
			if record.TTL > 0 && (minTTL == 0 || record.TTL < minTTL) {
				minTTL = record.TTL
			}
		}
	}
	if len(addresses) == 0 {
		if len(errors) > 0 {
			return ipAddressSetResolveResult{}, fmt.Errorf("no IP addresses resolved: %s", strings.Join(errors, "; "))
		}
		return ipAddressSetResolveResult{}, fmt.Errorf("no IP addresses configured or resolved")
	}
	if minTTL == 0 {
		minTTL = 5 * time.Minute
	}
	sort.Strings(addresses)
	sort.Strings(ipv4Addresses)
	sort.Strings(ipv6Addresses)
	sort.Strings(names)
	sort.Strings(errors)
	return ipAddressSetResolveResult{Addresses: addresses, IPv4Addresses: ipv4Addresses, IPv6Addresses: ipv6Addresses, Names: names, MinTTL: minTTL, Errors: errors}, nil
}

func nextIPAddressSetRefresh(minTTL time.Duration, refreshInterval string) time.Duration {
	if minTTL <= 0 {
		minTTL = 5 * time.Minute
	}
	next := minTTL / 2
	if next < time.Minute {
		next = time.Minute
	}
	if refreshInterval != "" {
		if configured, err := time.ParseDuration(refreshInterval); err == nil && configured > 0 && configured < next {
			next = configured
		}
	}
	return next
}

func ipAddressSetRefreshDue(status map[string]any, now time.Time) bool {
	if len(status) == 0 {
		return true
	}
	next, ok := status["nextRefreshAt"].(string)
	if !ok || strings.TrimSpace(next) == "" {
		return true
	}
	t, err := time.Parse(time.RFC3339Nano, next)
	if err != nil {
		return true
	}
	return !now.Before(t)
}

func (c IPAddressSetController) applyNftSet(ctx context.Context, command outputCommandFunc, family, table, setName string, addresses []string) error {
	dir := firstNonEmpty(c.RuntimeDir, os.TempDir())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".routerd-ip-address-set-*.nft")
	if err != nil {
		return err
	}
	path := file.Name()
	defer os.Remove(path)
	var b strings.Builder
	b.WriteString("flush set " + family + " " + table + " " + setName + "\n")
	if len(addresses) > 0 {
		b.WriteString("add element " + family + " " + table + " " + setName + " { " + strings.Join(addresses, ", ") + " }\n")
	}
	if _, err := file.WriteString(b.String()); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	nft := firstNonEmpty(c.NftCommand, "nft")
	if out, err := command(ctx, nft, "-f", path); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func nftSetSnapshot(ctx context.Context, command outputCommandFunc, nft, family, table, setName string) (bool, string) {
	out, err := command(ctx, nft, "list", "set", family, table, setName)
	return err == nil, string(out)
}

type ipAddressSetTarget struct {
	TableFamily   string
	AddressFamily string
	Table         string
	SetName       string
	Controller    string
}

func (t ipAddressSetTarget) key() string {
	return t.TableFamily + "/" + t.Table + "/" + t.SetName
}

func (t ipAddressSetTarget) String() string {
	return t.key()
}

func missingIPAddressSetTargets(result ipAddressSetResolveResult, targets []ipAddressSetTarget, existing map[string]ipAddressSetTarget) []string {
	var missing []string
	for _, target := range targets {
		if len(result.addressesForFamily(target.AddressFamily)) == 0 {
			continue
		}
		if _, ok := existing[target.key()]; !ok {
			missing = append(missing, target.String())
		}
	}
	sort.Strings(missing)
	return missing
}

func appliedIPAddressSetTargets(existing map[string]ipAddressSetTarget, desired []ipAddressSetTarget, dryRun bool) []string {
	if dryRun {
		out := make([]string, 0, len(desired))
		for _, target := range desired {
			out = append(out, target.String())
		}
		sort.Strings(out)
		return out
	}
	targets := sortedIPAddressSetTargets(existing)
	out := make([]string, 0, len(targets))
	for _, target := range targets {
		out = append(out, target.String())
	}
	return out
}

func dryRunIPAddressSetTargets(targets []ipAddressSetTarget, c IPAddressSetController) []string {
	var out []string
	for _, target := range targets {
		if c.targetDryRun(target) {
			out = append(out, target.String())
		}
	}
	sort.Strings(out)
	return out
}

func (c IPAddressSetController) activeIPAddressSetTargets(targets []ipAddressSetTarget) []ipAddressSetTarget {
	var out []ipAddressSetTarget
	for _, target := range targets {
		if !c.targetDryRun(target) {
			out = append(out, target)
		}
	}
	return out
}

func (c IPAddressSetController) targetDryRun(target ipAddressSetTarget) bool {
	if c.DryRun {
		return true
	}
	switch target.Controller {
	case "nat":
		return c.DryRunNAT
	case "route":
		return c.DryRunRoute
	case "firewall":
		return c.DryRunFirewall
	default:
		return false
	}
}

func cachedIPAddressSetResult(status map[string]any) (ipAddressSetResolveResult, bool) {
	result := ipAddressSetResolveResult{
		Addresses:     statusStringSlice(status["addresses"]),
		IPv4Addresses: statusStringSlice(status["ipv4Addresses"]),
		IPv6Addresses: statusStringSlice(status["ipv6Addresses"]),
		Names:         statusStringSlice(status["names"]),
		Errors:        statusStringSlice(status["resolveErrors"]),
	}
	if len(result.Addresses) == 0 && len(result.IPv4Addresses) == 0 && len(result.IPv6Addresses) == 0 {
		return ipAddressSetResolveResult{}, false
	}
	return result, true
}

func statusStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		out := append([]string(nil), typed...)
		sort.Strings(out)
		return out
	case []any:
		var out []string
		for _, item := range typed {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		sort.Strings(out)
		return out
	default:
		return nil
	}
}

func ipAddressSetTargetsCurrent(cached ipAddressSetResolveResult, targets []ipAddressSetTarget, outputs map[string]string) bool {
	for _, target := range targets {
		addresses := cached.addressesForFamily(target.AddressFamily)
		if len(addresses) == 0 {
			continue
		}
		output, ok := outputs[target.key()]
		if !ok {
			return false
		}
		for _, address := range addresses {
			if !strings.Contains(output, address) {
				return false
			}
		}
	}
	return true
}

func sortedIPAddressSetTargets(targets map[string]ipAddressSetTarget) []ipAddressSetTarget {
	out := make([]ipAddressSetTarget, 0, len(targets))
	for _, target := range targets {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key() < out[j].key() })
	return out
}

func referencedIPAddressSetTargets(router *api.Router) map[string][]ipAddressSetTarget {
	out := map[string][]ipAddressSetTarget{}
	if router == nil {
		return out
	}
	add := func(ref string, targets ...ipAddressSetTarget) {
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return
		}
		name := ref
		if kind, rest, ok := strings.Cut(ref, "/"); ok && kind == "IPAddressSet" {
			name = rest
		}
		for i := range targets {
			if targets[i].SetName == "" {
				targets[i].SetName = render.NftIPAddressSetName(name)
			}
		}
		seen := map[string]bool{}
		for _, existing := range out[name] {
			seen[existing.key()] = true
		}
		for _, target := range targets {
			if seen[target.key()] {
				continue
			}
			out[name] = append(out[name], target)
			seen[target.key()] = true
		}
		sort.Slice(out[name], func(i, j int) bool { return out[name][i].key() < out[name][j].key() })
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "LocalServiceRedirect":
			if resource.APIVersion != api.FirewallAPIVersion {
				continue
			}
			spec, err := resource.LocalServiceRedirectSpec()
			if err != nil {
				continue
			}
			for _, rule := range spec.Rules {
				add(rule.DestinationSetRef,
					ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", Controller: "nat"},
					ipAddressSetTarget{TableFamily: "ip6", AddressFamily: "ip6", Table: "routerd_nat", Controller: "nat"},
				)
			}
		case "NAT44Rule":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.NAT44RuleSpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				add(ref, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_nat", Controller: "nat"})
			}
		case "IPv4PolicyRoute":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.IPv4PolicyRouteSpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				add(ref, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_policy", Controller: "route"})
			}
		case "IPv4PolicyRouteSet":
			if resource.APIVersion != api.NetAPIVersion {
				continue
			}
			spec, err := resource.IPv4PolicyRouteSetSpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				add(ref, ipAddressSetTarget{TableFamily: "ip", AddressFamily: "ip", Table: "routerd_policy", Controller: "route"})
			}
		case "FirewallRule":
			if resource.APIVersion != api.FirewallAPIVersion {
				continue
			}
			spec, err := resource.FirewallRuleSpec()
			if err != nil {
				continue
			}
			for _, ref := range append(append([]string{}, spec.DestinationSetRefs...), spec.ExcludeDestinationSetRefs...) {
				name := strings.TrimSpace(ref)
				if kind, rest, ok := strings.Cut(name, "/"); ok && kind == "IPAddressSet" {
					name = rest
				}
				if name == "" {
					continue
				}
				add(ref,
					ipAddressSetTarget{TableFamily: "inet", AddressFamily: "ip", Table: "routerd_filter", SetName: render.NftFirewallIPAddressSetName(name, "ip"), Controller: "firewall"},
					ipAddressSetTarget{TableFamily: "inet", AddressFamily: "ip6", Table: "routerd_filter", SetName: render.NftFirewallIPAddressSetName(name, "ip6"), Controller: "firewall"},
				)
			}
		}
	}
	return out
}

type systemDNSResolver struct{}

func (systemDNSResolver) ResolveIP(ctx context.Context, name string) ([]ipAddressSetRecord, error) {
	cfg, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil || len(cfg.Servers) == 0 {
		cfg = &dns.ClientConfig{Servers: []string{"1.1.1.1"}, Port: "53", Timeout: 2, Attempts: 1}
	}
	var records []ipAddressSetRecord
	var errs []string
	for _, qtype := range []uint16{dns.TypeA, dns.TypeAAAA} {
		resolved, err := resolveSystemDNSRecords(ctx, cfg, name, qtype)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		records = append(records, resolved...)
	}
	if len(records) > 0 {
		return records, nil
	}
	return nil, fmt.Errorf("resolve %s: %s", name, strings.Join(errs, "; "))
}

func resolveSystemDNSRecords(ctx context.Context, cfg *dns.ClientConfig, name string, qtype uint16) ([]ipAddressSetRecord, error) {
	queryName := dns.Fqdn(name)
	msg := new(dns.Msg)
	msg.SetQuestion(queryName, qtype)
	client := &dns.Client{Net: "udp", Timeout: 2 * time.Second}
	var errs []string
	for _, server := range cfg.Servers {
		addr := net.JoinHostPort(server, firstNonEmpty(cfg.Port, "53"))
		resp, _, err := client.ExchangeContext(ctx, msg, addr)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", addr, err))
			continue
		}
		if resp == nil {
			errs = append(errs, fmt.Sprintf("%s: empty response", addr))
			continue
		}
		if resp.Rcode != dns.RcodeSuccess {
			errs = append(errs, fmt.Sprintf("%s: rcode %s", addr, dns.RcodeToString[resp.Rcode]))
			continue
		}
		var out []ipAddressSetRecord
		for _, rr := range resp.Answer {
			var addr netip.Addr
			var ttl uint32
			switch record := rr.(type) {
			case *dns.A:
				if qtype != dns.TypeA {
					continue
				}
				var ok bool
				addr, ok = netip.AddrFromSlice(record.A)
				if !ok {
					continue
				}
				addr = addr.Unmap()
				ttl = record.Hdr.Ttl
			case *dns.AAAA:
				if qtype != dns.TypeAAAA {
					continue
				}
				var ok bool
				addr, ok = netip.AddrFromSlice(record.AAAA)
				if !ok {
					continue
				}
				ttl = record.Hdr.Ttl
			default:
				continue
			}
			out = append(out, ipAddressSetRecord{Address: addr.String(), TTL: time.Duration(ttl) * time.Second})
		}
		if len(out) > 0 {
			return out, nil
		}
		errs = append(errs, fmt.Sprintf("%s: no %s records", addr, dns.TypeToString[qtype]))
	}
	return nil, fmt.Errorf("%s %s: %s", name, dns.TypeToString[qtype], strings.Join(errs, "; "))
}
