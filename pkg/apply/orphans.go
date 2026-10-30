// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/resource"
)

func (e *Engine) observeManagedOrphans(router *api.Router, aliases map[string]string) []OrphanedArtifact {
	var orphans []OrphanedArtifact
	orphans = append(orphans, e.observeNftTableOrphans(router, aliases)...)
	return orphans
}

func (e *Engine) AdoptionCandidates(router *api.Router, ledger resource.Ledger) ([]AdoptionCandidate, error) {
	candidates, _, err := e.AdoptionCandidateArtifacts(router, ledger)
	return candidates, err
}

func (e *Engine) AdoptionCandidateArtifacts(router *api.Router, ledger resource.Ledger) ([]AdoptionCandidate, []resource.Artifact, error) {
	if err := e.Validate(router); err != nil {
		return nil, nil, err
	}
	if ledger == nil {
		ledger = resource.NewLedger()
	}
	aliases := interfaceAliases(router)
	desired := DesiredOwnedArtifacts(router, aliases)
	actual := e.actualInventoryBackedArtifacts()
	actualByID := map[string]resource.Artifact{}
	for _, artifact := range actual {
		actualByID[artifact.Identity()] = artifact
	}
	var candidates []AdoptionCandidate
	var artifacts []resource.Artifact
	seen := map[string]bool{}
	for _, artifact := range desired {
		id := artifact.Identity()
		if seen[id] {
			continue
		}
		owns, err := ledger.Owns(artifact)
		if err != nil {
			return nil, nil, err
		}
		if owns {
			continue
		}
		actualArtifact, ok := actualByID[id]
		if !ok {
			continue
		}
		seen[id] = true
		reason := "desired artifact exists on host but is not recorded in the local ownership ledger"
		if desiredAttributesDrift(artifact.Attributes, actualArtifact.Attributes) {
			reason = "desired artifact identity exists on host but observed attributes differ from desired state"
		}
		candidates = append(candidates, AdoptionCandidate{
			Kind:     artifact.Kind,
			Name:     artifact.Name,
			Owner:    artifact.Owner,
			Reason:   reason,
			Desired:  artifact.Attributes,
			Observed: actualArtifact.Attributes,
		})
		artifacts = append(artifacts, mergeArtifactAttributes(artifact, actualArtifact))
	}
	if err := e.operationError(); err != nil {
		return nil, nil, err
	}
	return candidates, artifacts, nil
}

func (e *Engine) DesiredOwnedArtifacts(router *api.Router) ([]resource.Artifact, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	return DesiredOwnedArtifacts(router, interfaceAliases(router)), nil
}

func (e *Engine) AppliedOwnedArtifacts(router *api.Router) ([]resource.Artifact, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	aliases := interfaceAliases(router)
	desired := DesiredOwnedArtifacts(router, aliases)
	actualByID := map[string]resource.Artifact{}
	for _, artifact := range e.actualInventoryBackedArtifacts() {
		actualByID[artifact.Identity()] = artifact
	}
	var artifacts []resource.Artifact
	seen := map[string]bool{}
	for _, artifact := range desired {
		id := artifact.Identity()
		if seen[id] {
			continue
		}
		actual, ok := actualByID[id]
		if !ok {
			continue
		}
		seen[id] = true
		artifacts = append(artifacts, mergeArtifactAttributes(artifact, actual))
	}
	if err := e.operationError(); err != nil {
		return nil, err
	}
	return artifacts, nil
}

func (e *Engine) LedgerOwnedOrphans(router *api.Router, ledger resource.Ledger) ([]OrphanedArtifact, []resource.Artifact, error) {
	return e.LedgerOwnedOrphansEffective(router, router, ledger)
}

