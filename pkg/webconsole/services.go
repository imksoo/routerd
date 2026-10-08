// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"strings"

	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

type portProtocolFallback struct {
	app        string
	category   string
	confidence int
}

func applyTrafficFlowPortFallback(flow *logstore.TrafficFlow) {
	if flow == nil {
		return
	}
	flow.AppName = canonicalProtocolAppName(flow.AppName)
	if fallback, ok := portProtocolFallbackFor(flow.Protocol, flow.PeerPort, flow.ClientPort, flow.ResolvedHostname, ""); ok {
		override := knownAppName(flow.AppName) && preferPortFallbackOverApp(flow.AppName, fallback.app)
		if knownAppName(flow.AppName) && !override && !preferMoreSpecificPortFallback(flow.AppCategory, flow.AppName, flow.AppConfidence, fallback) {
			return
		}
		flow.AppName = fallback.app
		flow.AppCategory = fallback.category
		flow.AppConfidence = fallback.confidence
		flow.Source = "port-fallback"
		if override {
			flow.ResolvedHostname = ""
		}
	}
}

func applyTrafficFlowListPortFallback(flows []logstore.TrafficFlow) {
	for i := range flows {
		applyTrafficFlowPortFallback(&flows[i])
	}
}

func applyConnectionPortFallback(entry *observe.ConnectionEntry) {
	if entry == nil {
		return
	}
	entry.AppName = canonicalProtocolAppName(entry.AppName)
	if fallback, ok := portProtocolFallbackFor(entry.Protocol, atoiDefault(entry.Original.DestinationPort, 0), atoiDefault(entry.Original.SourcePort, 0), entry.Original.DestinationHostname, entry.Original.SourceHostname); ok {
		override := knownAppName(entry.AppName) && preferPortFallbackOverApp(entry.AppName, fallback.app)
		if knownAppName(entry.AppName) && !override && !preferMoreSpecificPortFallback(entry.AppCategory, entry.AppName, entry.AppConfidence, fallback) {
			return
		}
		entry.AppName = fallback.app
		entry.AppCategory = fallback.category
		entry.AppConfidence = fallback.confidence
		if override {
			entry.DNSQuery = ""
		}
	}
}

func preferMoreSpecificPortFallback(category, current string, confidence int, fallback portProtocolFallback) bool {
	if !strings.EqualFold(strings.TrimSpace(category), "port-fallback") {
		return false
	}
	current = strings.ToLower(strings.TrimSpace(current))
	if current == "" || current == "unknown" || current == "unidentified" {
		return true
	}
	if fallback.confidence > confidence {
		return true
	}
	return current == "stun" && fallback.app == "tailscale"
}

func applyConnectionTablePortFallback(table *observe.ConnectionTable) {
	if table == nil {
		return
	}
	for i := range table.Entries {
		applyConnectionPortFallback(&table.Entries[i])
	}
}

func knownAppName(value string) bool {
	value = canonicalProtocolAppName(value)
	return value != "" && value != "unknown" && value != "unidentified"
}

func canonicalProtocolAppName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "aws-https", "google-https", "microsoft-https", "apple-https", "cloudflare-https":
		return "tls"
	default:
		return value
	}
}

func preferPortFallbackOverApp(current, fallback string) bool {
	current = strings.ToLower(strings.TrimSpace(current))
	fallback = strings.ToLower(strings.TrimSpace(fallback))
	if current != "dns" {
		return false
	}
	switch fallback {
	case "tailscale", "stun", "wireguard", "quic":
		return true
	default:
		return false
	}
}

func portProtocolFallbackFor(protocol string, primaryPort, secondaryPort int, primaryHost, secondaryHost string) (portProtocolFallback, bool) {
	transport := strings.ToLower(strings.TrimSpace(protocol))
	for _, item := range []struct {
		port int
		host string
	}{{primaryPort, primaryHost}, {secondaryPort, secondaryHost}} {
		if fallback, ok := portProtocolFallbackByPort(transport, item.port, item.host); ok {
			return fallback, true
		}
	}
	return portProtocolFallback{}, false
}

