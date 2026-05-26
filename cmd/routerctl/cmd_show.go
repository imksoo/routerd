// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/render"
	"github.com/imksoo/routerd/pkg/resource"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type showOptions struct {
	Target           string
	Output           string
	ConfigPath       string
	StatePath        string
	LedgerPath       string
	Diff             bool
	LedgerOnly       bool
	AdoptOnly        bool
	Events           bool
	SpecOnly         bool
	StatusOnly       bool
	Verbose          bool
	IncludeStale     bool
	ConnectionsLimit int
}

func showCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseShowOptions(args)
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
	if kind := dedicatedShowKind(opts.Target); kind != "" {
		return writeDedicatedShow(stdout, router, store, opts, kind)
	}
	kind, name, err := parseShowTarget(opts.Target)
	if err != nil {
		return err
	}
	if kind == "Inventory" {
		rows, err := inventoryShowResources(store, name, opts.Events)
		if err != nil {
			return err
		}
		switch opts.Output {
		case "", "table":
			return writeShowTable(stdout, rows, opts)
		case "json":
			return writeJSON(stdout, rows)
		case "yaml":
			return writeYAML(stdout, rows)
		default:
			return fmt.Errorf("unsupported output %q", opts.Output)
		}
	}
	ledger, err := resource.LoadLedger(opts.LedgerPath)
	if err != nil {
		return err
	}
	resources := selectResources(router.Spec.Resources, kind, name)
	if len(resources) == 0 {
		return resourceSelectionError(router.Spec.Resources, kind, name)
	}
	rows, err := buildShowResources(router, resources, store, ledger, opts)
	if err != nil {
		return err
	}
	if opts.AdoptOnly {
		rows, err = adoptOnlyShowResources(router, rows, ledger)
		if err != nil {
			return err
		}
	}
	switch opts.Output {
	case "", "table":
		return writeShowTable(stdout, rows, opts)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", opts.Output)
	}
}

func parseShowOptions(args []string) (showOptions, error) {
	opts := showOptions{
		ConfigPath:       defaultConfigPath(),
		StatePath:        defaultStatePath(),
		LedgerPath:       defaultLedgerPath(),
		ConnectionsLimit: 20,
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
		case "--connections-limit":
			i++
			if i >= len(args) {
				return opts, errors.New("--connections-limit requires a value")
			}
			var parsed int
			if _, err := fmt.Sscanf(args[i], "%d", &parsed); err != nil {
				return opts, fmt.Errorf("--connections-limit must be an integer")
			}
			opts.ConnectionsLimit = parsed
		case "--diff":
			opts.Diff = true
		case "--ledger":
			opts.LedgerOnly = true
		case "--adopt":
			opts.AdoptOnly = true
		case "--events":
			opts.Events = true
		case "--spec":
			opts.SpecOnly = true
		case "--status":
			opts.StatusOnly = true
		case "-v", "--verbose":
			opts.Verbose = true
		case "--include-stale":
			opts.IncludeStale = true
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
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown show option %q", arg)
			}
			if opts.Target != "" {
				return opts, fmt.Errorf("unexpected show argument %q", arg)
			}
			opts.Target = arg
		}
	}
	if opts.Target == "" {
		return opts, errors.New("show requires <kind> or <kind>/<name>")
	}
	return opts, nil
}

func dedicatedShowKind(target string) string {
	if strings.Contains(target, "/") {
		return ""
	}
	switch strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(target), "-", ""), "_", "")) {
	case "bgp":
		return "bgp"
	case "vrrp":
		return "vrrp"
	case "ingress":
		return "ingress"
	case "derivedresources", "derived":
		return "derived-resources"
	default:
		return ""
	}
}