// LedgerOwnedOrphansEffective validates the declared graph while computing
// orphan candidates only from the effective graph.
func (e *Engine) LedgerOwnedOrphansEffective(effective, declared *api.Router, ledger resource.Ledger) ([]OrphanedArtifact, []resource.Artifact, error) {
	plan, err := e.LedgerOwnedOrphanPlanEffective(effective, declared, ledger)
	if err != nil {
		return nil, nil, err
	}
	var result []OrphanedArtifact
	var artifacts []resource.Artifact
	for _, removal := range plan.ArtifactRemovals {
		result = append(result, orphanedArtifactFromLifecycle(removal))
		artifacts = append(artifacts, removal.Artifact)
	}
	return result, artifacts, nil
}

func (e *Engine) LedgerOwnedOrphanPlan(router *api.Router, ledger resource.Ledger) (lifecycle.GCPlan, error) {
	return e.LedgerOwnedOrphanPlanEffective(router, router, ledger)
}

// LedgerOwnedOrphanPlanEffective validates the declared graph while deriving
// desired artifacts from the effective graph.
func (e *Engine) LedgerOwnedOrphanPlanEffective(effective, declared *api.Router, ledger resource.Ledger) (lifecycle.GCPlan, error) {
	if err := e.Validate(declared); err != nil {
		return lifecycle.GCPlan{}, err
	}
	if ledger == nil {
		return lifecycle.GCPlan{}, nil
	}
	aliases := interfaceAliases(effective)
	desired := DesiredOwnedArtifacts(effective, aliases)
	owned, err := ledger.All()
	if err != nil {
		return lifecycle.GCPlan{}, err
	}
	host := e.actualInventoryBackedArtifacts()
	if err := e.operationError(); err != nil {
		return lifecycle.GCPlan{}, err
	}
	return lifecycle.PlanArtifactOrphans(lifecycle.GCPlanInput{
		DesiredArtifacts: desired,
		LedgerArtifacts:  owned,
		HostArtifacts:    host,
	}), nil
}

func orphanedArtifactFromLifecycle(removal lifecycle.GCArtifactRemoval) OrphanedArtifact {
	return OrphanedArtifact{
		Kind:        removal.Artifact.Kind,
		Name:        removal.Artifact.Name,
		Owner:       removal.Artifact.Owner,
		Reason:      removal.Reason,
		Remediation: removal.Remediation,
		Observed:    removal.Artifact.Attributes,
	}
}

func desiredAttributesDrift(desired, actual map[string]string) bool {
	for key, desiredValue := range desired {
		if actual[key] != desiredValue {
			return true
		}
	}
	return false
}

func DesiredOwnedArtifacts(router *api.Router, aliases map[string]string) []resource.Artifact {
	var desired []resource.Artifact
	for _, intent := range routerArtifactIntents(router, aliases) {
		switch intent.Artifact.Kind {
		case "linux.ipv4.routeTable":
			continue
		default:
			desired = append(desired, intent.Artifact)
		}
	}
	for _, res := range router.Spec.Resources {
		for _, intent := range resourceArtifactIntents(res, aliases) {
			switch intent.Artifact.Kind {
			case "nft.table",
				"systemd.service",
				"file",
				"unix.socket",
				"directory",
				"host.sysctl",
				"host.hostname",
				"net.link",
				"net.ipv4.address",
				"net.ipv6.address",
				"linux.ipip6.tunnel":
				if intent.Action == resource.ActionEnsure {
					desired = append(desired, intent.Artifact)
				}
			}
		}
	}
	return desired
}

func mergeArtifactAttributes(desired, actual resource.Artifact) resource.Artifact {
	merged := desired
	attrs := map[string]string{}
	for key, value := range actual.Attributes {
		attrs[key] = value
	}
	for key, value := range desired.Attributes {
		attrs[key] = value
	}
	merged.Attributes = attrs
	return merged
}

