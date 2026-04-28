package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"routerd/pkg/api"
	"routerd/pkg/config"
	"routerd/pkg/controlapi"
	"routerd/pkg/observe"
	"routerd/pkg/platform"
	"routerd/pkg/reconcile"
	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
)

var platformDefaults, _ = platform.Current()

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("missing command")
	}
	switch args[0] {
	case "status":
		return statusCommand(args[1:], stdout)
	case "show":
		return showCommand(args[1:], stdout, stderr)
	case "reconcile":
		return reconcileCommand(args[1:], stdout)
	case "plan":
		return reconcileCommand(append([]string{"--dry-run"}, args[1:]...), stdout)
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	default:
		usage(stderr)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	status, err := controlapi.NewUnixClient(*socketPath).Status(ctx)
	if err != nil {
		return err
	}
	return writeJSON(stdout, status)
}

func reconcileCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("reconcile", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	dryRun := fs.Bool("dry-run", false, "plan without applying changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Reconcile(ctx, controlapi.ReconcileRequest{DryRun: *dryRun})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

type showOptions struct {
	Target     string
	Output     string
	ConfigPath string
	StatePath  string
	LedgerPath string
	Diff       bool
	LedgerOnly bool
	AdoptOnly  bool
	NAPTLimit  int
}

type showResource struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string              `json:"kind" yaml:"kind"`
	Name       string              `json:"name" yaml:"name"`
	Spec       any                 `json:"spec,omitempty" yaml:"spec,omitempty"`
	Observed   map[string]any      `json:"observed,omitempty" yaml:"observed,omitempty"`
	Ledger     []resource.Artifact `json:"ledger,omitempty" yaml:"ledger,omitempty"`
	State      map[string]any      `json:"state,omitempty" yaml:"state,omitempty"`
	Diff       []showDiff          `json:"diff,omitempty" yaml:"diff,omitempty"`
	Adopt      []any               `json:"adopt,omitempty" yaml:"adopt,omitempty"`
}

type showDiff struct {
	Field    string `json:"field" yaml:"field"`
	Spec     any    `json:"spec,omitempty" yaml:"spec,omitempty"`
	Observed any    `json:"observed,omitempty" yaml:"observed,omitempty"`
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
	ledger, err := resource.LoadLedger(opts.LedgerPath)
	if err != nil {
		return err
	}
	kind, name, err := parseShowTarget(opts.Target)
	if err != nil {
		return err
	}
	resources := selectResources(router.Spec.Resources, kind, name)
	if len(resources) == 0 {
		if !resourceKindExists(router.Spec.Resources, kind) {
			return fmt.Errorf("unknown resource kind %q", kind)
		}
		if name != "" {
			return fmt.Errorf("%s/%s not found", kind, name)
		}
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
		ConfigPath: defaultConfigPath(),
		StatePath:  defaultStatePath(),
		LedgerPath: defaultLedgerPath(),
		NAPTLimit:  20,
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
		case "--napt-limit":
			i++
			if i >= len(args) {
				return opts, errors.New("--napt-limit requires a value")
			}
			var parsed int
			if _, err := fmt.Sscanf(args[i], "%d", &parsed); err != nil {
				return opts, fmt.Errorf("--napt-limit must be an integer")
			}
			opts.NAPTLimit = parsed
		case "--diff":
			opts.Diff = true
		case "--ledger":
			opts.LedgerOnly = true
		case "--adopt":
			opts.AdoptOnly = true
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

func parseShowTarget(target string) (string, string, error) {
	kind, name, hasName := strings.Cut(target, "/")
	kind = canonicalShowKind(kind)
	if kind == "" {
		return "", "", fmt.Errorf("unknown resource kind %q", target)
	}
	if hasName && strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("show target %q has empty name", target)
	}
	return kind, name, nil
}

func canonicalShowKind(kind string) string {
	key := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(kind, "-", ""), "_", ""))
	aliases := map[string]string{
		"if":                   "Interface",
		"iface":                "Interface",
		"interface":            "Interface",
		"interfaces":           "Interface",
		"ipv6pd":               "IPv6PrefixDelegation",
		"prefixdelegation":     "IPv6PrefixDelegation",
		"ipv6prefixdelegation": "IPv6PrefixDelegation",
		"ipv4static":           "IPv4StaticAddress",
		"ipv4staticaddress":    "IPv4StaticAddress",
		"ipv4dhcp":             "IPv4DHCPAddress",
		"ipv4dhcpaddress":      "IPv4DHCPAddress",
		"nat":                  "IPv4SourceNAT",
		"snat":                 "IPv4SourceNAT",
		"ipv4nat":              "IPv4SourceNAT",
		"ipv4sourcenat":        "IPv4SourceNAT",
		"dslite":               "DSLiteTunnel",
		"dslitetunnel":         "DSLiteTunnel",
		"pppoe":                "PPPoEInterface",
		"pppoeinterface":       "PPPoEInterface",
		"fw":                   "FirewallPolicy",
		"firewall":             "FirewallPolicy",
		"firewallpolicy":       "FirewallPolicy",
		"zone":                 "Zone",
		"zones":                "Zone",
		"hostname":             "Hostname",
		"host":                 "Hostname",
		"route":                "IPv4PolicyRouteSet",
		"routeset":             "IPv4PolicyRouteSet",
		"ipv4route":            "IPv4PolicyRouteSet",
		"ipv4policyrouteset":   "IPv4PolicyRouteSet",
	}
	if canonical, ok := aliases[key]; ok {
		return canonical
	}
	if kind == "" {
		return ""
	}
	return kind
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

func buildShowResources(router *api.Router, resources []api.Resource, store *routerstate.Store, ledger *resource.Ledger, opts showOptions) ([]showResource, error) {
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
			item.State = stateForResource(res, store)
			if opts.Diff {
				item.Diff = diffSpecObserved(res.Spec, item.Observed)
				item.Spec = nil
				item.Observed = nil
				item.State = nil
			}
		}
		item.Ledger = ledgerArtifactsForOwner(ledger, res.ID())
		if opts.LedgerOnly {
			item.Spec = nil
			item.Observed = nil
			item.State = nil
			item.Diff = nil
		}
		rows = append(rows, item)
	}
	return rows, nil
}