func writeDedicatedShow(stdout io.Writer, router *api.Router, store routerstate.Store, opts showOptions, kind string) error {
	if kind == "derived-resources" {
		rows, err := buildDerivedShowResources(router, store, opts.IncludeStale)
		if err != nil {
			return err
		}
		switch opts.Output {
		case "", "table":
			return writeDerivedResourcesTable(stdout, rows)
		case "json":
			return writeJSON(stdout, rows)
		case "yaml":
			return writeYAML(stdout, rows)
		default:
			return fmt.Errorf("unsupported output %q", opts.Output)
		}
	}
	resources, err := listObjectStatuses(store)
	if err != nil {
		return err
	}
	if kind == "vrrp" {
		resources = withLiveVRRPRoles(router, resources)
	} else if kind == "bgp" {
		resources = withLiveBGPState(router, resources)
	}
	switch opts.Output {
	case "", "table":
		switch kind {
		case "bgp":
			return writeBGPShowTable(stdout, router, resources)
		case "vrrp":
			return writeVRRPShowTable(stdout, router, resources)
		case "ingress":
			return writeIngressShowTable(stdout, router, resources, opts.Verbose)
		}
	case "json":
		return writeJSON(stdout, filterShowStatuses(resources, kind))
	case "yaml":
		return writeYAML(stdout, filterShowStatuses(resources, kind))
	default:
		return fmt.Errorf("unsupported output %q", opts.Output)
	}
	return nil
}

func buildDerivedShowResources(router *api.Router, store routerstate.Store, includeStale bool) ([]showResource, error) {
	explicit := map[string]bool{}
	if router != nil {
		for _, res := range router.Spec.Resources {
			explicit[res.APIVersion+"/"+res.Kind+"/"+res.Metadata.Name] = true
		}
	}
	byID := map[string]showResource{}
	add := func(row showResource) {
		id := row.APIVersion + "/" + row.Kind + "/" + row.Name
		if existing, ok := byID[id]; ok {
			if len(row.Observed) > 0 {
				existing.Observed = row.Observed
				existing.State = row.State
			}
			if row.Source != "" {
				existing.Source = row.Source
			}
			byID[id] = existing
			return
		}
		byID[id] = row
	}
	for _, row := range plannedDerivedShowResources(router) {
		add(row)
	}
	statuses, err := listObjectStatuses(store)
	if err != nil {
		return nil, err
	}
	for _, status := range statuses {
		id := status.APIVersion + "/" + status.Kind + "/" + status.Name
		if explicit[id] {
			continue
		}
		_, planned := byID[id]
		if !planned && !includeStale {
			continue
		}
		source := firstNonEmpty(statusString(status.Status["source"]), status.Owner)
		observed := status.Status
		stale := false
		if !planned {
			stale = true
			observed = staleObjectStatus(status)
			source = firstNonEmpty(source, "stale-state")
		}
		add(showResource{
			APIVersion: status.APIVersion,
			Kind:       status.Kind,
			Name:       status.Name,
			Source:     source,
			Stale:      stale,
			Observed:   observed,
			State:      observed,
		})
	}
	rows := make([]showResource, 0, len(byID))
	for _, row := range byID {
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Kind != rows[j].Kind {
			return rows[i].Kind < rows[j].Kind
		}
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

func staleObjectStatus(status routerstate.ObjectStatus) map[string]any {
	out := map[string]any{}
	for key, value := range status.Status {
		out[key] = value
	}
	reason := "StaleStateNotInCurrentConfig"
	if showAPIVersionForKnownKind(canonicalShowKind(status.Kind)) == "" {
		reason = "UnsupportedResourceKind"
	}
	if phase := statusString(out["phase"]); phase != "" {
		out["previousPhase"] = phase
	}
	out["phase"] = "Stale"
	out["reason"] = reason
	out["stale"] = true
	out["message"] = "state row is not derived from the current router config"
	return out
}

func plannedDerivedShowResources(router *api.Router) []showResource {
	if router == nil {
		return nil
	}
	var rows []showResource
	addServiceUnit := func(name, source string) {
		rows = append(rows, showResource{
			APIVersion: api.SystemAPIVersion,
			Kind:       "ServiceUnit",
			Name:       name,
			Source:     source,
			State:      map[string]any{"phase": "Planned", "source": source},
		})
	}
	addServiceUnit(render.RouterdUnitName, "Router/"+router.Metadata.Name)
	if render.RouterWantsDPIClassifier(router) {
		addServiceUnit(render.DPIClassifierUnitName, "TrafficFlowLog/FirewallEventLog")
	}
	if render.RouterWantsNDPIAgent(router) {
		addServiceUnit(render.NDPIAgentUnitName, "TrafficFlowLog/FirewallEventLog")
	}
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "TailscaleNode":
			spec, err := res.TailscaleNodeSpec()
			if err != nil {
				continue
			}
			unit := render.TailscaleSystemdSpec(res.Metadata.Name, spec)
			addServiceUnit(firstNonEmpty(unit.UnitName, render.TailscaleUnitName(res.Metadata.Name)), "TailscaleNode/"+res.Metadata.Name)
		case "HealthCheck":
			addServiceUnit("routerd-healthcheck@"+res.Metadata.Name+".service", "HealthCheck/"+res.Metadata.Name)
		case "FirewallEventLog":
			spec, err := res.FirewallEventLogSpec()
			if err == nil && spec.Enabled {
				addServiceUnit("routerd-firewall-logger.service", "FirewallEventLog/"+res.Metadata.Name)
			}
		}
	}
	return rows
}