func (e *Engine) actualInventoryBackedArtifacts() []resource.Artifact {
	var actual []resource.Artifact
	if out, err := e.Command("nft", "list", "tables"); err == nil {
		actual = append(actual, parseNftTableArtifacts(string(out))...)
	}
	actual = append(actual, e.actualSystemdServiceArtifacts()...)
	actual = append(actual, e.actualFileArtifacts()...)
	actual = append(actual, e.actualUnixSocketArtifacts()...)
	actual = append(actual, e.actualDirectoryArtifacts()...)
	actual = append(actual, e.actualSysctlArtifacts()...)
	actual = append(actual, e.actualHostnameArtifacts()...)
	actual = append(actual, e.actualLinkArtifacts()...)
	actual = append(actual, e.actualAddressArtifacts()...)
	actual = append(actual, e.actualIPIP6TunnelArtifacts()...)
	return actual
}

func (e *Engine) actualSystemdServiceArtifacts() []resource.Artifact {
	names := []string{
		"routerd-dnsmasq.service",
	}
	if out, err := e.Command("systemctl", "list-unit-files", "routerd-*.service", "--no-legend", "--no-pager"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 0 && strings.HasPrefix(fields[0], "routerd-") && strings.HasSuffix(fields[0], ".service") {
				names = append(names, fields[0])
			}
		}
	}
	seen := map[string]bool{}
	var artifacts []resource.Artifact
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		if _, err := e.Command("systemctl", "cat", name); err == nil {
			artifacts = append(artifacts, newSimpleArtifact("systemd.service", name, "systemd"))
		}
	}
	return artifacts
}

func (e *Engine) actualFileArtifacts() []resource.Artifact {
	paths := []string{
		"/etc/ppp/chap-secrets",
		"/etc/ppp/pap-secrets",
		"/usr/local/etc/routerd/dnsmasq.conf",
		"/usr/local/etc/routerd/nftables.nft",
		"/usr/local/etc/routerd/default-route.nft",
	}
	if out, err := e.Command("find", "/etc/ppp/peers", "-maxdepth", "1", "-type", "f", "-name", "routerd-*", "-print"); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			path := strings.TrimSpace(line)
			if path != "" {
				paths = append(paths, path)
			}
		}
	}
	var artifacts []resource.Artifact
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if _, err := e.Command("test", "-f", path); err == nil {
			artifacts = append(artifacts, newSimpleArtifact("file", path, "file"))
		}
	}
	return artifacts
}

func (e *Engine) actualUnixSocketArtifacts() []resource.Artifact {
	out, err := e.Command("find", "/run/routerd/pppoe-client", "-maxdepth", "1", "-type", "s", "-name", "*.sock", "-print")
	if err != nil {
		return nil
	}
	var artifacts []resource.Artifact
	for _, line := range strings.Split(string(out), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}
		artifacts = append(artifacts, newSimpleArtifact("unix.socket", path, "file"))
	}
	return artifacts
}

func (e *Engine) actualDirectoryArtifacts() []resource.Artifact {
	var artifacts []resource.Artifact
	for _, root := range []string{"/run/routerd/pppoe-client", "/var/lib/routerd/pppoe-client"} {
		out, err := e.Command("find", root, "-mindepth", "1", "-maxdepth", "1", "-type", "d", "-print")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			path := strings.TrimSpace(line)
			if path == "" {
				continue
			}
			artifacts = append(artifacts, newSimpleArtifact("directory", path, "file"))
		}
	}
	return artifacts
}

func (e *Engine) actualSysctlArtifacts() []resource.Artifact {
	keys := []string{
		"net.ipv4.ip_forward",
		"net.ipv6.conf.all.forwarding",
		"net.ipv4.conf.all.rp_filter",
		"net.ipv4.conf.default.rp_filter",
	}
	if out, err := e.Command("ls", "/proc/sys/net/ipv4/conf"); err == nil {
		for _, ifname := range strings.Fields(string(out)) {
			keys = append(keys, "net.ipv4.conf."+ifname+".rp_filter")
		}
	}
	var artifacts []resource.Artifact
	for _, key := range keys {
		if out, err := e.Command("sysctl", "-n", key); err == nil {
			artifacts = append(artifacts, resource.Artifact{
				Kind: "host.sysctl",
				Name: key,
				Attributes: map[string]string{
					"value": strings.TrimSpace(string(out)),
				},
			})
		}
	}
	return artifacts
}

