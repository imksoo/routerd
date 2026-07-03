// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
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
	Target      string
	Output      string
	Socket      string
	Timeout     time.Duration
	Limit       int
	EventsLimit int
	SinceID     int64
	Topic       string
	Resource    string
	KindFilter  string
	NameFilter  string
}

func getCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseGetOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printGetHelp(stdout)
			return nil
		}
		usage(stderr)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	return getViaControlAPI(ctx, controlapi.NewUnixClient(opts.Socket), opts, stdout)
}

func printGetHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  routerctl get <subject|kind[/name]> [--socket <path>] [--limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -o, --output <format>       output format: table, json, yaml")
	fmt.Fprintln(w, "      --socket <path>         routerd read-only status Unix domain socket path")
	fmt.Fprintln(w, "      --timeout <duration>    request timeout")
	fmt.Fprintln(w, "      --limit <n>             maximum rows for runtime subjects")
	fmt.Fprintln(w, "      --events-limit <n>      recent per-resource events in resource views")
	fmt.Fprintln(w, "      --since <id>            events with id greater than this value")
	fmt.Fprintln(w, "      --topic <topic>         event topic filter")
	fmt.Fprintln(w, "      --resource <kind/name>  event resource filter")
	fmt.Fprintln(w, "      --kind <kind>           event kind filter")
	fmt.Fprintln(w, "      --name <name>           event name filter")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  routerctl get status -o json")
	fmt.Fprintln(w, "  routerctl get Interface/wan")
	fmt.Fprintln(w, "  routerctl get events --topic routerd.resource.status.changed --limit 20")
	fmt.Fprintln(w, "  routerctl get connections")
	fmt.Fprintln(w, "  routerctl get dns-queries")
	fmt.Fprintln(w, "  routerctl get traffic-flows")
	fmt.Fprintln(w, "  routerctl get firewall-logs")
}

func parseGetOptions(args []string) (getOptions, error) {
	opts := getOptions{Socket: defaultStatusSocketPath(), Timeout: 30 * time.Second, Output: "table", Limit: 100, EventsLimit: 10}
	fs := flag.NewFlagSet("get", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.Output, "o", opts.Output, "output format: table, json, yaml")
	fs.StringVar(&opts.Output, "output", opts.Output, "output format: table, json, yaml")
	fs.StringVar(&opts.Socket, "socket", opts.Socket, "routerd read-only status Unix domain socket path")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "request timeout")
	fs.IntVar(&opts.Limit, "limit", opts.Limit, "maximum rows for runtime subjects")
	fs.IntVar(&opts.EventsLimit, "events-limit", opts.EventsLimit, "recent per-resource events in resource views")
	fs.Int64Var(&opts.SinceID, "since", 0, "events with id greater than this value")
	fs.StringVar(&opts.Topic, "topic", "", "event topic filter")
	fs.StringVar(&opts.Resource, "resource", "", "event resource filter as <kind>/<name>")
	fs.StringVar(&opts.KindFilter, "kind", "", "event kind filter")
	fs.StringVar(&opts.NameFilter, "name", "", "event name filter")
	normalized, err := normalizeInspectionArgs(args, map[string]bool{
		"-o": true, "--output": true, "--socket": true, "--timeout": true,
		"--limit": true, "--events-limit": true, "--since": true,
		"--topic": true, "--resource": true, "--kind": true, "--name": true,
	})
	if err != nil {
		return opts, err
	}
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, err
		}
		return opts, err
	}
	if fs.NArg() != 1 {
		return opts, errors.New("get requires <subject> or <kind>/<name>")
	}
	opts.Target = fs.Arg(0)
	return opts, nil
}

func getViaControlAPI(ctx context.Context, client *controlapi.Client, opts getOptions, stdout io.Writer) error {
	subject := canonicalGetSubject(opts.Target)
	switch subject {
	case "connections":
		result, err := client.Connections(ctx, opts.Limit)
		if err != nil {
			return err
		}
		return emitGetValue(stdout, opts.Output, result.Status, func() error { return writeConnectionsTable(stdout, result.Status) })
	case "dns-queries":
		result, err := client.DNSQueries(ctx, controlapi.DNSQueriesRequest{Limit: opts.Limit})
		if err != nil {
			return err
		}
		return emitDNSRows(stdout, opts.Output, result.Items)
	case "traffic-flows":
		result, err := client.TrafficFlows(ctx, controlapi.TrafficFlowsRequest{Limit: opts.Limit})
		if err != nil {
			return err
		}
		return emitTrafficRows(stdout, opts.Output, result.Items)
	case "firewall-logs":
		result, err := client.FirewallLogs(ctx, controlapi.FirewallLogsRequest{Limit: opts.Limit})
		if err != nil {
			return err
		}
		return emitFirewallRows(stdout, opts.Output, result.Items)
	default:
		target, err := canonicalInspectionTargetForAPI("get", opts.Target)
		if err != nil {
			return err
		}
		result, err := client.Get(ctx, controlapi.GetRequest{
			Subject:     target,
			EventsLimit: opts.EventsLimit,
			Limit:       opts.Limit,
			SinceID:     opts.SinceID,
			Topic:       opts.Topic,
			Resource:    opts.Resource,
			KindFilter:  opts.KindFilter,
			NameFilter:  opts.NameFilter,
		})
		if err != nil {
			return err
		}
		return emitGetResult(stdout, opts.Output, result)
	}
}

