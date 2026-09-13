// SPDX-License-Identifier: BSD-3-Clause

package dnsresolver

import (
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

const DaemonKind = "routerd-dns-resolver"

type RuntimeConfig struct {
	Resource string              `json:"resource"`
	Spec     api.DNSResolverSpec `json:"spec"`
	Zones    []RuntimeZone       `json:"zones,omitempty"`
}

type RuntimeZone struct {
	Name            string                  `json:"name"`
	Spec            api.DNSZoneSpec         `json:"spec"`
	DHCPHostRecords []RuntimeDHCPHostRecord `json:"dhcpHostRecords,omitempty"`
}

// RuntimeDHCPHostRecord is a routerd-generated dnsmasq host entry captured in
// the resolver runtime snapshot. It lets a freshly started resolver restore a
// sticky DHCP hostname even when that client is not present in the active
// dnsmasq lease file and has not emitted a new lease event yet.
type RuntimeDHCPHostRecord struct {
	Hostname string `json:"hostname"`
	IP       string `json:"ip"`
}

// ParseDNSMasqHostRecords extracts hostname/address pairs from routerd's
// dnsmasq-hosts sidecar. The sidecar contains both reservation form
// (mac,set:tag,hostname,ip) and sticky form (mac,ip,hostname,lease-time).
func ParseDNSMasqHostRecords(data []byte) []RuntimeDHCPHostRecord {
	seen := map[string]bool{}
	var records []RuntimeDHCPHostRecord
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ",")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		ipIndex := -1
		ip := ""
		for i := 1; i < len(fields); i++ {
			candidate := strings.Trim(fields[i], "[]")
			if net.ParseIP(candidate) != nil {
				ipIndex = i
				ip = candidate
				break
			}
		}
		if ipIndex < 0 {
			continue
		}
		hostname := ""
		for _, index := range []int{ipIndex + 1, ipIndex - 1} {
			if index >= 0 && index < len(fields) && dnsmasqHostnameField(fields[index]) {
				hostname = fields[index]
				break
			}
		}
		if hostname == "" {
			continue
		}
		key := strings.ToLower(hostname) + "|" + ip
		if seen[key] {
			continue
		}
		seen[key] = true
		records = append(records, RuntimeDHCPHostRecord{Hostname: hostname, IP: ip})
	}
	sort.Slice(records, func(i, j int) bool {
		left := strings.ToLower(records[i].Hostname) + "|" + records[i].IP
		right := strings.ToLower(records[j].Hostname) + "|" + records[j].IP
		return left < right
	})
	return records
}

func dnsmasqHostnameField(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || value == "*" || net.ParseIP(strings.Trim(value, "[]")) != nil {
		return false
	}
	if _, err := net.ParseMAC(value); err == nil {
		return false
	}
	lower := strings.ToLower(value)
	for _, prefix := range []string{"set:", "tag:", "id:", "net:"} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	if _, err := time.ParseDuration(lower); err == nil {
		return false
	}
	return !strings.ContainsAny(value, " \t")
}

func NormalizeSpec(spec api.DNSResolverSpec) api.DNSResolverSpec {
	if len(spec.Listen) == 0 {
		spec.Listen = []api.DNSResolverListenSpec{{Addresses: []string{"127.0.0.1"}, Port: 5053}}
	}
	for i := range spec.Listen {
		if len(spec.Listen[i].Addresses) == 0 && len(spec.Listen[i].AddressFrom) == 0 {
			spec.Listen[i].Addresses = []string{"127.0.0.1"}
		}
		if spec.Listen[i].Port == 0 {
			spec.Listen[i].Port = 5053
		}
		if spec.Listen[i].Name == "" {
			spec.Listen[i].Name = fmt.Sprintf("listen-%d", i)
		}
	}
	for i := range spec.Sources {
		if spec.Sources[i].Name == "" {
			spec.Sources[i].Name = fmt.Sprintf("source-%d", i)
		}
		if strings.TrimSpace(spec.Sources[i].Healthcheck.Interval) == "" {
			spec.Sources[i].Healthcheck.Interval = "15s"
		}
		if strings.TrimSpace(spec.Sources[i].Healthcheck.Timeout) == "" {
			spec.Sources[i].Healthcheck.Timeout = "3s"
		}
		if spec.Sources[i].Healthcheck.FailThreshold == 0 {
			spec.Sources[i].Healthcheck.FailThreshold = 3
		}
		if spec.Sources[i].Healthcheck.PassThreshold == 0 {
			spec.Sources[i].Healthcheck.PassThreshold = 2
		}
	}
	if spec.Cache.MaxEntries == 0 {
		spec.Cache.MaxEntries = 10000
	}
	if spec.QueryLog.Enabled {
		if strings.TrimSpace(spec.QueryLog.Path) == "" {
			spec.QueryLog.Path = "/var/lib/routerd/dns-queries.db"
		}
	}
	return spec
}