func writeDerivedResourcesTable(stdout io.Writer, rows []showResource) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tSOURCE\tPHASE\tDETAIL")
	for _, row := range rows {
		status := row.Observed
		if len(status) == 0 {
			status = row.State
		}
		detail := firstNonEmpty(statusString(status["path"]), statusString(status["unitName"]), statusString(status["reason"]), "-")
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			row.Kind,
			row.Name,
			defaultShowString(row.Source, "-"),
			defaultShowString(statusString(status["phase"]), "Planned"),
			detail,
		)
	}
	return w.Flush()
}

func listObjectStatuses(store routerstate.Store) ([]routerstate.ObjectStatus, error) {
	lister, ok := store.(routerstate.ObjectStatusLister)
	if !ok {
		return nil, nil
	}
	return lister.ListObjectStatuses()
}

func filterShowStatuses(resources []routerstate.ObjectStatus, kind string) []routerstate.ObjectStatus {
	var out []routerstate.ObjectStatus
	for _, resource := range resources {
		switch kind {
		case "bgp":
			if resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer" {
				out = append(out, resource)
			}
		case "vrrp":
			if resource.Kind == "VirtualAddress" {
				out = append(out, resource)
			}
		case "ingress":
			if resource.Kind == "IngressService" {
				out = append(out, resource)
			}
		}
	}
	return out
}

func buildShowResources(router *api.Router, resources []api.Resource, store routerstate.Store, ledger resource.Ledger, opts showOptions) ([]showResource, error) {
	aliases := interfaceAliases(router.Spec.Resources)
	var rows []showResource
	for _, res := range resources {
		item := showResource{
			APIVersion: res.APIVersion,
			Kind:       res.Kind,
			Name:       res.Metadata.Name,
			Spec:       res.Spec,
		}
		if !opts.LedgerOnly {
			item.Observed = observeResource(res, aliases, opts)
			objectStatus := objectStatusForResource(res, store)
			if len(item.Observed) == 0 && len(objectStatus) > 0 {
				item.Observed = objectStatus
			}
			item.State = stateForResource(res, store)
			if len(item.State) == 0 && len(objectStatus) > 0 {
				item.State = objectStatus
			}
			if opts.Diff {
				item.Diff = diffSpecObserved(res.Spec, item.Observed)
				item.Spec = nil
				item.Observed = nil
				item.State = nil
			}
		}
		item.Ledger = ledgerArtifactsForOwner(ledger, res.ID())
		if opts.Events {
			item.Events = eventsForResource(store, res)
		}
		if opts.LedgerOnly {
			item.Spec = nil
			item.Observed = nil
			item.State = nil
			item.Diff = nil
			item.Events = nil
		}
		if opts.SpecOnly {
			item.Observed = nil
			item.Ledger = nil
			item.State = nil
			item.Diff = nil
			item.Adopt = nil
			item.Events = nil
		}
		if opts.StatusOnly {
			item.Spec = nil
			item.Ledger = nil
			item.Diff = nil
			item.Adopt = nil
		}
		rows = append(rows, item)
	}
	return rows, nil
}

func objectStatusForResource(res api.Resource, store routerstate.Store) map[string]any {
	objectStore, ok := store.(routerstate.ObjectStatusStore)
	if !ok {
		return nil
	}
	return objectStore.ObjectStatus(res.APIVersion, res.Kind, res.Metadata.Name)
}

func inventoryShowResources(store routerstate.Store, name string, includeEvents bool) ([]showResource, error) {
	if name != "" && name != "host" {
		return nil, fmt.Errorf("Inventory/%s not found", name)
	}
	row, err := inventoryShowResource(store, "host", 20)
	if err != nil {
		return nil, err
	}
	if !includeEvents {
		row.Events = nil
	}
	return []showResource{row}, nil
}

