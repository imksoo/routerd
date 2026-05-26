// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/apply"
	"routerd/pkg/controlapi"
	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
)

func filterRouterByWhen(router *api.Router, store routerstate.Store) *api.Router {
	filtered := *router
	filtered.Spec.Resources = nil
	for _, res := range router.Spec.Resources {
		when := resourceWhen(res)
		if resourceWhenMatches(when, store) {
			if res.Kind == "EgressRoutePolicy" {
				res = filterEgressRoutePolicyCandidatesByWhen(res, store)
			}
			filtered.Spec.Resources = append(filtered.Spec.Resources, res)
		}
	}
	return api.ExpandClusterNetworkRoutes(&filtered)
}

func filterEgressRoutePolicyCandidatesByWhen(res api.Resource, store routerstate.Store) api.Resource {
	spec, err := res.EgressRoutePolicySpec()
	if err != nil {
		return res
	}
	var candidates []api.EgressRoutePolicyCandidate
	for _, candidate := range spec.Candidates {
		if !api.BoolDefault(candidate.Enabled, true) {
			continue
		}
		if resourceWhenMatches(candidate.When, store) {
			candidates = append(candidates, candidate)
		}
	}
	spec.Candidates = candidates
	res.Spec = spec
	return res
}

func resourceWhen(res api.Resource) api.ResourceWhenSpec {
	switch res.Kind {
	case "ObservabilityPipeline":
		spec, _ := res.ObservabilityPipelineSpec()
		return spec.When
	case "RouterdCluster":
		spec, _ := res.RouterdClusterSpec()
		return spec.When
	case "VirtualAddress":
		spec, _ := res.VirtualAddressSpec()
		return spec.When
	case "BGPRouter":
		spec, _ := res.BGPRouterSpec()
		return spec.When
	case "BGPPeer":
		spec, _ := res.BGPPeerSpec()
		return spec.When
	case "BFD":
		spec, _ := res.BFDSpec()
		return spec.When
	case "ClusterNetworkRoute":
		spec, _ := res.ClusterNetworkRouteSpec()
		return spec.When
	case "DHCPv4Server":
		spec, _ := res.DHCPv4ServerSpec()
		return spec.When
	case "IPv6DelegatedAddress":
		spec, _ := res.IPv6DelegatedAddressSpec()
		return spec.When
	case "DHCPv6Server":
		spec, _ := res.DHCPv6ServerSpec()
		return spec.When
	case "DSLiteTunnel":
		spec, _ := res.DSLiteTunnelSpec()
		return spec.When
	case "DNSForwarder":
		spec, _ := res.DNSForwarderSpec()
		return spec.When
	case "DNSUpstream":
		spec, _ := res.DNSUpstreamSpec()
		return spec.When
	case "HealthCheck":
		spec, _ := res.HealthCheckSpec()
		return spec.When
	case "NAT44Rule":
		spec, _ := res.NAT44RuleSpec()
		return spec.When
	case "PortForward":
		spec, _ := res.PortForwardSpec()
		return spec.When
	case "IngressService":
		spec, _ := res.IngressServiceSpec()
		return spec.When
	case "IPAddressSet":
		spec, _ := res.IPAddressSetSpec()
		return spec.When
	case "LocalServiceRedirect":
		spec, _ := res.LocalServiceRedirectSpec()
		return spec.When
	case "EgressRoutePolicy":
		spec, _ := res.EgressRoutePolicySpec()
		return spec.When
	default:
		return api.ResourceWhenSpec{}
	}
}

func resourceWhenMatches(when api.ResourceWhenSpec, store routerstate.Store) bool {
	if len(when.All) > 0 {
		for _, child := range when.All {
			if !resourceWhenMatches(child, store) {
				return false
			}
		}
		return true
	}
	if len(when.Any) > 0 {
		for _, child := range when.Any {
			if resourceWhenMatches(child, store) {
				return true
			}
		}
		return false
	}
	if len(when.State) == 0 {
		return true
	}
	for name, match := range when.State {
		if !stateMatch(store, name, match) {
			return false
		}
	}
	return true
}