func portProtocolFallbackByPort(protocol string, port int, host string) (portProtocolFallback, bool) {
	if port <= 0 {
		return portProtocolFallback{}, false
	}
	confidence := 40
	category := "port-fallback"
	host = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if (port == 443 || port == 8443) && protocol == "tcp" {
		if tailscaleHostLabel(host) {
			return portProtocolFallback{app: "tailscale", category: category, confidence: 60}, true
		}
	}
	if protocol == "udp" && tailscaleHostLabel(host) {
		switch port {
		case 3478, 5349, 41641:
			return portProtocolFallback{app: "tailscale", category: category, confidence: 60}, true
		}
	}
	switch port {
	case 20, 21:
		return portProtocolFallback{app: "ftp", category: category, confidence: confidence}, true
	case 22:
		return portProtocolFallback{app: "ssh", category: category, confidence: confidence}, true
	case 25, 465, 587:
		return portProtocolFallback{app: "smtp", category: category, confidence: confidence}, true
	case 53:
		return portProtocolFallback{app: "dns", category: category, confidence: confidence}, true
	case 67, 68:
		if protocol == "udp" {
			return portProtocolFallback{app: "dhcp", category: category, confidence: confidence}, true
		}
	case 80, 8080, 8000, 8888:
		return portProtocolFallback{app: "http", category: category, confidence: confidence}, true
	case 110, 995:
		return portProtocolFallback{app: "pop3", category: category, confidence: confidence}, true
	case 123:
		if protocol == "udp" {
			return portProtocolFallback{app: "ntp", category: category, confidence: confidence}, true
		}
	case 137, 138:
		if protocol == "udp" {
			return portProtocolFallback{app: "netbios", category: category, confidence: confidence}, true
		}
	case 139, 445:
		return portProtocolFallback{app: "smb", category: category, confidence: confidence}, true
	case 143, 993:
		return portProtocolFallback{app: "imap", category: category, confidence: confidence}, true
	case 443, 8443:
		if protocol == "udp" {
			return portProtocolFallback{app: "quic", category: category, confidence: 35}, true
		}
		return portProtocolFallback{app: "tls", category: category, confidence: confidence}, true
	case 500, 4500:
		if protocol == "udp" {
			return portProtocolFallback{app: "ipsec", category: category, confidence: confidence}, true
		}
	case 853:
		return portProtocolFallback{app: "dns", category: category, confidence: confidence}, true
	case 1900:
		if protocol == "udp" {
			return portProtocolFallback{app: "ssdp", category: category, confidence: confidence}, true
		}
	case 3306:
		return portProtocolFallback{app: "mysql", category: category, confidence: confidence}, true
	case 3389:
		return portProtocolFallback{app: "rdp", category: category, confidence: confidence}, true
	case 4317:
		if protocol == "tcp" {
			return portProtocolFallback{app: "otlp", category: category, confidence: confidence}, true
		}
	case 3478, 5349:
		if protocol == "udp" {
			return portProtocolFallback{app: "stun", category: category, confidence: confidence}, true
		}
	case 4318:
		if protocol == "tcp" {
			return portProtocolFallback{app: "otlp-http", category: category, confidence: confidence}, true
		}
	case 5353:
		if protocol == "udp" {
			return portProtocolFallback{app: "mdns", category: category, confidence: confidence}, true
		}
	case 5355:
		if protocol == "udp" {
			return portProtocolFallback{app: "llmnr", category: category, confidence: confidence}, true
		}
	case 5432:
		return portProtocolFallback{app: "postgresql", category: category, confidence: confidence}, true
	case 51820:
		if protocol == "udp" {
			return portProtocolFallback{app: "wireguard", category: category, confidence: confidence}, true
		}
	case 41641:
		if protocol == "udp" {
			return portProtocolFallback{app: "tailscale", category: category, confidence: 55}, true
		}
	}
	return portProtocolFallback{}, false
}

func tailscaleHostLabel(host string) bool {
	if host == "" {
		return false
	}
	return host == "stun.l.google.com" ||
		host == "login.tailscale.com" ||
		host == "controlplane.tailscale.com" ||
		strings.HasSuffix(host, ".tailscale.com") ||
		strings.HasSuffix(host, ".ts.net")
}

func serviceNameForPort(protocol string, port int) string {
	if port <= 0 {
		return ""
	}
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if name, ok := ianaServiceNames[port]; ok {
		if protocol == "udp" {
			if udp, ok := ianaUDPServiceNames[port]; ok {
				return udp
			}
		}
		return name
	}
	return ""
}

var ianaServiceNames = map[int]string{
	20:    "ftp-data",
	21:    "ftp",
	22:    "ssh",
	25:    "smtp",
	53:    "dns",
	67:    "dhcp-server",
	68:    "dhcp-client",
	80:    "http",
	110:   "pop3",
	123:   "ntp",
	137:   "netbios-ns",
	138:   "netbios-dgm",
	139:   "netbios-ssn",
	143:   "imap",
	443:   "https",
	445:   "microsoft-ds",
	465:   "submissions",
	500:   "isakmp",
	587:   "submission",
	853:   "domain-s",
	993:   "imaps",
	995:   "pop3s",
	1900:  "ssdp",
	3306:  "mysql",
	3389:  "ms-wbt-server",
	3478:  "stun",
	4317:  "otlp",
	4318:  "otlp-http",
	4500:  "ipsec-nat-t",
	5432:  "postgresql",
	5353:  "mdns",
	5355:  "llmnr",
	51820: "wireguard",
	41641: "tailscale",
}

var ianaUDPServiceNames = map[int]string{
	53:    "dns",
	67:    "dhcp-server",
	68:    "dhcp-client",
	123:   "ntp",
	137:   "netbios-ns",
	138:   "netbios-dgm",
	443:   "quic",
	500:   "isakmp",
	853:   "domain-s",
	1900:  "ssdp",
	3478:  "stun",
	4500:  "ipsec-nat-t",
	5353:  "mdns",
	5355:  "llmnr",
	51820: "wireguard",
	41641: "tailscale",
}
