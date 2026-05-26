// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func deleteCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	filePath := fs.String("f", "", "router config file whose resources should be deleted")
	ledgerPath := fs.String("ledger-file", defaultLedgerPath, "routerd ownership ledger file")
	statePath := fs.String("state-file", defaultStatePath, "routerd state database file")
	dryRun := fs.Bool("dry-run", false, "show what would be deleted without changing host state")
	force := fs.Bool("force", false, "delete stale state even when the kind is no longer in the current schema")
	apiVersion := fs.String("api-version", "", "apiVersion to use with --force when a stale kind is ambiguous")
	if err := fs.Parse(args); err != nil {
		return err
	}
	targets, err := deleteTargets(fs.Args(), *filePath, *statePath, *force, *apiVersion)
	if err != nil {
		return err
	}
	if len(targets) == 0 {
		return errors.New("delete requires <kind>/<name> or -f <router.yaml>")
	}
	result, err := performDeleteTargets(targets, *statePath, *ledgerPath, *dryRun)
	if err != nil {
		return err
	}
	prefix := ""
	if result.DryRun {
		prefix = "would "
	}
	for _, artifact := range result.Artifacts {
		fmt.Fprintf(stdout, "%sdelete owned artifact %s\n", prefix, artifact)
	}
	for _, item := range result.Deleted {
		fmt.Fprintf(stdout, "%sdelete %s\n", prefix, item)
	}
	return nil
}

func performDeleteTargets(targets []deleteTarget, statePath, ledgerPath string, dryRun bool) (controlapi.DeleteResult, error) {
	stateStore, err := routerstate.Load(statePath)
	if err != nil {
		return controlapi.DeleteResult{}, err
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return controlapi.DeleteResult{}, err
	}
	result := controlapi.DeleteResult{TypeMeta: controlapi.TypeMeta{APIVersion: controlapi.APIVersion, Kind: "DeleteResult"}, DryRun: dryRun}
	for _, target := range targets {
		owner := target.APIVersion + "/" + target.Kind + "/" + target.Name
		artifacts := artifactsForOwner(ledger, owner)
		for _, artifact := range artifacts {
			label := artifact.Kind + "/" + artifact.Name
			if !dryRun {
				cleaned, err := cleanupLedgerOwnedArtifact(artifact)
				if err != nil {
					return result, fmt.Errorf("%s cleanup %s/%s: %w", owner, artifact.Kind, artifact.Name, err)
				}
				if cleaned != "" {
					label = cleaned
				}
			}
			result.Artifacts = append(result.Artifacts, label)
		}
		if !dryRun {
			ledger.Forget(artifacts)
			if deleter, ok := stateStore.(routerstate.ObjectDeleteStore); ok {
				if err := deleter.DeleteObject(target.APIVersion, target.Kind, target.Name); err != nil {
					return result, err
				}
			}
			if recorder, ok := stateStore.(routerstate.EventRecorder); ok {
				_ = recorder.RecordEvent(target.APIVersion, target.Kind, target.Name, "Normal", "Deleted", "resource deleted by routerd delete")
			}
		}
		result.Deleted = append(result.Deleted, owner)
	}
	if !dryRun {
		if err := ledger.Save(ledgerPath); err != nil {
			return result, err
		}
		if err := stateStore.Save(statePath); err != nil {
			return result, err
		}
	}
	return result, nil
}

type deleteTarget struct {
	APIVersion string
	Kind       string
	Name       string
}

func deleteTargets(args []string, filePath, statePath string, force bool, apiVersion string) ([]deleteTarget, error) {
	if filePath != "" {
		if len(args) != 0 {
			return nil, errors.New("delete accepts either -f or <kind>/<name>, not both")
		}
		router, err := config.Load(filePath)
		if err != nil {
			return nil, err
		}
		targets := make([]deleteTarget, 0, len(router.Spec.Resources))
		for _, res := range router.Spec.Resources {
			targets = append(targets, deleteTarget{APIVersion: res.APIVersion, Kind: res.Kind, Name: res.Metadata.Name})
		}
		return targets, nil
	}
	if len(args) != 1 {
		return nil, errors.New("delete requires exactly one <kind>/<name> target")
	}
	target, err := deleteTargetFromArg(args[0])
	if err != nil {
		if !force {
			return nil, err
		}
		target, err = forceDeleteTargetFromArg(args[0], statePath, apiVersion)
		if err != nil {
			return nil, err
		}
	}
	return []deleteTarget{target}, nil
}