func stateMatch(store routerstate.Store, name string, match api.StateMatchSpec) bool {
	value := store.Get(name)
	ok := true
	if match.Status != "" {
		ok = ok && value.Status == match.Status
	}
	if match.Exists != nil {
		if *match.Exists {
			ok = ok && value.Status == routerstate.StatusSet
		} else {
			ok = ok && value.Status == routerstate.StatusUnset
		}
	}
	if match.Equals != "" {
		ok = ok && value.Status == routerstate.StatusSet && value.Value == match.Equals
	}
	if len(match.In) > 0 {
		ok = ok && value.Status == routerstate.StatusSet && stringIn(value.Value, match.In)
	}
	if match.Contains != "" {
		ok = ok && value.Status == routerstate.StatusSet && strings.Contains(value.Value, match.Contains)
	}
	if !ok {
		return false
	}
	if match.For != "" {
		duration, err := time.ParseDuration(match.For)
		if err != nil || store.Age(name) < duration {
			return false
		}
	}
	return true
}

func appendPrefixDelegationStateWarnings(result *apply.Result, router *api.Router, store routerstate.Store) {
	for _, res := range router.Spec.Resources {
		if res.Kind != "DHCPv6PrefixDelegation" {
			continue
		}
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		lease, _ := routerstate.PDLeaseFromStore(store, base)
		if lease.CurrentPrefix != "" {
			continue
		}
		msg := fmt.Sprintf("%s is not currently observable", res.ID())
		if lease.LastPrefix != "" {
			msg += "; last delegated prefix was " + lease.LastPrefix
		} else {
			msg += "; no delegated prefix has been recorded locally yet"
		}
		if lease.LastObservedAt != "" {
			msg += " observed at " + lease.LastObservedAt
		}
		msg += ". The OS DHCPv6 client must renew or reacquire PD before the upstream lease expires."
		result.Warnings = append(result.Warnings, msg)
	}
}

func stringIn(value string, values []string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}

func appendLedgerOwnedOrphans(result *apply.Result, router *api.Router, ledgerPath string, transient bool) error {
	if ledgerPath == "" {
		return nil
	}
	ledger, err := loadPlanLedger(ledgerPath, transient)
	if err != nil {
		return err
	}
	engine := apply.New()
	orphans, _, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		return nil
	}
	result.Orphans = appendUniqueOrphans(result.Orphans, orphans)
	result.Warnings = append(result.Warnings, fmt.Sprintf("%d ledger-owned orphaned artifacts found", len(orphans)))
	if result.Phase == "Healthy" {
		result.Phase = "Drifted"
	}
	return nil
}

func loadPlanLedger(path string, transient bool) (resource.Ledger, error) {
	if !transient {
		return resource.LoadLedger(path)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return resource.NewLedger(), nil
	}
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return resource.NewLedger(), nil
	} else if err != nil {
		return nil, err
	}
	return resource.LoadLedger(path)
}

func appendUniqueOrphans(existing, additions []apply.OrphanedArtifact) []apply.OrphanedArtifact {
	seen := map[string]int{}
	for i, orphan := range existing {
		seen[orphan.Name+"/"+orphan.Remediation] = i
	}
	for _, orphan := range additions {
		id := orphan.Name + "/" + orphan.Remediation
		if index, ok := seen[id]; ok {
			if existing[index].Owner == "" && orphan.Owner != "" {
				existing[index] = orphan
			}
			continue
		}
		seen[id] = len(existing)
		existing = append(existing, orphan)
	}
	return existing
}

func cleanupLedgerOwnedOrphans(router *api.Router, ledgerPath string) ([]string, error) {
	return cleanupLedgerOwnedOrphansMatching(router, ledgerPath, func(resource.Artifact) bool { return true })
}

