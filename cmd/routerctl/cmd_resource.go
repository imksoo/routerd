// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type showResource struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string              `json:"kind" yaml:"kind"`
	Name       string              `json:"name" yaml:"name"`
	Source     string              `json:"source,omitempty" yaml:"source,omitempty"`
	Stale      bool                `json:"stale,omitempty" yaml:"stale,omitempty"`
	Spec       any                 `json:"spec,omitempty" yaml:"spec,omitempty"`
	Observed   map[string]any      `json:"observed,omitempty" yaml:"observed,omitempty"`
	Ledger     []resource.Artifact `json:"ledger,omitempty" yaml:"ledger,omitempty"`
	State      map[string]any      `json:"state,omitempty" yaml:"state,omitempty"`
	Diff       []showDiff          `json:"diff,omitempty" yaml:"diff,omitempty"`
	Adopt      []any               `json:"adopt,omitempty" yaml:"adopt,omitempty"`
	Events     []routerstate.Event `json:"events,omitempty" yaml:"events,omitempty"`
}

type showDiff struct {
	Field    string `json:"field" yaml:"field"`
	Spec     any    `json:"spec,omitempty" yaml:"spec,omitempty"`
	Observed any    `json:"observed,omitempty" yaml:"observed,omitempty"`
}

type getOptions struct {
	Target     string
	Output     string
	ConfigPath string
	ListKinds  bool
}

func getCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseGetOptions(args)
	if err != nil {
		usage(stderr)
		return err
	}
	router, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	if opts.ListKinds {
		return writeGetKinds(stdout, router.Spec.Resources, opts.Output)
	}
	kind, name, err := parseResourceTarget("get", opts.Target)
	if err != nil {
		return err
	}
	resources := selectResources(router.Spec.Resources, kind, name)
	if len(resources) == 0 {
		return resourceSelectionError(router.Spec.Resources, kind, name)
	}
	switch opts.Output {
	case "", "table":
		return writeGetTable(stdout, resources)
	case "json":
		return writeJSON(stdout, resources)
	case "yaml":
		return writeYAML(stdout, resources)
	default:
		return fmt.Errorf("unsupported output %q", opts.Output)
	}
}

func parseGetOptions(args []string) (getOptions, error) {
	opts := getOptions{ConfigPath: defaultConfigPath()}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-o", "--output":
			i++
			if i >= len(args) {
				return opts, errors.New("-o requires a value")
			}
			opts.Output = args[i]
		case "--config":
			i++
			if i >= len(args) {
				return opts, errors.New("--config requires a value")
			}
			opts.ConfigPath = args[i]
		case "--list-kinds":
			opts.ListKinds = true
		default:
			if strings.HasPrefix(arg, "-o=") {
				opts.Output = strings.TrimPrefix(arg, "-o=")
				continue
			}
			if strings.HasPrefix(arg, "--output=") {
				opts.Output = strings.TrimPrefix(arg, "--output=")
				continue
			}
			if strings.HasPrefix(arg, "--config=") {
				opts.ConfigPath = strings.TrimPrefix(arg, "--config=")
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown get option %q", arg)
			}
			if opts.Target != "" {
				return opts, fmt.Errorf("unexpected get argument %q", arg)
			}
			opts.Target = arg
		}
	}
	if !opts.ListKinds && opts.Target == "" {
		return opts, errors.New("get requires <kind>, <kind>/<name>, or --list-kinds")
	}
	return opts, nil
}

type describeOptions struct {
	Target      string
	Output      string
	ConfigPath  string
	StatePath   string
	LedgerPath  string
	EventsLimit int
}

func describeCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseDescribeOptions(args)
	if err != nil {
		usage(stderr)
		return err
	}
	router, err := config.Load(opts.ConfigPath)
	if err != nil {
		return err
	}
	store, err := routerstate.Load(opts.StatePath)
	if err != nil {
		return err
	}
	ledger, err := resource.LoadLedger(opts.LedgerPath)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	kind, name, err := parseResourceTarget("describe", opts.Target)
	if err != nil {
		return err
	}
	if kind == "FirewallPolicy" && (name == "" || name == "firewall") {
		if opts.Output != "" && opts.Output != "table" {
			return fmt.Errorf("unsupported output %q", opts.Output)
		}
		return describeFirewall(stdout, router)
	}
	if kind == "Orphan" {
		if opts.Output != "" && opts.Output != "table" {
			return fmt.Errorf("unsupported output %q", opts.Output)
		}
		return writeOrphans(stdout, router, ledger)
	}
	if name == "" {
		return errors.New("describe requires <kind>/<name>")
	}
	if kind == "Inventory" {
		row, err := inventoryShowResource(store, name, opts.EventsLimit)
		if err != nil {
			return err
		}
		return writeDescribeOutput(stdout, row, store, opts.Output)
	}
	resources := selectResources(router.Spec.Resources, kind, name)
	if len(resources) == 0 {
		return resourceSelectionError(router.Spec.Resources, kind, name)
	}
	rows, err := buildShowResources(router, resources, store, ledger, showOptions{Events: true, ConnectionsLimit: 20})
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("describe expected one resource, got %d", len(rows))
	}
	rows[0].Events = eventsForResourceLimit(store, resources[0], opts.EventsLimit)
	return writeDescribeOutput(stdout, rows[0], store, opts.Output)
}

func parseDescribeOptions(args []string) (describeOptions, error) {
	opts := describeOptions{
		ConfigPath:  defaultConfigPath(),
		StatePath:   defaultStatePath(),
		LedgerPath:  defaultLedgerPath(),
		EventsLimit: 10,
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-o", "--output":
			i++
			if i >= len(args) {
				return opts, errors.New("-o requires a value")
			}
			opts.Output = args[i]
		case "--config":
			i++
			if i >= len(args) {
				return opts, errors.New("--config requires a value")
			}
			opts.ConfigPath = args[i]
		case "--state-file":
			i++
			if i >= len(args) {
				return opts, errors.New("--state-file requires a value")
			}
			opts.StatePath = args[i]
		case "--ledger-file":
			i++
			if i >= len(args) {
				return opts, errors.New("--ledger-file requires a value")
			}
			opts.LedgerPath = args[i]
		case "--events-limit":
			i++
			if i >= len(args) {
				return opts, errors.New("--events-limit requires a value")
			}
			if _, err := fmt.Sscanf(args[i], "%d", &opts.EventsLimit); err != nil || opts.EventsLimit < 0 {
				return opts, errors.New("--events-limit must be a non-negative integer")
			}
		default:
			if strings.HasPrefix(arg, "-o=") {
				opts.Output = strings.TrimPrefix(arg, "-o=")
				continue
			}
			if strings.HasPrefix(arg, "--output=") {
				opts.Output = strings.TrimPrefix(arg, "--output=")
				continue
			}
			if strings.HasPrefix(arg, "--config=") {
				opts.ConfigPath = strings.TrimPrefix(arg, "--config=")
				continue
			}
			if strings.HasPrefix(arg, "--state-file=") {
				opts.StatePath = strings.TrimPrefix(arg, "--state-file=")
				continue
			}
			if strings.HasPrefix(arg, "--ledger-file=") {
				opts.LedgerPath = strings.TrimPrefix(arg, "--ledger-file=")
				continue
			}
			if strings.HasPrefix(arg, "--events-limit=") {
				if _, err := fmt.Sscanf(strings.TrimPrefix(arg, "--events-limit="), "%d", &opts.EventsLimit); err != nil || opts.EventsLimit < 0 {
					return opts, errors.New("--events-limit must be a non-negative integer")
				}
				continue
			}
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown describe option %q", arg)
			}
			if opts.Target != "" {
				return opts, fmt.Errorf("unexpected describe argument %q", arg)
			}
			opts.Target = arg
		}
	}
	if opts.Target == "" {
		return opts, errors.New("describe requires <kind>/<name>")
	}
	return opts, nil
}

func parseShowTarget(target string) (string, string, error) {
	return parseResourceTarget("show", target)
}

func parseResourceTarget(verb, target string) (string, string, error) {
	kind, name, hasName := strings.Cut(target, "/")
	kind = canonicalShowKind(kind)
	if kind == "" {
		return "", "", fmt.Errorf("unknown resource kind %q", target)
	}
	if hasName && strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("%s target %q has empty name", verb, target)
	}
	return kind, name, nil
}