func adoptOnlyShowResources(router *api.Router, rows []showResource, ledger *resource.Ledger) ([]showResource, error) {
	candidates, _, err := reconcile.New().AdoptionCandidateArtifacts(router, ledger)
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

func ledgerArtifactsForOwner(ledger *resource.Ledger, owner string) []resource.Artifact {
	var out []resource.Artifact
	for _, artifact := range ledger.Artifacts {
		if artifact.Owner == owner {
			out = append(out, artifact)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Identity() < out[j].Identity() })
	return out
}

func observeResource(res api.Resource, aliases map[string]string, opts showOptions) map[string]any {
	switch res.Kind {
	case "Interface":
		spec, _ := res.InterfaceSpec()
		return observeInterface(spec.IfName)
	case "IPv4StaticAddress":
		spec, _ := res.IPv4StaticAddressSpec()
		return map[string]any{"interface": aliases[spec.Interface], "addresses": interfaceIPv4Addresses(aliases[spec.Interface])}
	case "IPv4DHCPAddress":
		spec, _ := res.IPv4DHCPAddressSpec()
		return map[string]any{"interface": aliases[spec.Interface], "addresses": interfaceIPv4Addresses(aliases[spec.Interface])}
	case "IPv6PrefixDelegation":
		spec, _ := res.IPv6PrefixDelegationSpec()
		return map[string]any{"interface": aliases[spec.Interface]}
	case "IPv4SourceNAT":
		table, err := observe.NAPT(opts.NAPTLimit)
		if err != nil {
			return map[string]any{"naptError": err.Error()}
		}
		return map[string]any{"napt": table}
	case "DSLiteTunnel":
		spec, _ := res.DSLiteTunnelSpec()
		return observeInterface(firstNonEmpty(spec.TunnelName, res.Metadata.Name))
	case "PPPoEInterface":
		spec, _ := res.PPPoEInterfaceSpec()
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

func stateForResource(res api.Resource, store *routerstate.Store) map[string]any {
	switch res.Kind {
	case "IPv6PrefixDelegation":
		base := "ipv6PrefixDelegation." + res.Metadata.Name
		lease, _ := routerstate.PDLeaseFromStore(store, base)
		return map[string]any{
			"lease":              lease,
			"client":             stateString(store, base+".client"),
			"profile":            stateString(store, base+".profile"),
			"prefixLength":       stateString(store, base+".prefixLength"),
			"convergenceTimeout": stateString(store, base+".convergenceTimeout"),
			"uplinkIfname":       stateString(store, base+".uplinkIfname"),
			"downstreamIfname":   stateString(store, base+".downstreamIfname"),
		}
	case "Hostname":
		return prefixedState(store, "hostname.")
	default:
		return prefixedState(store, statePrefixForKind(res.Kind, res.Metadata.Name))
	}
}

func statePrefixForKind(kind, name string) string {
	prefixes := map[string]string{
		"Interface":         "interface.",
		"IPv4StaticAddress": "ipv4StaticAddress.",
		"IPv4DHCPAddress":   "ipv4DHCPAddress.",
		"IPv4SourceNAT":     "ipv4SourceNAT.",
		"DSLiteTunnel":      "dsLiteTunnel.",
		"PPPoEInterface":    "pppoeInterface.",
		"FirewallPolicy":    "firewallPolicy.",
		"Zone":              "zone.",
	}
	if prefix := prefixes[kind]; prefix != "" {
		return prefix + name + "."
	}
	return strings.ToLower(kind[:1]) + kind[1:] + "." + name + "."
}

func prefixedState(store *routerstate.Store, prefix string) map[string]any {
	out := map[string]any{}
	for key, value := range store.Variables {
		if strings.HasPrefix(key, prefix) {
			out[strings.TrimPrefix(key, prefix)] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stateString(store *routerstate.Store, key string) string {
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

func writeShowTable(stdout io.Writer, rows []showResource, opts showOptions) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	switch {
	case opts.AdoptOnly:
		fmt.Fprintln(w, "KIND\tNAME\tADOPT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%d candidates\n", row.Kind, row.Name, len(row.Adopt))
		}
	case opts.Diff:
		fmt.Fprintln(w, "KIND\tNAME\tDIFF")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%d fields\n", row.Kind, row.Name, len(row.Diff))
		}
	case opts.LedgerOnly:
		fmt.Fprintln(w, "KIND\tNAME\tLEDGER")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%d artifacts\n", row.Kind, row.Name, len(row.Ledger))
		}
	default:
		fmt.Fprintln(w, "KIND\tNAME\tSPEC\tOBSERVED\tLEDGER\tSTATE")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d artifacts\t%s\n",
				row.Kind,
				row.Name,
				specSummary(row.Spec),
				observedSummary(row.Observed),
				len(row.Ledger),
				stateSummary(row.State),
			)
		}
	}
	return w.Flush()
}

func specSummary(spec any) string {
	values := flattenAny(spec)
	if len(values) == 0 {
		return "-"
	}
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var parts []string
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(values[key]))
		if len(parts) >= 3 {
			break
		}
	}
	return strings.Join(parts, ",")
}

