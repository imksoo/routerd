// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/resource"
)

type ipv4FwmarkRuleArtifact struct {
	Priority int
	Mark     int
	Table    int
}

func (e *Engine) observeManagedOrphans(router *api.Router, aliases map[string]string) []OrphanedArtifact {
	var orphans []OrphanedArtifact
	orphans = append(orphans, e.observeIPv4FwmarkRuleOrphans(router)...)
	orphans = append(orphans, e.observeIPv4RouteTableOrphans(router, aliases)...)
	orphans = append(orphans, e.observeNftTableOrphans(router, aliases)...)
	return orphans
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
	return lifecycle.PlanArtifactOrphans(lifecycle.GCPlanInput{
		DesiredArtifacts: desired,
		LedgerArtifacts:  owned,
		HostArtifacts:    e.actualInventoryBackedArtifacts(),
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
			case "linux.ipv4.fwmarkRule",
				"linux.ipv4.routeTable",
				"nft.table",
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

func (e *Engine) actualInventoryBackedArtifacts() []resource.Artifact {
	var actual []resource.Artifact
	if out, err := runCommand("ip", "-N", "-4", "rule", "show"); err == nil {
		actual = append(actual, parseIPv4FwmarkRuleArtifacts(string(out))...)
	}
	if out, err := runCommand("ip", "-N", "-4", "route", "show", "table", "all"); err == nil {
		actual = append(actual, parseIPv4RouteTableArtifacts(string(out))...)
	}
	if out, err := runCommand("nft", "list", "tables"); err == nil {
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
	if out, err := runCommand("systemctl", "list-unit-files", "routerd-*.service", "--no-legend", "--no-pager"); err == nil {
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
		if _, err := runCommand("systemctl", "cat", name); err == nil {
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
	if out, err := runCommand("find", "/etc/ppp/peers", "-maxdepth", "1", "-type", "f", "-name", "routerd-*", "-print"); err == nil {
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
		if _, err := runCommand("test", "-f", path); err == nil {
			artifacts = append(artifacts, newSimpleArtifact("file", path, "file"))
		}
	}
	return artifacts
}

func (e *Engine) actualUnixSocketArtifacts() []resource.Artifact {
	out, err := runCommand("find", "/run/routerd/pppoe-client", "-maxdepth", "1", "-type", "s", "-name", "*.sock", "-print")
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
		out, err := runCommand("find", root, "-mindepth", "1", "-maxdepth", "1", "-type", "d", "-print")
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
	if out, err := runCommand("ls", "/proc/sys/net/ipv4/conf"); err == nil {
		for _, ifname := range strings.Fields(string(out)) {
			keys = append(keys, "net.ipv4.conf."+ifname+".rp_filter")
		}
	}
	var artifacts []resource.Artifact
	for _, key := range keys {
		if out, err := runCommand("sysctl", "-n", key); err == nil {
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
	if out, err := runCommand("hostname"); err == nil && strings.TrimSpace(string(out)) != "" {
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
	out, err := runCommand("ip", "-brief", "link", "show")
	source := "ip-link"
	if err != nil {
		out, err = runCommand("ifconfig", "-l")
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
	if out, err := runCommand("ip", "-brief", "-4", "addr", "show"); err == nil {
		artifacts = append(artifacts, parseBriefAddressArtifacts("net.ipv4.address", string(out))...)
		observedWithIP = true
	}
	if out, err := runCommand("ip", "-brief", "-6", "addr", "show"); err == nil {
		artifacts = append(artifacts, parseBriefAddressArtifacts("net.ipv6.address", string(out))...)
		observedWithIP = true
	}
	if !observedWithIP {
		if out, err := runCommand("ifconfig"); err == nil {
			artifacts = append(artifacts, parseIfconfigAddressArtifacts(string(out))...)
		}
	}
	return artifacts
}

func (e *Engine) actualIPIP6TunnelArtifacts() []resource.Artifact {
	out, err := runCommand("ip", "-j", "-6", "tunnel", "show")
	if err != nil {
		return nil
	}
	var tunnels []struct {
		Name string `json:"ifname"`
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(out, &tunnels); err != nil {
		return nil
	}
	var artifacts []resource.Artifact
	for _, tunnel := range tunnels {
		if tunnel.Mode == "ip/ipv6" && tunnel.Name != "" {
			artifacts = append(artifacts, newSimpleArtifact("linux.ipip6.tunnel", tunnel.Name, "ip-link"))
		}
	}
	return artifacts
}

func (e *Engine) observeIPv4FwmarkRuleOrphans(router *api.Router) []OrphanedArtifact {
	desired := desiredIPv4FwmarkRuleArtifacts(router)
	out, err := runCommand("ip", "-N", "-4", "rule", "show")
	if err != nil {
		return nil
	}
	var orphans []OrphanedArtifact
	actual := parseIPv4FwmarkRuleArtifacts(string(out))
	for _, artifact := range resource.Orphans(desired, actual, managedIPv4FwmarkRuleArtifact) {
		rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
		if !ok {
			continue
		}
		orphans = append(orphans, OrphanedArtifact{
			Kind:        "IPv4FwmarkRule",
			Name:        fmt.Sprintf("priority=%d,mark=0x%x,table=%d", rule.Priority, rule.Mark, rule.Table),
			Reason:      "no current resource owns this routerd-managed fwmark rule",
			Remediation: fmt.Sprintf("delete ip rule priority %d fwmark 0x%x table %d; flush table %d if unused", rule.Priority, rule.Mark, rule.Table, rule.Table),
			Observed: map[string]string{
				"priority": fmt.Sprintf("%d", rule.Priority),
				"mark":     fmt.Sprintf("0x%x", rule.Mark),
				"table":    fmt.Sprintf("%d", rule.Table),
			},
		})
	}
	return orphans
}

// IPv4RuleTableReferences counts numeric table references in ip rule output.
func IPv4RuleTableReferences(output string) map[int]int {
	references := map[int]int{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		for i, field := range fields {
			if field == "lookup" && i+1 < len(fields) {
				if table, err := strconv.Atoi(fields[i+1]); err == nil {
					references[table]++
				}
			}
		}
	}
	return references
}

// DesiredIPv4PolicyRouteTables identifies configured policy and VRF tables.
func DesiredIPv4PolicyRouteTables(router *api.Router, aliases map[string]string) (map[int]bool, error) {
	tables := map[int]bool{}
	for _, artifact := range DesiredOwnedArtifacts(router, aliases) {
		if artifact.Kind == "linux.ipv4.routeTable" {
			if table, err := strconv.Atoi(artifact.Attributes["table"]); err == nil {
				tables[table] = true
			}
		}
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != "VRF" {
			continue
		}
		spec, err := res.VRFSpec()
		if err != nil {
			return nil, err
		}
		tables[spec.RouteTable] = true
	}
	return tables, nil
}

// IPv4TailscaleRouteTables identifies active routes from a configured Tailscale node.
func IPv4TailscaleRouteTables(router *api.Router, output string) (map[int]bool, error) {
	tables := map[int]bool{}
	configured := false
	for _, res := range router.Spec.Resources {
		if res.Kind != "TailscaleNode" {
			continue
		}
		spec, err := res.TailscaleNodeSpec()
		if err != nil {
			return nil, err
		}
		if spec.State != "absent" {
			configured = true
		}
	}
	if !configured {
		return tables, nil
	}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if routeLineDev(fields) != "tailscale0" {
			continue
		}
		if table, ok := routeLineTable(fields); ok {
			tables[table] = true
		}
	}
	return tables, nil
}

func (e *Engine) observeIPv4RouteTableOrphans(router *api.Router, aliases map[string]string) []OrphanedArtifact {
	desiredTables, err := DesiredIPv4PolicyRouteTables(router, aliases)
	if err != nil {
		return nil
	}
	rules, err := runCommand("ip", "-N", "-4", "rule", "show")
	if err != nil {
		return nil
	}
	references := IPv4RuleTableReferences(string(rules))
	desiredRules := map[string]bool{}
	for _, artifact := range desiredIPv4FwmarkRuleArtifacts(router) {
		desiredRules[artifact.Identity()] = true
	}
	seenRules := map[string]bool{}
	for _, artifact := range parseIPv4FwmarkRuleArtifacts(string(rules)) {
		id := artifact.Identity()
		if desiredRules[id] || seenRules[id] {
			continue
		}
		seenRules[id] = true
		if rule, ok := ipv4FwmarkRuleFromArtifact(artifact); ok {
			references[rule.Table]--
		}
	}
	out, err := runCommand("ip", "-N", "-4", "route", "show", "table", "all")
	if err != nil {
		return nil
	}
	dynamicTables, err := IPv4TailscaleRouteTables(router, string(out))
	if err != nil {
		return nil
	}
	var orphans []OrphanedArtifact
	for _, artifact := range parseIPv4RouteTableArtifacts(string(out)) {
		table, err := strconv.Atoi(artifact.Attributes["table"])
		if err != nil || table == 253 || table == 254 || table == 255 || desiredTables[table] || dynamicTables[table] || references[table] > 0 {
			continue
		}
		orphans = append(orphans, OrphanedArtifact{
			Kind:        "IPv4RouteTable",
			Name:        artifact.Name,
			Reason:      "no configured policy or live rule references this IPv4 route table",
			Remediation: "flush ip route table " + strconv.Itoa(table),
			Observed:    map[string]string{"table": strconv.Itoa(table)},
		})
	}
	return orphans
}

func (e *Engine) observeNftTableOrphans(router *api.Router, aliases map[string]string) []OrphanedArtifact {
	desired := desiredArtifactsByKind(router, aliases, "nft.table")
	out, err := runCommand("nft", "list", "tables")
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

func desiredIPv4FwmarkRuleArtifacts(router *api.Router) []resource.Artifact {
	var desired []resource.Artifact
	add := func(owner string, priority, mark, table int) {
		if priority == 0 || mark == 0 || table == 0 {
			return
		}
		desired = append(desired, newIPv4FwmarkRuleArtifact(owner, priority, mark, table))
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "EgressRoutePolicy":
			spec, err := res.EgressRoutePolicySpec()
			if err != nil {
				continue
			}
			if defaultString(spec.Mode, "") == "priority" && defaultString(spec.Selection, "highest-weight-ready") != "highest-weight-ready" {
				continue
			}
			for _, candidate := range spec.Candidates {
				if !api.BoolDefault(candidate.Enabled, true) {
					continue
				}
				if len(candidate.Targets) > 0 {
					for _, target := range candidate.Targets {
						add(res.ID(), target.Priority, target.Mark, target.EffectiveTable())
					}
					continue
				}
				add(res.ID(), candidate.Priority, candidate.Mark, candidate.EffectiveTable())
			}
		}
	}
	return desired
}

func parseIPv4FwmarkRuleArtifacts(output string) []resource.Artifact {
	var rules []resource.Artifact
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 7 || fields[1] != "from" || fields[2] != "all" || fields[3] != "fwmark" || fields[5] != "lookup" {
			continue
		}
		priority, priorityErr := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
		mark, markErr := strconv.ParseInt(fields[4], 0, 64)
		table, tableErr := strconv.Atoi(fields[6])
		if priorityErr != nil || markErr != nil || tableErr != nil || mark == 0 || table == 0 {
			continue
		}
		rules = append(rules, newIPv4FwmarkRuleArtifact("", priority, int(mark), table))
	}
	return rules
}

func parseIPv4RouteTableArtifacts(output string) []resource.Artifact {
	type tableObservation struct {
		Table       int
		IfName      string
		HasDefault  bool
		HasAnyRoute bool
	}
	byTable := map[int]tableObservation{}
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		table, ok := routeLineTable(fields)
		if !ok {
			continue
		}
		observation := byTable[table]
		observation.Table = table
		observation.HasAnyRoute = true
		ifname := routeLineDev(fields)
		isDefault := len(fields) > 0 && fields[0] == "default"
		if ifname != "" && (observation.IfName == "" || isDefault || !observation.HasDefault) {
			observation.IfName = ifname
		}
		if isDefault {
			observation.HasDefault = true
		}
		byTable[table] = observation
	}
	var tables []int
	for table := range byTable {
		tables = append(tables, table)
	}
	sort.Ints(tables)
	var artifacts []resource.Artifact
	for _, table := range tables {
		observation := byTable[table]
		artifacts = append(artifacts, newIPv4RouteTableArtifactWithIfName("", table, observation.IfName))
	}
	return artifacts
}

func routeLineTable(fields []string) (int, bool) {
	for i, field := range fields {
		if field != "table" || i+1 >= len(fields) {
			continue
		}
		table, err := strconv.Atoi(fields[i+1])
		if err != nil {
			return 0, false
		}
		return table, true
	}
	return 0, false
}

func routeLineDev(fields []string) string {
	for i, field := range fields {
		if field == "dev" && i+1 < len(fields) {
			return fields[i+1]
		}
	}
	return ""
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

func newIPv4FwmarkRuleArtifact(owner string, priority, mark, table int) resource.Artifact {
	return resource.Artifact{
		Kind:  "linux.ipv4.fwmarkRule",
		Name:  fmt.Sprintf("priority=%d,mark=0x%x,table=%d", priority, mark, table),
		Owner: owner,
		Attributes: map[string]string{
			"priority": fmt.Sprintf("%d", priority),
			"mark":     fmt.Sprintf("0x%x", mark),
			"table":    fmt.Sprintf("%d", table),
		},
	}
}

func newIPv4RouteTableArtifactWithIfName(owner string, table int, ifname string) resource.Artifact {
	attrs := map[string]string{
		"table": fmt.Sprintf("%d", table),
	}
	if ifname != "" {
		attrs["ifname"] = ifname
	}
	return resource.Artifact{
		Kind:       "linux.ipv4.routeTable",
		Name:       fmt.Sprintf("table=%d", table),
		Owner:      owner,
		Attributes: attrs,
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

func ipv4FwmarkRuleFromArtifact(artifact resource.Artifact) (ipv4FwmarkRuleArtifact, bool) {
	priority, err := strconv.Atoi(artifact.Attributes["priority"])
	if err != nil {
		return ipv4FwmarkRuleArtifact{}, false
	}
	mark, err := strconv.ParseInt(artifact.Attributes["mark"], 0, 64)
	if err != nil {
		return ipv4FwmarkRuleArtifact{}, false
	}
	table, err := strconv.Atoi(artifact.Attributes["table"])
	if err != nil {
		return ipv4FwmarkRuleArtifact{}, false
	}
	return ipv4FwmarkRuleArtifact{Priority: priority, Mark: int(mark), Table: table}, true
}

func managedIPv4FwmarkRuleArtifact(artifact resource.Artifact) bool {
	_, ok := ipv4FwmarkRuleFromArtifact(artifact)
	return ok
}

func managedNftTableArtifact(artifact resource.Artifact) bool {
	return strings.HasPrefix(artifact.Attributes["name"], "routerd_")
}