func (e *Engine) actualHostnameArtifacts() []resource.Artifact {
	if out, err := e.Command("hostname"); err == nil && strings.TrimSpace(string(out)) != "" {
		return []resource.Artifact{{
			Kind: "host.hostname",
			Name: "system",
			Attributes: map[string]string{
				"hostname": strings.TrimSpace(string(out)),
			},
		}}
	}
	return nil
}

func (e *Engine) actualLinkArtifacts() []resource.Artifact {
	out, err := e.Command("ip", "-brief", "link", "show")
	source := "ip-link"
	if err != nil {
		out, err = e.Command("ifconfig", "-l")
		source = "ifconfig"
		if err != nil {
			return nil
		}
		var artifacts []resource.Artifact
		for _, name := range strings.Fields(string(out)) {
			artifacts = append(artifacts, newSimpleArtifact("net.link", name, source))
		}
		return artifacts
	}
	var artifacts []resource.Artifact
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 1 {
			continue
		}
		name := strings.TrimSuffix(fields[0], "@")
		if i := strings.Index(name, "@"); i >= 0 {
			name = name[:i]
		}
		artifacts = append(artifacts, newSimpleArtifact("net.link", name, source))
	}
	return artifacts
}

func (e *Engine) actualAddressArtifacts() []resource.Artifact {
	var artifacts []resource.Artifact
	observedWithIP := false
	if out, err := e.Command("ip", "-brief", "-4", "addr", "show"); err == nil {
		artifacts = append(artifacts, parseBriefAddressArtifacts("net.ipv4.address", string(out))...)
		observedWithIP = true
	}
	if out, err := e.Command("ip", "-brief", "-6", "addr", "show"); err == nil {
		artifacts = append(artifacts, parseBriefAddressArtifacts("net.ipv6.address", string(out))...)
		observedWithIP = true
	}
	if !observedWithIP {
		if out, err := e.Command("ifconfig"); err == nil {
			artifacts = append(artifacts, parseIfconfigAddressArtifacts(string(out))...)
		}
	}
	return artifacts
}

func (e *Engine) actualIPIP6TunnelArtifacts() []resource.Artifact {
	out, err := e.Command("ip", "-d", "link", "show", "type", "ip6tnl")
	if err != nil {
		return nil
	}
	var artifacts []resource.Artifact
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) >= 2 && strings.HasSuffix(fields[0], ":") {
			name := strings.TrimSuffix(fields[1], ":")
			if i := strings.Index(name, "@"); i >= 0 {
				name = name[:i]
			}
			artifacts = append(artifacts, newSimpleArtifact("linux.ipip6.tunnel", name, "ip-link"))
		}
	}
	return artifacts
}

func (e *Engine) observeNftTableOrphans(router *api.Router, aliases map[string]string) []OrphanedArtifact {
	desired := desiredArtifactsByKind(router, aliases, "nft.table")
	out, err := e.Command("nft", "list", "tables")
	if err != nil {
		return nil
	}
	var orphans []OrphanedArtifact
	for _, artifact := range resource.Orphans(desired, parseNftTableArtifacts(string(out)), managedNftTableArtifact) {
		family := artifact.Attributes["family"]
		name := artifact.Attributes["name"]
		orphans = append(orphans, OrphanedArtifact{
			Kind:        "NftTable",
			Name:        artifact.Name,
			Reason:      "no current resource owns this routerd-managed nftables table",
			Remediation: "delete nft table " + family + " " + name,
			Observed: map[string]string{
				"family": family,
				"name":   name,
			},
		})
	}
	return orphans
}