func inventoryShowResource(store routerstate.Store, name string, eventsLimit int) (showResource, error) {
	objectStore, ok := store.(routerstate.ObjectStatusStore)
	if !ok {
		return showResource{}, errors.New("inventory requires SQLite state storage")
	}
	status := objectStore.ObjectStatus(api.RouterAPIVersion, "Inventory", name)
	if len(status) == 0 {
		return showResource{}, fmt.Errorf("Inventory/%s not found", name)
	}
	res := api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Inventory"},
		Metadata: api.ObjectMeta{Name: name},
		Spec:     api.InventorySpec{},
	}
	return showResource{
		APIVersion: res.APIVersion,
		Kind:       res.Kind,
		Name:       res.Metadata.Name,
		Spec:       res.Spec,
		Observed:   status,
		State:      status,
		Events:     eventsForResourceLimit(store, res, eventsLimit),
	}, nil
}

func adoptOnlyShowResources(router *api.Router, rows []showResource, ledger resource.Ledger) ([]showResource, error) {
	candidates, _, err := apply.New().AdoptionCandidateArtifacts(router, ledger)
	if err != nil {
		return nil, err
	}
	byOwner := map[string][]any{}
	for _, candidate := range candidates {
		byOwner[candidate.Owner] = append(byOwner[candidate.Owner], candidate)
	}
	var out []showResource
	for _, row := range rows {
		owner := row.APIVersion + "/" + row.Kind + "/" + row.Name
		row.Spec = nil
		row.Observed = nil
		row.Ledger = nil
		row.State = nil
		row.Diff = nil
		row.Adopt = byOwner[owner]
		if len(row.Adopt) > 0 {
			out = append(out, row)
		}
	}
	return out, nil
}

func interfaceAliases(resources []api.Resource) map[string]string {
	aliases := map[string]string{}
	for _, res := range resources {
		if res.Kind != "Interface" {
			continue
		}
		spec, err := res.InterfaceSpec()
		if err == nil {
			aliases[res.Metadata.Name] = spec.IfName
		}
	}
	return aliases
}

func ledgerArtifactsForOwner(ledger resource.Ledger, owner string) []resource.Artifact {
	var out []resource.Artifact
	for _, artifact := range ledger.All() {
		if artifact.Owner == owner {
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

func writeOrphans(stdout io.Writer, router *api.Router, ledger resource.Ledger) error {
	engine := apply.New()
	if err := engine.Validate(router); err != nil {
		return err
	}
	orphans, _, err := engine.LedgerOwnedOrphans(router, ledger)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tOWNER\tREMEDIATION")
	for _, orphan := range orphans {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", orphan.Kind, orphan.Name, orphan.Owner, orphan.Remediation)
	}
	return w.Flush()
}

func eventsForResource(store routerstate.Store, res api.Resource) []routerstate.Event {
	return eventsForResourceLimit(store, res, 20)
}

func eventsForResourceLimit(store routerstate.Store, res api.Resource, limit int) []routerstate.Event {
	recorder, ok := store.(routerstate.EventRecorder)
	if !ok {
		return nil
	}
	return recorder.Events(res.APIVersion, res.Kind, res.Metadata.Name, limit)
}

func observeResource(res api.Resource, aliases map[string]string, opts showOptions) map[string]any {
	switch res.Kind {
	case "Interface":
		spec, _ := res.InterfaceSpec()
		return observeInterface(spec.IfName)
	case "IPv4StaticAddress":
		spec, _ := res.IPv4StaticAddressSpec()
		return map[string]any{"interface": aliases[spec.Interface], "addresses": interfaceIPv4Addresses(aliases[spec.Interface])}
	case "DHCPv4Client":
		spec, _ := res.DHCPv4ClientSpec()
		return map[string]any{"interface": aliases[spec.Interface], "addresses": interfaceIPv4Addresses(aliases[spec.Interface])}
	case "DHCPv6PrefixDelegation":
		spec, _ := res.DHCPv6PrefixDelegationSpec()
		return map[string]any{"interface": aliases[spec.Interface]}
	case "DSLiteTunnel":
		spec, _ := res.DSLiteTunnelSpec()
		return observeInterface(firstNonEmpty(spec.TunnelName, res.Metadata.Name))
	case "PPPoESession":
		spec, _ := res.PPPoESessionSpec()
		return observeInterface(firstNonEmpty(spec.IfName, "ppp-"+res.Metadata.Name))
	case "Hostname":
		hostname, err := os.Hostname()
		if err != nil {
			return map[string]any{"error": err.Error()}
		}
		return map[string]any{"hostname": hostname}
	default:
		return map[string]any{}
	}
}

func observeInterface(ifname string) map[string]any {
	if ifname == "" {
		return map[string]any{"exists": false}
	}
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return map[string]any{"ifname": ifname, "exists": false, "error": err.Error()}
	}
	addrs, _ := iface.Addrs()
	var addressStrings []string
	for _, addr := range addrs {
		addressStrings = append(addressStrings, addr.String())
	}
	sort.Strings(addressStrings)
	return map[string]any{
		"ifname":       ifname,
		"exists":       true,
		"flags":        iface.Flags.String(),
		"mtu":          iface.MTU,
		"hardwareAddr": iface.HardwareAddr.String(),
		"addresses":    addressStrings,
	}
}

func interfaceIPv4Addresses(ifname string) []string {
	iface, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return nil
	}
	var out []string
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil || ip.To4() == nil {
			continue
		}
		out = append(out, addr.String())
	}
	sort.Strings(out)
	return out
}