func deleteTargetFromArg(arg string) (deleteTarget, error) {
	kind, name, ok := strings.Cut(arg, "/")
	if !ok || kind == "" || name == "" {
		return deleteTarget{}, fmt.Errorf("invalid delete target %q, want <kind>/<name>", arg)
	}
	canonical := canonicalResourceKind(kind)
	apiVersion := apiVersionForKind(canonical)
	if apiVersion == "" {
		return deleteTarget{}, fmt.Errorf("unknown resource kind %q", kind)
	}
	return deleteTarget{APIVersion: apiVersion, Kind: canonical, Name: name}, nil
}

func forceDeleteTargetFromArg(arg, statePath, apiVersion string) (deleteTarget, error) {
	kind, name, ok := strings.Cut(arg, "/")
	if !ok || kind == "" || name == "" {
		return deleteTarget{}, fmt.Errorf("invalid delete target %q, want <kind>/<name>", arg)
	}
	if strings.TrimSpace(apiVersion) != "" {
		return deleteTarget{APIVersion: strings.TrimSpace(apiVersion), Kind: kind, Name: name}, nil
	}
	store, err := routerstate.Load(statePath)
	if err != nil {
		return deleteTarget{}, err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer func() { _ = closer.Close() }()
	}
	lister, ok := store.(routerstate.ObjectStatusLister)
	if !ok {
		return deleteTarget{}, errors.New("delete --force requires object status storage")
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return deleteTarget{}, err
	}
	var matches []routerstate.ObjectStatus
	for _, status := range statuses {
		if status.Kind == kind && status.Name == name {
			matches = append(matches, status)
		}
	}
	if len(matches) == 0 {
		return deleteTarget{}, fmt.Errorf("unknown resource kind %q and no stale state row for %s/%s", kind, kind, name)
	}
	if len(matches) > 1 {
		var versions []string
		for _, match := range matches {
			versions = append(versions, match.APIVersion)
		}
		sort.Strings(versions)
		return deleteTarget{}, fmt.Errorf("delete --force target %s/%s is ambiguous; found apiVersions %s; rerun with --api-version <value>", kind, name, strings.Join(versions, ", "))
	}
	return deleteTarget{APIVersion: matches[0].APIVersion, Kind: kind, Name: name}, nil
}