func canonicalShowKind(kind string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(kind, "-", ""), "_", ""))
	aliases := map[string]string{
		"if":                              "Interface",
		"iface":                           "Interface",
		"interface":                       "Interface",
		"interfaces":                      "Interface",
		"br":                              "Bridge",
		"bridge":                          "Bridge",
		"bridges":                         "Bridge",
		"vxlan":                           "VXLANSegment",
		"vxlans":                          "VXLANSegment",
		"vxlansegment":                    "VXLANSegment",
		"wireguard":                       "WireGuardInterface",
		"wg":                              "WireGuardInterface",
		"wireguardinterface":              "WireGuardInterface",
		"tunnel":                          "TunnelInterface",
		"tunnelinterface":                 "TunnelInterface",
		"wireguardpeer":                   "WireGuardPeer",
		"wgpeer":                          "WireGuardPeer",
		"tailscale":                       "TailscaleNode",
		"tailscalenode":                   "TailscaleNode",
		"ts":                              "TailscaleNode",
		"ipsec":                           "IPsecConnection",
		"ipsecconnection":                 "IPsecConnection",
		"vrf":                             "VRF",
		"vxlantunnel":                     "VXLANTunnel",
		"pd":                              "DHCPv6PrefixDelegation",
		"dhcpv6pd":                        "DHCPv6PrefixDelegation",
		"prefixdelegation":                "DHCPv6PrefixDelegation",
		"dhcpv6prefixdelegation":          "DHCPv6PrefixDelegation",
		"ipv4static":                      "IPv4StaticAddress",
		"ipv4staticaddress":               "IPv4StaticAddress",
		"vip":                             "VirtualAddress",
		"vips":                            "VirtualAddress",
		"virtualip":                       "VirtualAddress",
		"virtualipv4":                     "VirtualAddress",
		"virtualipv4address":              "VirtualAddress",
		"virtualipv6":                     "VirtualAddress",
		"virtualipv6address":              "VirtualAddress",
		"vrrp":                            "VirtualAddress",
		"bgp":                             "BGPRouter",
		"bgprouter":                       "BGPRouter",
		"bgppeer":                         "BGPPeer",
		"bgppeers":                        "BGPPeer",
		"bfd":                             "BFD",
		"ingress":                         "IngressService",
		"ingressservice":                  "IngressService",
		"ingressservices":                 "IngressService",
		"dhcpv4client":                    "DHCPv4Client",
		"dhcpv4server":                    "DHCPv4Server",
		"dhcpv4serverleasesync":           "DHCPv4ServerLeaseSync",
		"dhcpv4-lease-sync":               "DHCPv4ServerLeaseSync",
		"dhcpv4reservation":               "DHCPv4Reservation",
		"dhcpv4relay":                     "DHCPv4Relay",
		"dhcprelay":                       "DHCPv4Relay",
		"dhcpv6address":                   "DHCPv6Address",
		"dhcpv6server":                    "DHCPv6Server",
		"dhcpv6serverleasesync":           "DHCPv6ServerLeaseSync",
		"dhcpv6-lease-sync":               "DHCPv6ServerLeaseSync",
		"dhcpv6prefixdelegationleasesync": "DHCPv6PrefixDelegationLeaseSync",
		"dhcpv6-pd-lease-sync":            "DHCPv6PrefixDelegationLeaseSync",
		"dhcpv6information":               "DHCPv6Information",
		"ipv6ra":                          "IPv6RAAddress",
		"ipv4staticroute":                 "IPv4StaticRoute",
		"clusternetworkroute":             "ClusterNetworkRoute",
		"k8sroutes":                       "ClusterNetworkRoute",
		"ipv6route":                       "IPv6StaticRoute",
		"ipv6staticroute":                 "IPv6StaticRoute",
		"ipv6raaddress":                   "IPv6RAAddress",
		"slaac":                           "IPv6RAAddress",
		"nat":                             "NAT44Rule",
		"snat":                            "NAT44Rule",
		"ipv4nat":                         "NAT44Rule",
		"nat44":                           "NAT44Rule",
		"nat44rule":                       "NAT44Rule",
		"portforward":                     "PortForward",
		"portforwards":                    "PortForward",
		"portnat":                         "PortForward",
		"addressset":                      "IPAddressSet",
		"ipset":                           "IPAddressSet",
		"localserviceredirect":            "LocalServiceRedirect",
		"serviceredirect":                 "LocalServiceRedirect",
		"dslite":                          "DSLiteTunnel",
		"dslitetunnel":                    "DSLiteTunnel",
		"dnszone":                         "DNSZone",
		"dnsresolver":                     "DNSResolver",
		"dns":                             "DNSResolver",
		"dnsforwarder":                    "DNSForwarder",
		"dnsupstream":                     "DNSUpstream",
		"pppoe":                           "PPPoESession",
		"pppoesession":                    "PPPoESession",
		"pppoeclient":                     "PPPoESession",
		"fw":                              "FirewallRule",
		"firewall":                        "FirewallPolicy",
		"firewallzone":                    "FirewallZone",
		"firewallpolicy":                  "FirewallPolicy",
		"firewalleventlog":                "FirewallEventLog",
		"firewalllog":                     "FirewallEventLog",
		"firewallrule":                    "FirewallRule",
		"zone":                            "FirewallZone",
		"zones":                           "FirewallZone",
		"hostname":                        "Hostname",
		"host":                            "Hostname",
		"observabilitypipeline":           "ObservabilityPipeline",
		"obspipeline":                     "ObservabilityPipeline",
		"routerdcluster":                  "RouterdCluster",
		"cluster":                         "RouterdCluster",
		"managementaccess":                "ManagementAccess",
		"mgmtaccess":                      "ManagementAccess",
		"kernelmodule":                    "KernelModule",
		"kernelmodules":                   "KernelModule",
		"kmod":                            "KernelModule",
		"inventory":                       "Inventory",
		"inv":                             "Inventory",
		"orphan":                          "Orphan",
		"orphans":                         "Orphan",
		"route":                           "EgressRoutePolicy",
		"routeset":                        "EgressRoutePolicy",
		"ipv4route":                       "EgressRoutePolicy",
		"ipv4policyrouteset":              "EgressRoutePolicy",
	}
	if canonical, ok := aliases[key]; ok {
		return canonical
	}
	if kind == "" {
		return ""
	}
	return kind
}