func cleanupLedgerOwnedOrphansMatching(router *api.Router, ledgerPath string, match func(resource.Artifact) bool) ([]string, error) {
	if ledgerPath == "" {
		return nil, nil
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return nil, err
	}
	engine := apply.New()
	_, artifacts, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		return nil, err
	}
	var removed []string
	var removedArtifacts []resource.Artifact
	sort.SliceStable(artifacts, func(i, j int) bool {
		return cleanupArtifactPriority(artifacts[i]) < cleanupArtifactPriority(artifacts[j])
	})
	for _, artifact := range artifacts {
		if match != nil && !match(artifact) {
			continue
		}
		label, err := cleanupLedgerOwnedArtifact(artifact)
		if err != nil {
			return removed, err
		}
		if label == "" {
			continue
		}
		removed = append(removed, label)
		removedArtifacts = append(removedArtifacts, artifact)
	}
	if len(removedArtifacts) > 0 {
		ledger.Forget(removedArtifacts)
		if err := ledger.Save(ledgerPath); err != nil {
			return removed, err
		}
	}
	return removed, nil
}

func cleanupArtifactPriority(artifact resource.Artifact) int {
	switch artifact.Kind {
	case "linux.ipv4.fwmarkRule":
		return 0
	case "linux.ipv4.routeTable":
		return 5
	case "systemd.service":
		return 10
	case "file":
		return 20
	case "unix.socket":
		return 30
	case "directory":
		return 40
	default:
		return 50
	}
}