func desiredArtifactsByKind(router *api.Router, aliases map[string]string, kind string) []resource.Artifact {
	var desired []resource.Artifact
	for _, intent := range routerArtifactIntents(router, aliases) {
		if intent.Artifact.Kind == kind && intent.Action != resource.ActionDelete {
			desired = append(desired, intent.Artifact)
		}
	}
	for _, res := range router.Spec.Resources {
		for _, intent := range resourceArtifactIntents(res, aliases) {
			if intent.Artifact.Kind == kind && intent.Action != resource.ActionDelete {
				desired = append(desired, intent.Artifact)
			}
		}
	}
	return desired
}

func parseNftTableArtifacts(output string) []resource.Artifact {
	var artifacts []resource.Artifact
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 || fields[0] != "table" {
			continue
		}
		artifacts = append(artifacts, newNftTableArtifact("", fields[1], fields[2]))
	}
	return artifacts
}

func parseBriefAddressArtifacts(kind, output string) []resource.Artifact {
	var artifacts []resource.Artifact
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		ifname := fields[0]
		if i := strings.Index(ifname, "@"); i >= 0 {
			ifname = ifname[:i]
		}
		for _, field := range fields[2:] {
			if strings.Contains(field, "/") {
				attrs := map[string]string{
					"ifname":  ifname,
					"address": field,
				}
				if kind == "net.ipv4.address" {
					if peer := peerForBriefAddressField(fields, field); peer != "" {
						attrs["peer"] = peer
					}
				}
				artifacts = append(artifacts, resource.Artifact{
					Kind:       kind,
					Name:       ifname + ":" + field,
					Attributes: attrs,
				})
			}
		}
	}
	return artifacts
}

func peerForBriefAddressField(fields []string, address string) string {
	for i := 2; i+2 < len(fields); i++ {
		if fields[i] == address && fields[i+1] == "peer" {
			return fields[i+2]
		}
	}
	return ""
}

func parseIfconfigAddressArtifacts(output string) []resource.Artifact {
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
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "inet":
			address := fields[1]
			prefix := ""
			for i, field := range fields {
				if field == "netmask" && i+1 < len(fields) {
					prefix = freeBSDIPv4MaskPrefix(fields[i+1])
					break
				}
			}
			if prefix != "" {
				address += "/" + prefix
			}
			artifacts = append(artifacts, addressArtifact("net.ipv4.address", ifname, address, "ifconfig"))
		case "inet6":
			address := strings.SplitN(fields[1], "%", 2)[0]
			prefix := ""
			for i, field := range fields {
				if field == "prefixlen" && i+1 < len(fields) {
					prefix = fields[i+1]
					break
				}
			}
			if prefix != "" {
				address += "/" + prefix
			}
			artifacts = append(artifacts, addressArtifact("net.ipv6.address", ifname, address, "ifconfig"))
		}
	}
	return artifacts
}

func addressArtifact(kind, ifname, address, source string) resource.Artifact {
	return resource.Artifact{
		Kind: kind,
		Name: ifname + ":" + address,
		Attributes: map[string]string{
			"ifname":  ifname,
			"address": address,
			"source":  source,
		},
	}
}

func freeBSDIPv4MaskPrefix(mask string) string {
	mask = strings.TrimPrefix(mask, "0x")
	if len(mask) != 8 {
		return ""
	}
	value, err := strconv.ParseUint(mask, 16, 32)
	if err != nil {
		return ""
	}
	prefix := 0
	for i := 31; i >= 0; i-- {
		if value&(1<<uint(i)) == 0 {
			break
		}
		prefix++
	}
	return fmt.Sprintf("%d", prefix)
}

func newSimpleArtifact(kind, name, source string) resource.Artifact {
	return resource.Artifact{
		Kind: kind,
		Name: name,
		Attributes: map[string]string{
			"source": source,
		},
	}
}

func newNftTableArtifact(owner, family, name string) resource.Artifact {
	return resource.Artifact{
		Kind:  "nft.table",
		Name:  family + "/" + name,
		Owner: owner,
		Attributes: map[string]string{
			"family": family,
			"name":   name,
		},
	}
}

func managedNftTableArtifact(artifact resource.Artifact) bool {
	return strings.HasPrefix(artifact.Attributes["name"], "routerd_")
}