func showAPIVersionForKnownKind(kind string) string {
	switch kind {
	case "Inventory":
		return api.RouterAPIVersion
	case "LogSink", "ObservabilityPipeline", "RouterdCluster", "LogRetention", "Sysctl", "SysctlProfile", "Package", "NTPClient", "NTPServer", "WebConsole", "ServiceUnit":
		return api.SystemAPIVersion
	case "Telemetry":
		return api.ObservabilityAPIVersion
	case "FirewallZone", "FirewallPolicy", "FirewallEventLog", "FirewallRule", "ClientPolicy", "PortForward", "IngressService", "LocalServiceRedirect":
		return api.FirewallAPIVersion
	case "TunnelInterface", "OverlayPeer", "HybridRoute", "AddressMobilityDomain", "CloudProviderProfile", "RemoteAddressClaim", "ProviderActionPolicy":
		return api.HybridAPIVersion
	case "Interface", "Bridge", "VXLANSegment", "WireGuardInterface", "WireGuardPeer", "TailscaleNode", "IPsecConnection", "VRF", "VXLANTunnel", "PPPoESession", "IPv4StaticAddress", "VirtualAddress", "BGPRouter", "BGPPeer", "BFD", "DHCPv4Client", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "DHCPv4Server", "DHCPv4ServerLeaseSync", "DHCPv4Reservation", "DHCPv6Address", "IPv6RAAddress", "DHCPv6PrefixDelegation", "IPv6DelegatedAddress", "DHCPv6Information", "IPv6RouterAdvertisement", "DHCPv6Server", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync", "DHCPv4Relay", "DNSZone", "DNSResolver", "DNSForwarder", "DNSUpstream", "SelfAddressPolicy", "DSLiteTunnel", "IPv4Route", "HealthCheck", "EgressRoutePolicy", "EventRule", "DerivedEvent", "NAT44Rule", "ManagementAccess", "IPAddressSet", "TrafficFlowLog":
		return api.NetAPIVersion
	default:
		return ""
	}
}

func resourceSelectionError(resources []api.Resource, kind, name string) error {
	if !resourceKindExists(resources, kind) {
		return fmt.Errorf("unknown resource kind %q", kind)
	}
	if name != "" {
		return fmt.Errorf("%s/%s not found", kind, name)
	}
	return fmt.Errorf("no %s resources found", kind)
}

func selectResources(resources []api.Resource, kind, name string) []api.Resource {
	var selected []api.Resource
	for _, res := range resources {
		if res.Kind != kind {
			continue
		}
		if name != "" && res.Metadata.Name != name {
			continue
		}
		selected = append(selected, res)
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].Metadata.Name < selected[j].Metadata.Name
	})
	return selected
}

func resourceKindExists(resources []api.Resource, kind string) bool {
	for _, res := range resources {
		if res.Kind == kind {
			return true
		}
	}
	return false
}