func cleanupLedgerOwnedArtifact(artifact resource.Artifact) (string, error) {
	switch artifact.Kind {
	case "linux.ipip6.tunnel":
		if platformFeatures.HasIproute2 {
			if err := runLogged("ip", "-6", "tunnel", "del", artifact.Name); err != nil {
				return "", err
			}
			return artifact.Kind + "/" + artifact.Name, nil
		}
		if platformFeatures.HasPF {
			if err := runLogged("ifconfig", artifact.Name, "destroy"); err != nil {
				return "", err
			}
			return artifact.Kind + "/" + artifact.Name, nil
		}
		return "", nil
	case "linux.ipv4.fwmarkRule":
		rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
		if !ok {
			return "", nil
		}
		if err := deleteIPv4FwmarkRule(rule); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "linux.ipv4.routeTable":
		table, err := strconv.Atoi(artifact.Attributes["table"])
		if err != nil || table == 0 {
			return "", nil
		}
		if err := flushIPv4RouteTable(table); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "nft.table":
		family := artifact.Attributes["family"]
		name := artifact.Attributes["name"]
		if !strings.HasPrefix(name, "routerd_") {
			return "", nil
		}
		if err := runLogged("nft", "delete", "table", family, name); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + name, nil
	case "systemd.service":
		if !strings.HasPrefix(artifact.Name, "routerd-") || !strings.HasSuffix(artifact.Name, ".service") {
			return "", nil
		}
		if err := runLogged("systemctl", "disable", "--now", artifact.Name); err != nil {
			return "", err
		}
		_ = runLogged("systemctl", "reset-failed", artifact.Name)
		unitPath := "/etc/systemd/system/" + artifact.Name
		if err := os.Remove(unitPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := runLogged("systemctl", "daemon-reload"); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "file":
		if !apply.IsPPPoEPeerFileArtifactForCleanup(artifact) {
			return "", nil
		}
		if err := os.Remove(artifact.Name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "unix.socket":
		if !apply.IsPPPoERuntimeSocketArtifactForCleanup(artifact) {
			return "", nil
		}
		if err := os.Remove(artifact.Name); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "directory":
		if !apply.IsPPPoERuntimeDirectoryArtifactForCleanup(artifact) {
			return "", nil
		}
		if err := os.RemoveAll(artifact.Name); err != nil {
			return "", err
		}
		return artifact.Kind + "/" + artifact.Name, nil
	case "net.ipv4.address":
		if !isDSLiteIPv4AddressArtifact(artifact) {
			return "", nil
		}
		ifname, address, ok := strings.Cut(artifact.Name, ":")
		if !ok || ifname == "" || address == "" {
			return "", nil
		}
		if platformFeatures.HasIproute2 {
			if err := runLogged("ip", "-4", "addr", "del", address, "dev", ifname); err != nil {
				return "", err
			}
			return artifact.Kind + "/" + artifact.Name, nil
		}
		if platformFeatures.HasPF {
			if strings.HasPrefix(ifname, "gif") && strings.Contains(artifact.Owner, "/IPv4StaticAddress/ds-lite-source") {
				if err := runLogged("ifconfig", ifname, "destroy"); err != nil {
					return "", err
				}
				return "freebsd.gif.tunnel/" + ifname, nil
			}
			addr := strings.SplitN(address, "/", 2)[0]
			if err := runLogged("ifconfig", ifname, "inet", addr, "-alias"); err != nil {
				return "", err
			}
			return artifact.Kind + "/" + artifact.Name, nil
		}
		return "", nil
	default:
		return "", nil
	}
}

func isDSLiteIPv4AddressArtifact(artifact resource.Artifact) bool {
	return strings.Contains(artifact.Owner, "/IPv4StaticAddress/ds-lite") ||
		strings.Contains(artifact.Name, ":192.168.160.249/32") ||
		strings.Contains(artifact.Name, ":192.168.160.250/32") ||
		strings.Contains(artifact.Name, ":192.168.160.251/32") ||
		strings.Contains(artifact.Name, ":192.168.160.252/32") ||
		strings.Contains(artifact.Name, ":172.18.255.249/32") ||
		strings.Contains(artifact.Name, ":172.18.255.250/32") ||
		strings.Contains(artifact.Name, ":172.18.255.251/32") ||
		strings.Contains(artifact.Name, ":172.18.255.252/32")
}

func cleanupStaleDSLiteTunnels(router *api.Router) ([]string, error) {
	desired := desiredDSLiteTunnelIfNames(router)
	if platformFeatures.HasIproute2 {
		return cleanupStaleLinuxDSLiteTunnels(desired)
	}
	if platformFeatures.HasPF {
		return cleanupStaleFreeBSDDSLiteTunnels(desired)
	}
	return nil, nil
}

func cleanupStaleDSLiteIPv4Aliases(router *api.Router) ([]string, error) {
	desired := desiredDSLiteTunnelIfNames(router)
	if len(desired) == 0 {
		return nil, nil
	}
	if platformFeatures.HasIproute2 {
		return cleanupStaleLinuxDSLiteIPv4Aliases(desired)
	}
	if platformFeatures.HasPF {
		return cleanupStaleFreeBSDDSLiteIPv4Aliases(desired)
	}
	return nil, nil
}

func desiredDSLiteTunnelIfNames(router *api.Router) map[string]bool {
	desired := map[string]bool{}
	if router == nil {
		return desired
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "DSLiteTunnel" {
			continue
		}
		spec, err := res.DSLiteTunnelSpec()
		if err != nil {
			continue
		}
		name := strings.TrimSpace(spec.TunnelName)
		if name == "" {
			name = res.Metadata.Name
		}
		if name != "" {
			desired[name] = true
		}
	}
	return desired
}

func cleanupStaleLinuxDSLiteTunnels(desired map[string]bool) ([]string, error) {
	out, err := exec.Command("ip", "-d", "link", "show", "type", "ip6tnl").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var removed []string
	for _, name := range parseLinuxIPIP6TunnelNames(string(out)) {
		if desired[name] || !looksRouterdDSLiteTunnelName(name) {
			continue
		}
		if err := runLogged("ip", "-6", "tunnel", "del", name); err != nil {
			return removed, err
		}
		removed = append(removed, "linux.ipip6.tunnel/"+name)
	}
	return removed, nil
}

func parseLinuxIPIP6TunnelNames(output string) []string {
	var names []string
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 2 || !strings.HasSuffix(fields[0], ":") {
			continue
		}
		name := strings.TrimSuffix(fields[1], ":")
		if i := strings.Index(name, "@"); i >= 0 {
			name = name[:i]
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func cleanupStaleLinuxDSLiteIPv4Aliases(desired map[string]bool) ([]string, error) {
	out, err := exec.Command("ip", "-brief", "-4", "addr", "show").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var removed []string
	seen := map[string]bool{}
	for _, candidate := range parseBriefIPv4AddressCleanupCandidates(string(out)) {
		if !desired[candidate.ifname] || !staleDSLiteIPv4Address(candidate.address) {
			continue
		}
		id := candidate.ifname + ":" + candidate.address
		if seen[id] {
			continue
		}
		seen[id] = true
		args := []string{"-4", "addr", "del", candidate.address}
		if candidate.peer != "" {
			args = append(args, "peer", candidate.peer)
		}
		args = append(args, "dev", candidate.ifname)
		if err := runLogged("ip", args...); err != nil {
			return removed, err
		}
		removed = append(removed, "net.ipv4.address/"+id)
	}
	return removed, nil
}

func cleanupStaleFreeBSDDSLiteTunnels(desired map[string]bool) ([]string, error) {
	out, err := exec.Command("ifconfig").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var removed []string
	for name, block := range parseIfconfigBlocks(string(out)) {
		if desired[name] || !looksFreeBSDDSLiteTunnel(name, block) {
			continue
		}
		if err := runLogged("ifconfig", name, "destroy"); err != nil {
			return removed, err
		}
		removed = append(removed, "freebsd.gif.tunnel/"+name)
	}
	return removed, nil
}

func cleanupStaleFreeBSDDSLiteIPv4Aliases(desired map[string]bool) ([]string, error) {
	out, err := exec.Command("ifconfig").CombinedOutput()
	if err != nil {
		return nil, nil
	}
	var removed []string
	seen := map[string]bool{}
	for _, artifact := range parseIfconfigIPv4AddressArtifacts(string(out)) {
		ifname, address, ok := strings.Cut(artifact.Name, ":")
		if !ok || !desired[ifname] || !staleDSLiteIPv4Address(address) {
			continue
		}
		id := ifname + ":" + address
		if seen[id] {
			continue
		}
		seen[id] = true
		addr := strings.SplitN(address, "/", 2)[0]
		if err := runLogged("ifconfig", ifname, "inet", addr, "-alias"); err != nil {
			return removed, err
		}
		removed = append(removed, "net.ipv4.address/"+id)
	}
	return removed, nil
}

func staleDSLiteIPv4Address(address string) bool {
	host := strings.SplitN(address, "/", 2)[0]
	switch host {
	case "192.168.160.249", "192.168.160.250", "192.168.160.251", "192.168.160.252",
		"172.18.255.249", "172.18.255.250", "172.18.255.251", "172.18.255.252":
		return true
	default:
		return false
	}
}

type ipv4AddressCleanupCandidate struct {
	ifname  string
	address string
	peer    string
}

func parseBriefIPv4AddressCleanupCandidates(output string) []ipv4AddressCleanupCandidate {
	var candidates []ipv4AddressCleanupCandidate
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		ifname := fields[0]
		if i := strings.Index(ifname, "@"); i >= 0 {
			ifname = ifname[:i]
		}
		for i := 2; i < len(fields); i++ {
			field := fields[i]
			if i+2 < len(fields) && fields[i+1] == "peer" && staleDSLiteIPv4Address(field) {
				candidates = append(candidates, ipv4AddressCleanupCandidate{ifname: ifname, address: field, peer: fields[i+2]})
				continue
			}
			if strings.Contains(field, "/") && staleDSLiteIPv4Address(field) {
				candidates = append(candidates, ipv4AddressCleanupCandidate{ifname: ifname, address: field})
			}
		}
	}
	return candidates
}

func parseIfconfigIPv4AddressArtifacts(output string) []resource.Artifact {
	var artifacts []resource.Artifact
	var ifname string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !strings.HasPrefix(line, "\t") && !strings.HasPrefix(line, " ") {
			fields := strings.Fields(line)
			if len(fields) == 0 {
				ifname = ""
				continue
			}
			ifname = strings.TrimSuffix(fields[0], ":")
			continue
		}
		if ifname == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet" {
			continue
		}
		address := fields[1]
		for i, field := range fields {
			if field == "netmask" && i+1 < len(fields) {
				if prefix := freeBSDIPv4MaskPrefixForCleanup(fields[i+1]); prefix != "" {
					address += "/" + prefix
				}
				break
			}
		}
		artifacts = append(artifacts, resource.Artifact{
			Kind: "net.ipv4.address",
			Name: ifname + ":" + address,
		})
	}
	return artifacts
}

func freeBSDIPv4MaskPrefixForCleanup(mask string) string {
	mask = strings.TrimSpace(strings.ToLower(mask))
	if strings.HasPrefix(mask, "0x") {
		value, err := strconv.ParseUint(strings.TrimPrefix(mask, "0x"), 16, 32)
		if err != nil {
			return ""
		}
		bits := 0
		for i := 31; i >= 0; i-- {
			if value&(1<<uint(i)) == 0 {
				break
			}
			bits++
		}
		return strconv.Itoa(bits)
	}
	ip := net.ParseIP(mask).To4()
	if ip == nil {
		return ""
	}
	bits, _ := net.IPMask(ip).Size()
	if bits < 0 {
		return ""
	}
	return strconv.Itoa(bits)
}

func parseIfconfigBlocks(output string) map[string]string {
	blocks := map[string]string{}
	current := ""
	var lines []string
	flush := func() {
		if current != "" {
			blocks[current] = strings.Join(lines, "\n")
		}
	}
	header := regexp.MustCompile(`^([A-Za-z0-9_.:-]+):\s+flags=`)
	for _, line := range strings.Split(output, "\n") {
		if match := header.FindStringSubmatch(line); match != nil {
			flush()
			current = match[1]
			lines = []string{line}
			continue
		}
		if current != "" {
			lines = append(lines, line)
		}
	}
	flush()
	return blocks
}

func looksRouterdDSLiteTunnelName(name string) bool {
	return name == "ds-routerd" || strings.HasPrefix(name, "ds-lite")
}

func looksFreeBSDDSLiteTunnel(name, block string) bool {
	if !strings.HasPrefix(name, "gif") {
		return false
	}
	return strings.Contains(block, "tunnel inet6 ") &&
		strings.Contains(block, " --> ") &&
		strings.Contains(block, "inet ") &&
		strings.Contains(block, "--> 192.0.0.1")
}

func rememberAppliedArtifacts(router *api.Router, ledgerPath string, generation int64) (int, error) {
	if ledgerPath == "" {
		return 0, nil
	}
	engine := apply.New()
	artifacts, err := engine.AppliedOwnedArtifacts(router)
	if err != nil {
		return 0, err
	}
	ledger, err := resource.LoadLedger(ledgerPath)
	if err != nil {
		return 0, err
	}
	if sqliteLedger, ok := ledger.(interface{ SetGeneration(int64) }); ok {
		sqliteLedger.SetGeneration(generation)
	}
	ledger.Remember(artifacts)
	if err := ledger.Save(ledgerPath); err != nil {
		return 0, err
	}
	return len(adoptedArtifactsForResult(artifacts)), nil
}

func recordLastAppliedPath(router *api.Router, store routerstate.Store, path string) error {
	if path == "" {
		return nil
	}
	applySourceStore, ok := store.(routerstate.ObjectApplySourceStore)
	if !ok {
		return nil
	}
	for _, res := range router.Spec.Resources {
		if err := applySourceStore.SaveObjectApplySource(res.APIVersion, res.Kind, res.Metadata.Name, path); err != nil {
			return err
		}
	}
	return nil
}

func controllerDefaultStatuses() []controlapi.ControllerStatus {
	names := []string{
		"address",
		"bgp",
		"dhcpv4client",
		"dhcpv6",
		"dns-resolver",
		"dslite",
		"firewall",
		"ingress",
		"kernel-module",
		"nat",
		"network-adoption",
		"package",
		"pppoesession",
		"route",
		"service-unit",
		"vrrp",
	}
	out := make([]controlapi.ControllerStatus, 0, len(names))
	for _, name := range names {
		out = append(out, controlapi.ControllerStatus{
			Name:          name,
			Mode:          "live",
			Reason:        controlapi.ControllerModeReasonLive,
			Message:       "controller is reconciling declared router state",
			ResourceKinds: controllerResourceKinds(name),
		})
	}
	return out
}