func Validate(spec api.DNSResolverSpec) error {
	spec = NormalizeSpec(spec)
	for _, listen := range spec.Listen {
		if listen.Port < 1 || listen.Port > 65535 {
			return fmt.Errorf("listen port must be between 1 and 65535")
		}
		if len(listen.Addresses) == 0 && len(listen.AddressFrom) == 0 {
			return fmt.Errorf("listen %q requires addresses or addressFrom", listen.Name)
		}
		for _, address := range listen.Addresses {
			if isStatusExpression(address) {
				return fmt.Errorf("listen address %q must use addressFrom", address)
			}
			if net.ParseIP(strings.TrimSpace(address)) == nil {
				return fmt.Errorf("listen address %q must be an IP address", address)
			}
		}
		for _, source := range listen.AddressFrom {
			if strings.TrimSpace(source.Resource) == "" {
				return fmt.Errorf("listen addressFrom requires resource")
			}
		}
	}
	if len(spec.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}
	for _, source := range spec.Sources {
		switch source.Kind {
		case "zone":
			if len(source.ZoneRef) == 0 {
				return fmt.Errorf("zone source %q requires zoneRef", source.Name)
			}
		case "forward", "upstream":
			if len(source.Upstreams) == 0 && len(source.UpstreamFrom) == 0 {
				return fmt.Errorf("%s source %q requires upstreams or upstreamFrom", source.Kind, source.Name)
			}
			for _, upstream := range source.Upstreams {
				if isStatusExpression(upstream) {
					return fmt.Errorf("source %q upstream %q must use upstreamFrom", source.Name, upstream)
				}
				if err := ValidateUpstreamURL(upstream); err != nil {
					return fmt.Errorf("source %q: %w", source.Name, err)
				}
			}
			for _, upstream := range source.UpstreamFrom {
				if strings.TrimSpace(upstream.Resource) == "" {
					return fmt.Errorf("source %q upstreamFrom requires resource", source.Name)
				}
			}
		default:
			return fmt.Errorf("unsupported DNS source kind %q", source.Kind)
		}
		if strings.TrimSpace(source.Healthcheck.Interval) != "" {
			if _, err := time.ParseDuration(source.Healthcheck.Interval); err != nil {
				return fmt.Errorf("source %q healthcheck.interval must be a duration", source.Name)
			}
		}
		if strings.TrimSpace(source.Healthcheck.Timeout) != "" {
			if _, err := time.ParseDuration(source.Healthcheck.Timeout); err != nil {
				return fmt.Errorf("source %q healthcheck.timeout must be a duration", source.Name)
			}
		}
	}
	if spec.QueryLog.Enabled {
		if strings.TrimSpace(spec.QueryLog.Path) == "" {
			return fmt.Errorf("queryLog.path is required when queryLog.enabled is true")
		}
	}
	return nil
}

func parseRetentionDuration(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := time.ParseDuration(strings.TrimSuffix(value, "d") + "h")
		if err != nil {
			return 0, err
		}
		return days * 24, nil
	}
	return time.ParseDuration(value)
}

func isStatusExpression(value string) bool {
	value = strings.TrimSpace(value)
	return strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") && strings.Contains(value, ".status.")
}

func ValidateUpstreamURL(raw string) error {
	normalized := NormalizeUpstream(raw)
	parsed, err := url.Parse(normalized)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" {
		return fmt.Errorf("invalid DNS upstream %q", raw)
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https", "tls", "quic", "udp", "tcp":
		return nil
	default:
		return fmt.Errorf("DNS upstream %q must use https, tls, quic, udp, or tcp", raw)
	}
}

func NormalizeUpstream(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.Contains(trimmed, "://") {
		return trimmed
	}
	if addr, err := netip.ParseAddr(strings.Trim(trimmed, "[]")); err == nil {
		return "udp://" + net.JoinHostPort(addr.String(), "53")
	}
	if host, port, err := net.SplitHostPort(trimmed); err == nil {
		return "udp://" + net.JoinHostPort(host, port)
	}
	if strings.Count(trimmed, ":") > 1 {
		return "udp://" + net.JoinHostPort(trimmed, "53")
	}
	return "udp://" + net.JoinHostPort(trimmed, "53")
}