func canonicalGetSubject(subject string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(subject), "_", "-"))
	switch key {
	case "conn", "connection":
		return "connections"
	case "dns", "dns-query", "dns-queries":
		return "dns-queries"
	case "flow", "flows", "traffic", "traffic-flow", "traffic-flows":
		return "traffic-flows"
	case "firewall", "firewall-log", "firewall-logs":
		return "firewall-logs"
	default:
		return key
	}
}

func canonicalInspectionTargetForAPI(verb, target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return target, nil
	}
	switch canonicalGetSubject(target) {
	case "resources", "all", "status", "controllers", "runtime", "events", "ledger",
		"connections", "dns-queries", "traffic-flows", "firewall-logs":
		return canonicalGetSubject(target), nil
	}
	kind, name, err := parseResourceTarget(verb, target)
	if err != nil {
		return "", err
	}
	if name == "" {
		return kind, nil
	}
	return kind + "/" + name, nil
}

func normalizeInspectionArgs(args []string, valueFlags map[string]bool) ([]string, error) {
	var flags []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "=") {
				flags = append(flags, arg)
				continue
			}
			flags = append(flags, arg)
			if valueFlags[arg] {
				i++
				if i >= len(args) {
					return nil, fmt.Errorf("%s requires a value", arg)
				}
				flags = append(flags, args[i])
			}
			continue
		}
		positionals = append(positionals, arg)
	}
	return append(flags, positionals...), nil
}

func emitGetResult(stdout io.Writer, output string, result *controlapi.GetResult) error {
	switch output {
	case "", "table":
		if len(result.Items) > 0 {
			return writeResourceViewsTable(stdout, result.Items)
		}
		if len(result.Events) > 0 {
			return writeEventsTable(stdout, result.Events)
		}
		if result.Ledger != nil {
			return writeLedgerReportTable(stdout, *result.Ledger)
		}
		if result.Status != nil {
			return writeStatusSummaryTable(stdout, *result.Status)
		}
		return emitGetValue(stdout, output, result.Raw, nil)
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func emitGetValue(stdout io.Writer, output string, value any, table func() error) error {
	switch output {
	case "", "table":
		if table != nil {
			return table()
		}
		return writeJSON(stdout, value)
	case "json":
		return writeJSON(stdout, value)
	case "yaml":
		return writeYAML(stdout, value)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

type describeOptions struct {
	Target      string
	Output      string
	Socket      string
	Timeout     time.Duration
	EventsLimit int
}

func describeCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseDescribeOptions(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printDescribeHelp(stdout)
			return nil
		}
		usage(stderr)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(opts.Socket).Describe(ctx, controlapi.DescribeRequest{Target: opts.Target, EventsLimit: opts.EventsLimit})
	if err != nil {
		return err
	}
	return emitDescribeResult(stdout, opts.Output, result)
}

func printDescribeHelp(w io.Writer) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  routerctl describe <kind>/<name> [--socket <path>] [--events-limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Flags:")
	fmt.Fprintln(w, "  -o, --output <format>    output format: table, json, yaml")
	fmt.Fprintln(w, "      --socket <path>      routerd read-only status Unix domain socket path")
	fmt.Fprintln(w, "      --timeout <duration> request timeout")
	fmt.Fprintln(w, "      --events-limit <n>   recent events to include")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Examples:")
	fmt.Fprintln(w, "  routerctl describe Interface/wan")
	fmt.Fprintln(w, "  routerctl describe pd/wan-pd -o yaml")
}

func parseDescribeOptions(args []string) (describeOptions, error) {
	opts := describeOptions{
		Socket:      defaultStatusSocketPath(),
		Timeout:     30 * time.Second,
		EventsLimit: 10,
	}
	fs := flag.NewFlagSet("describe", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.Output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&opts.Output, "output", "table", "output format: table, json, yaml")
	fs.StringVar(&opts.Socket, "socket", opts.Socket, "routerd read-only status Unix domain socket path")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "request timeout")
	fs.IntVar(&opts.EventsLimit, "events-limit", opts.EventsLimit, "recent events to include")
	normalized, err := normalizeInspectionArgs(args, map[string]bool{
		"-o": true, "--output": true, "--socket": true, "--timeout": true, "--events-limit": true,
	})
	if err != nil {
		return opts, err
	}
	if err := fs.Parse(normalized); err != nil {
		return opts, err
	}
	if fs.NArg() != 1 {
		return opts, errors.New("describe requires <kind>/<name>")
	}
	target, err := canonicalInspectionTargetForAPI("describe", fs.Arg(0))
	if err != nil {
		return opts, err
	}
	opts.Target = target
	return opts, nil
}

func emitDescribeResult(stdout io.Writer, output string, result *controlapi.DescribeResult) error {
	switch output {
	case "", "table":
		return writeResourceViewDescribe(stdout, result.Resource)
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeResourceViewsTable(stdout io.Writer, rows []controlapi.ResourceView) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tSPEC\tSTATUS\tEVENTS")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", row.Kind, row.Name, specSummary(row.Spec), stateSummary(row.Status), len(row.Events))
	}
	return w.Flush()
}

func writeResourceViewDescribe(stdout io.Writer, row controlapi.ResourceView) error {
	show := showResource{
		APIVersion: row.APIVersion,
		Kind:       row.Kind,
		Name:       row.Name,
		Spec:       row.Spec,
		Observed:   row.Status,
		State:      row.Status,
		Events:     row.Events,
	}
	return writeDescribe(stdout, show, nil)
}

func writeLedgerReportTable(stdout io.Writer, report controlapi.LedgerReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "INTEGRITY\t%s\n", displayCell(report.Integrity))
	if len(report.Generations) > 0 {
		fmt.Fprintln(w, "GENERATION\tPHASE\tSTARTED\tHAS_YAML")
		for _, rec := range report.Generations {
			fmt.Fprintf(w, "%d\t%s\t%s\t%t\n", rec.Generation, displayCell(rec.Phase), rec.StartedAt.Format(time.RFC3339), rec.HasYAML)
		}
	}
	return w.Flush()
}