func observedSummary(observed map[string]any) string {
	if len(observed) == 0 {
		return "-"
	}
	if exists, ok := observed["exists"]; ok {
		return "exists=" + fmt.Sprint(exists)
	}
	if hostname, ok := observed["hostname"]; ok {
		return "hostname=" + fmt.Sprint(hostname)
	}
	if addrs, ok := observed["addresses"]; ok {
		return "addresses=" + fmt.Sprint(addrs)
	}
	if napt, ok := observed["napt"]; ok {
		if table, ok := napt.(*observe.NAPTTable); ok {
			return fmt.Sprintf("conntrack=%d", table.Count)
		}
	}
	if err, ok := observed["naptError"]; ok {
		return "error=" + fmt.Sprint(err)
	}
	return "observed"
}

func stateSummary(state map[string]any) string {
	if len(state) == 0 {
		return "-"
	}
	if leaseValue, ok := state["lease"]; ok {
		if lease, ok := leaseValue.(routerstate.PDLease); ok {
			return "current=" + displayCell(lease.CurrentPrefix) + ",last=" + displayCell(lease.LastPrefix)
		}
	}
	return fmt.Sprintf("%d values", len(state))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func displayCell(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func writeJSON(stdout io.Writer, value any) error {
	encoder := json.NewEncoder(stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func writeYAML(stdout io.Writer, value any) error {
	data, err := yaml.Marshal(value)
	if err != nil {
		return err
	}
	_, err = stdout.Write(data)
	return err
}

func defaultRuntimeDir() string {
	return platformDefaults.RuntimeDir
}

func defaultConfigPath() string {
	return platformDefaults.ConfigFile()
}

func defaultLedgerPath() string {
	return platformDefaults.LedgerFile()
}

func defaultStatePath() string {
	return platformDefaults.StateDir + "/state.json"
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  status [--socket <path>]")
	fmt.Fprintln(w, "  show <kind> [--config <path>] [--state-file <path>] [--ledger-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <kind>/<name> [--diff|--ledger|--adopt] [-o table|json|yaml]")
	fmt.Fprintln(w, "  plan [--socket <path>]")
	fmt.Fprintln(w, "  reconcile [--socket <path>] [--dry-run]")
}