func stateForResource(res api.Resource, store routerstate.Store) map[string]any {
	switch res.Kind {
	case "DHCPv6PrefixDelegation":
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		lease, _ := routerstate.PDLeaseFromStore(store, base)
		return map[string]any{
			"lease":            lease,
			"client":           stateString(store, base+".client"),
			"profile":          stateString(store, base+".profile"),
			"prefixLength":     stateString(store, base+".prefixLength"),
			"uplinkIfname":     stateString(store, base+".uplinkIfname"),
			"downstreamIfname": stateString(store, base+".downstreamIfname"),
		}
	case "Hostname":
		return prefixedState(store, "hostname.")
	default:
		return prefixedState(store, statePrefixForKind(res.Kind, res.Metadata.Name))
	}
}

func statePrefixForKind(kind, name string) string {
	prefixes := map[string]string{
		"Interface":            "interface.",
		"IPv4StaticAddress":    "ipv4StaticAddress.",
		"DHCPv4Client":         "dhcpv4Client.",
		"DSLiteTunnel":         "dsLiteTunnel.",
		"PPPoESession":         "pppoeSession.",
		"FirewallPolicy":       "firewallPolicy.",
		"FirewallZone":         "firewallZone.",
		"FirewallRule":         "firewallRule.",
		"IPAddressSet":         "ipAddressSet.",
		"LocalServiceRedirect": "localServiceRedirect.",
	}
	if prefix := prefixes[kind]; prefix != "" {
		return prefix + name + "."
	}
	return strings.ToLower(kind[:1]) + kind[1:] + "." + name + "."
}

func prefixedState(store routerstate.Store, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range store.Variables() {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stateString(store routerstate.Store, key string) string {
	value := store.Get(key)
	if value.Status != routerstate.StatusSet {
		return ""
	}
	return value.Value
}

func diffSpecObserved(spec any, observed map[string]any) []showDiff {
	specMap := flattenAny(spec)
	observedMap := flattenAny(observed)
	keys := map[string]bool{}
	for key := range specMap {
		keys[key] = true
	}
	for key := range observedMap {
		keys[key] = true
	}
	var sorted []string
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	var diffs []showDiff
	for _, key := range sorted {
		specValue, specOK := specMap[key]
		observedValue, observedOK := observedMap[key]
		if !specOK || !observedOK || !reflect.DeepEqual(fmt.Sprint(specValue), fmt.Sprint(observedValue)) {
			diffs = append(diffs, showDiff{Field: key, Spec: specValue, Observed: observedValue})
		}
	}
	return diffs
}

func flattenAny(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return map[string]any{}
	}
	out := map[string]any{}
	flattenValue("", decoded, out)
	return out
}

func flattenValue(prefix string, value any, out map[string]any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			next := key
			if prefix != "" {
				next = prefix + "." + key
			}
			flattenValue(next, child, out)
		}
	case []any:
		out[prefix] = typed
	default:
		if prefix != "" {
			out[prefix] = typed
		}
	}
}