func writeStatusSummaryTable(stdout io.Writer, status controlapi.StatusStatus) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "STATUS\t%s\tgeneration=%d\tresources=%d\n", strings.ToUpper(status.Phase), status.Generation, status.ResourceCount)
	return w.Flush()
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
		"nat44flowdnatpinhole":            "NAT44FlowDNATPinhole",
		"nat44-flow-dnat-pinhole":         "NAT44FlowDNATPinhole",
		"flowdnatpinhole":                 "NAT44FlowDNATPinhole",
		"nat44sessionsync":                "NAT44SessionSync",
		"nat44-session-sync":              "NAT44SessionSync",
		"natsessionsync":                  "NAT44SessionSync",
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
		"flowpinhole":                     "FirewallFlowPinhole",
		"firewall":                        "FirewallPolicy",
		"firewallflowpinhole":             "FirewallFlowPinhole",
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
	case "FirewallZone", "FirewallPolicy", "FirewallEventLog", "FirewallRule", "FirewallFlowPinhole", "ClientPolicy", "PortForward", "IngressService", "LocalServiceRedirect":
		return api.FirewallAPIVersion
	case "TunnelInterface", "OverlayPeer", "HybridRoute", "AddressMobilityDomain", "CloudProviderProfile", "RemoteAddressClaim", "ProviderActionPolicy":
		return api.HybridAPIVersion
	case "Interface", "Bridge", "VXLANSegment", "WireGuardInterface", "WireGuardPeer", "TailscaleNode", "IPsecConnection", "VRF", "VXLANTunnel", "PPPoESession", "IPv4StaticAddress", "VirtualAddress", "BGPRouter", "BGPPeer", "BFD", "DHCPv4Client", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "DHCPv4Server", "DHCPv4ServerLeaseSync", "DHCPv4Reservation", "DHCPv6Address", "IPv6RAAddress", "DHCPv6PrefixDelegation", "IPv6DelegatedAddress", "DHCPv6Information", "IPv6RouterAdvertisement", "DHCPv6Server", "DHCPv6ServerLeaseSync", "DHCPv6PrefixDelegationLeaseSync", "DHCPv4Relay", "DNSZone", "DNSResolver", "DNSForwarder", "DNSUpstream", "SelfAddressPolicy", "DSLiteTunnel", "IPv4Route", "HealthCheck", "EgressRoutePolicy", "EventRule", "DerivedEvent", "NAT44Rule", "NAT44FlowDNATPinhole", "NAT44SessionSync", "ManagementAccess", "IPAddressSet", "TrafficFlowLog":
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
