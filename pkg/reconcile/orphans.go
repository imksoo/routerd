package reconcile

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/resource"
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
		if seen[id] || ledger.Owns(artifact) {
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
	return candidates, artifacts, nil
}

func (e *Engine) DesiredOwnedArtifacts(router *api.Router) ([]resource.Artifact, error) {
	if err := e.Validate(router); err != nil {
		return nil, err
	}
	return DesiredOwnedArtifacts(router, interfaceAliases(router)), nil
}

func (e *Engine) ReconciledOwnedArtifacts(router *api.Router) ([]resource.Artifact, error) {
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
	return artifacts, nil
}

func (e *Engine) LedgerOwnedOrphans(router *api.Router, ledger resource.Ledger) ([]OrphanedArtifact, []resource.Artifact, error) {
	if err := e.Validate(router); err != nil {
		return nil, nil, err
	}
	if ledger == nil {
		return nil, nil, nil
	}
	aliases := interfaceAliases(router)
	desired := DesiredOwnedArtifacts(router, aliases)
	desiredIDs := map[string]bool{}
	for _, artifact := range desired {
		desiredIDs[artifact.Identity()] = true
	}
	actualByID := map[string]resource.Artifact{}
	for _, artifact := range e.actualInventoryBackedArtifacts() {
		actualByID[artifact.Identity()] = artifact
	}
	var result []OrphanedArtifact
	var artifacts []resource.Artifact
	seen := map[string]bool{}
	for _, owned := range ledger.All() {
		id := owned.Identity()
		if seen[id] || desiredIDs[id] {
			continue
		}
		seen[id] = true
		actual, ok := actualByID[id]
		if !ok {
			continue
		}
		artifact := mergeArtifactAttributes(owned, actual)
		if !cleanupEligibleLedgerOrphan(artifact) {
			continue
		}
		result = append(result, orphanedArtifactFromLedger(artifact))
		artifacts = append(artifacts, artifact)
	}
	return result, artifacts, nil
}

func cleanupEligibleLedgerOrphan(artifact resource.Artifact) bool {
	switch artifact.Kind {
	case "linux.ipip6.tunnel", "systemd.service":
		return true
	case "nft.table":
		return strings.HasPrefix(artifact.Attributes["name"], "routerd_")
	default:
		return false
	}
}