func artifactsForOwner(ledger resource.Ledger, owner string) []resource.Artifact {
	var out []resource.Artifact
	for _, artifact := range ledger.All() {
		if artifact.Owner == owner {
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

func canonicalResourceKind(kind string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(kind, "-", ""), "_", ""))
	aliases := map[string]string{
		"if":                     "Interface",
		"iface":                  "Interface",
		"interface":              "Interface",
		"interfaces":             "Interface",
		"br":                     "Bridge",
		"bridge":                 "Bridge",
		"bridges":                "Bridge",
		"vxlan":                  "VXLANSegment",
		"vxlans":                 "VXLANSegment",
		"vxlansegment":           "VXLANSegment",
		"wireguard":              "WireGuardInterface",
		"wg":                     "WireGuardInterface",
		"wireguardinterface":     "WireGuardInterface",
		"wireguardpeer":          "WireGuardPeer",
		"wgpeer":                 "WireGuardPeer",
		"tailscale":              "TailscaleNode",
		"tailscalenode":          "TailscaleNode",
		"ts":                     "TailscaleNode",
		"ipsec":                  "IPsecConnection",
		"ipsecconnection":        "IPsecConnection",
		"vrf":                    "VRF",
		"vxlantunnel":            "VXLANTunnel",
		"pd":                     "DHCPv6PrefixDelegation",
		"dhcpv6pd":               "DHCPv6PrefixDelegation",
		"prefixdelegation":       "DHCPv6PrefixDelegation",
		"dhcpv6prefixdelegation": "DHCPv6PrefixDelegation",
		"ipv4static":             "IPv4StaticAddress",
		"ipv4staticaddress":      "IPv4StaticAddress",
		"dhcpv4client":           "DHCPv4Client",
		"dhcpv4server":           "DHCPv4Server",
		"dhcpv4reservation":      "DHCPv4Reservation",
		"dhcpv4relay":            "DHCPv4Relay",
		"dhcprelay":              "DHCPv4Relay",
		"dhcpv6address":          "DHCPv6Address",
		"dhcpv6server":           "DHCPv6Server",
		"dhcpv6information":      "DHCPv6Information",
		"ipv4staticroute":        "IPv4StaticRoute",
		"ipv6route":              "IPv6StaticRoute",
		"ipv6staticroute":        "IPv6StaticRoute",
		"nat":                    "NAT44Rule",
		"snat":                   "NAT44Rule",
		"nat44":                  "NAT44Rule",
		"nat44rule":              "NAT44Rule",
		"portforward":            "PortForward",
		"portforwards":           "PortForward",
		"portnat":                "PortForward",
		"ingress":                "IngressService",
		"ingressservice":         "IngressService",
		"addressset":             "IPAddressSet",
		"ipset":                  "IPAddressSet",
		"localserviceredirect":   "LocalServiceRedirect",
		"serviceredirect":        "LocalServiceRedirect",
		"dslite":                 "DSLiteTunnel",
		"dslitetunnel":           "DSLiteTunnel",
		"dns":                    "DNSResolver",
		"resolver":               "DNSResolver",
		"dnsforwarder":           "DNSForwarder",
		"dnsupstream":            "DNSUpstream",
		"bgp":                    "BGPRouter",
		"bgprouter":              "BGPRouter",
		"bgppeer":                "BGPPeer",
		"bgppeers":               "BGPPeer",
		"bfd":                    "BFD",
		"pppoe":                  "PPPoESession",
		"pppoesession":           "PPPoESession",
		"pppoeclient":            "PPPoESession",
		"fw":                     "FirewallRule",
		"firewall":               "FirewallPolicy",
		"firewallzone":           "FirewallZone",
		"firewallpolicy":         "FirewallPolicy",
		"firewalleventlog":       "FirewallEventLog",
		"firewalllog":            "FirewallEventLog",
		"firewallrule":           "FirewallRule",
		"zone":                   "FirewallZone",
		"hostname":               "Hostname",
		"host":                   "Hostname",
		"sysctlprofile":          "SysctlProfile",
		"sysctlprofiles":         "SysctlProfile",
		"package":                "Package",
		"packages":               "Package",
		"telemetry":              "Telemetry",
		"observabilitypipeline":  "ObservabilityPipeline",
		"obspipeline":            "ObservabilityPipeline",
		"routerdcluster":         "RouterdCluster",
		"cluster":                "RouterdCluster",
		"managementaccess":       "ManagementAccess",
		"mgmtaccess":             "ManagementAccess",
		"route":                  "EgressRoutePolicy",
		"ipv4policyrouteset":     "EgressRoutePolicy",
		"clusternetworkroute":    "ClusterNetworkRoute",
		"k8sroutes":              "ClusterNetworkRoute",
	}
	if canonical, ok := aliases[key]; ok {
		return canonical
	}
	return kind
}

func apiVersionForKind(kind string) string {
	switch kind {
	case "FirewallZone", "FirewallPolicy", "FirewallRule", "FirewallEventLog", "ClientPolicy", "PortForward", "IngressService", "LocalServiceRedirect":
		return api.FirewallAPIVersion
	case "Hostname", "Sysctl", "SysctlProfile", "Package", "NTPClient", "NTPServer", "LogSink", "ObservabilityPipeline", "RouterdCluster", "LogRetention", "WebConsole", "ServiceUnit":
		return api.SystemAPIVersion
	case "Telemetry":
		return api.ObservabilityAPIVersion
	case "Inventory":
		return api.RouterAPIVersion
	case "Interface", "Bridge", "VXLANSegment", "WireGuardInterface", "WireGuardPeer", "TailscaleNode", "IPsecConnection", "VRF", "VXLANTunnel", "PPPoESession", "IPv4StaticAddress", "VirtualAddress", "DHCPv4Client", "IPv4StaticRoute", "IPv6StaticRoute", "ClusterNetworkRoute", "DHCPv4Server", "DHCPv4Reservation", "DHCPv6Address", "IPv6RAAddress", "DHCPv6PrefixDelegation", "IPv6DelegatedAddress", "DHCPv6Information", "IPv6RouterAdvertisement", "DHCPv6Server", "DHCPv4Relay", "DNSZone", "DNSResolver", "DNSForwarder", "DNSUpstream", "SelfAddressPolicy", "DSLiteTunnel", "IPv4Route", "HealthCheck", "EgressRoutePolicy", "EventRule", "DerivedEvent", "NAT44Rule", "ManagementAccess", "IPAddressSet", "BFD", "TrafficFlowLog":
		return api.NetAPIVersion
	default:
		return ""
	}
}