func orphanedArtifactFromLedger(artifact resource.Artifact) OrphanedArtifact {
	orphan := OrphanedArtifact{
		Kind:     artifact.Kind,
		Name:     artifact.Name,
		Owner:    artifact.Owner,
		Reason:   "local ownership ledger records this artifact but no current resource owns it",
		Observed: artifact.Attributes,
	}
	switch artifact.Kind {
	case "linux.ipip6.tunnel":
		orphan.Remediation = "delete ipip6 tunnel " + artifact.Name
	case "nft.table":
		orphan.Remediation = "delete nft table " + artifact.Attributes["family"] + " " + artifact.Attributes["name"]
	case "systemd.service":
		orphan.Remediation = "disable and stop systemd service " + artifact.Name
	}
	return orphan
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
	for _, res := range router.Spec.Resources {
		for _, intent := range resourceArtifactIntents(res, aliases) {
			switch intent.Artifact.Kind {
			case "linux.ipv4.fwmarkRule",
				"linux.ipv4.routeTable",
				"nft.table",
				"systemd.service",
				"file",
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
	if out, err := e.Command("ip", "-4", "rule", "show"); err == nil {
		actual = append(actual, parseIPv4FwmarkRuleArtifacts(string(out))...)
	}
	if out, err := e.Command("ip", "-4", "route", "show", "table", "all"); err == nil {
		actual = append(actual, parseIPv4RouteTableArtifacts(string(out))...)
	}
	if out, err := e.Command("nft", "list", "tables"); err == nil {
		actual = append(actual, parseNftTableArtifacts(string(out))...)
	}
	actual = append(actual, e.actualSystemdServiceArtifacts()...)
	actual = append(actual, e.actualFileArtifacts()...)
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
	var artifacts []resource.Artifact
	for _, path := range paths {
		if _, err := e.Command("test", "-f", path); err == nil {
			artifacts = append(artifacts, newSimpleArtifact("file", path, "file"))
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

func (e *Engine) observeIPv4FwmarkRuleOrphans(router *api.Router) []OrphanedArtifact {
	desired := desiredIPv4FwmarkRuleArtifacts(router)
	out, err := e.Command("ip", "-4", "rule", "show")
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

func (e *Engine) observeIPv4RouteTableOrphans(router *api.Router, aliases map[string]string) []OrphanedArtifact {
	desired := desiredArtifactsByKind(router, aliases, "linux.ipv4.routeTable")
	out, err := e.Command("ip", "-4", "route", "show", "table", "all")
	if err != nil {
		return nil
	}
	var orphans []OrphanedArtifact
	for _, artifact := range resource.Orphans(desired, parseIPv4RouteTableArtifacts(string(out)), managedIPv4RouteTableArtifact) {
		table := artifact.Attributes["table"]
		orphans = append(orphans, OrphanedArtifact{
			Kind:        "IPv4RouteTable",
			Name:        artifact.Name,
			Reason:      "no current resource owns this routerd-managed IPv4 route table",
			Remediation: "flush ip route table " + table,
			Observed: map[string]string{
				"table": table,
			},
		})
	}
	return orphans
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
		case "IPv4PolicyRoute":
			spec, err := res.IPv4PolicyRouteSpec()
			if err != nil {
				continue
			}
			add(res.ID(), spec.Priority, spec.Mark, spec.Table)
		case "IPv4PolicyRouteSet":
			spec, err := res.IPv4PolicyRouteSetSpec()
			if err != nil {
				continue
			}
			for _, target := range spec.Targets {
				add(res.ID(), target.Priority, target.Mark, target.Table)
			}
		case "IPv4DefaultRoutePolicy":
			spec, err := res.IPv4DefaultRoutePolicySpec()
			if err != nil {
				continue
			}
			for _, candidate := range spec.Candidates {
				if candidate.RouteSet != "" {
					continue
				}
				add(res.ID(), candidate.Priority, candidate.Mark, candidate.Table)
			}
		}
	}
	return desired
}

func parseIPv4FwmarkRuleArtifacts(output string) []resource.Artifact {
	var rules []resource.Artifact
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		rule := ipv4FwmarkRuleArtifact{}
		priority, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
		if err != nil {
			continue
		}
		rule.Priority = priority
		for i, field := range fields {
			switch field {
			case "fwmark":
				if i+1 >= len(fields) {
					continue
				}
				mark, err := strconv.ParseInt(strings.SplitN(fields[i+1], "/", 2)[0], 0, 64)
				if err != nil {
					continue
				}
				rule.Mark = int(mark)
			case "lookup":
				if i+1 >= len(fields) {
					continue
				}
				table, err := strconv.Atoi(fields[i+1])
				if err != nil {
					continue
				}
				rule.Table = table
			}
		}
		if rule.Mark != 0 && rule.Table != 0 {
			rules = append(rules, newIPv4FwmarkRuleArtifact("", rule.Priority, rule.Mark, rule.Table))
		}
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
		for _, field := range fields[2:] {
			if strings.Contains(field, "/") {
				artifacts = append(artifacts, resource.Artifact{
					Kind: kind,
					Name: ifname + ":" + field,
					Attributes: map[string]string{
						"ifname":  ifname,
						"address": field,
					},
				})
			}
		}
	}
	return artifacts
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

func newIPv4RouteTableArtifact(owner string, table int) resource.Artifact {
	return newIPv4RouteTableArtifactWithIfName(owner, table, "")
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
		Name:  name,
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
	rule, ok := ipv4FwmarkRuleFromArtifact(artifact)
	return ok && routerdManagedFwmark(rule.Mark)
}

func routerdManagedFwmark(mark int) bool {
	return mark >= 0x100 && mark <= 0x1ff
}

func managedIPv4RouteTableArtifact(artifact resource.Artifact) bool {
	table, err := strconv.Atoi(artifact.Attributes["table"])
	return err == nil && table >= 100 && table <= 199
}

func managedNftTableArtifact(artifact resource.Artifact) bool {
	return strings.HasPrefix(artifact.Attributes["name"], "routerd_")
}
