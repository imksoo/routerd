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
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"routerd/pkg/api"
	"routerd/pkg/apply"
	"routerd/pkg/config"
	"routerd/pkg/controlapi"
	"routerd/pkg/observe"
	"routerd/pkg/platform"
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
	case "get":
		return getCommand(args[1:], stdout, stderr)
	case "describe":
		return describeCommand(args[1:], stdout, stderr)
	case "show":
		return showCommand(args[1:], stdout, stderr)
	case "apply":
		return applyCommand(args[1:], stdout)
	case "plan":
		return applyCommand(append([]string{"--dry-run"}, args[1:]...), stdout)
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

func applyCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	dryRun := fs.Bool("dry-run", false, "plan without applying changes")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Apply(ctx, controlapi.ApplyRequest{DryRun: *dryRun})
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
	Events     bool
	SpecOnly   bool
	StatusOnly bool
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
	kind, name, err := parseResourceTarget("describe", opts.Target)
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("describe requires <kind>/<name>")
	}
	if kind == "Inventory" {
		row, err := inventoryShowResource(store, name, opts.EventsLimit)
		if err != nil {
			return err
		}
		return writeDescribe(stdout, row, store)
	}
	resources := selectResources(router.Spec.Resources, kind, name)
	if len(resources) == 0 {
		return resourceSelectionError(router.Spec.Resources, kind, name)
	}
	rows, err := buildShowResources(router, resources, store, ledger, showOptions{Events: true, NAPTLimit: 20})
	if err != nil {
		return err
	}
	if len(rows) != 1 {
		return fmt.Errorf("describe expected one resource, got %d", len(rows))
	}
	rows[0].Events = eventsForResourceLimit(store, resources[0], opts.EventsLimit)
	return writeDescribe(stdout, rows[0], store)
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
		case "--events":
			opts.Events = true
		case "--spec":
			opts.SpecOnly = true
		case "--status":
			opts.StatusOnly = true
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
		"if":                   "Interface",
		"iface":                "Interface",
		"interface":            "Interface",
		"interfaces":           "Interface",
		"pd":                   "IPv6PrefixDelegation",
		"ipv6pd":               "IPv6PrefixDelegation",
		"prefixdelegation":     "IPv6PrefixDelegation",
		"ipv6prefixdelegation": "IPv6PrefixDelegation",
		"ipv4static":           "IPv4StaticAddress",
		"ipv4staticaddress":    "IPv4StaticAddress",
		"ipv4dhcp":             "IPv4DHCPAddress",
		"ipv4dhcpaddress":      "IPv4DHCPAddress",
		"ipv6ra":               "IPv6RAAddress",
		"ipv6raaddress":        "IPv6RAAddress",
		"slaac":                "IPv6RAAddress",
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
		"inventory":            "Inventory",
		"inv":                  "Inventory",
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
			item.State = stateForResource(res, store)
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

func stateForResource(res api.Resource, store routerstate.Store) map[string]any {
	switch res.Kind {
	case "IPv6PrefixDelegation":
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
		header := "KIND\tNAME\tSPEC\tOBSERVED\tLEDGER\tSTATE"
		if opts.Events {
			header += "\tEVENTS"
		}
		fmt.Fprintln(w, header)
		for _, row := range rows {
			line := fmt.Sprintf("%s\t%s\t%s\t%s\t%d artifacts\t%s",
				row.Kind,
				row.Name,
				specSummary(row.Spec),
				observedSummary(row.Observed),
				len(row.Ledger),
				stateSummary(row.State),
			)
			if opts.Events {
				line += fmt.Sprintf("\t%d events", len(row.Events))
			}
			fmt.Fprintln(w, line)
		}
	}
	return w.Flush()
}

func writeGetTable(stdout io.Writer, resources []api.Resource) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "KIND\tNAME\tSPEC")
	for _, res := range resources {
		fmt.Fprintf(w, "%s\t%s\t%s\n", res.Kind, res.Metadata.Name, specSummary(res.Spec))
	}
	return w.Flush()
}

func writeGetKinds(stdout io.Writer, resources []api.Resource, output string) error {
	counts := map[string]int{}
	for _, res := range resources {
		counts[res.Kind]++
	}
	var kinds []string
	for kind := range counts {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	type kindRow struct {
		Kind  string `json:"kind" yaml:"kind"`
		Count int    `json:"count" yaml:"count"`
	}
	var rows []kindRow
	for _, kind := range kinds {
		rows = append(rows, kindRow{Kind: kind, Count: counts[kind]})
	}
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "KIND\tCOUNT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%d\n", row.Kind, row.Count)
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeDescribe(stdout io.Writer, row showResource, store routerstate.Store) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "Name:\t%s\n", row.Name)
	fmt.Fprintf(w, "Kind:\t%s\n", row.Kind)
	fmt.Fprintf(w, "API Version:\t%s\n", row.APIVersion)
	if generationReader, ok := store.(routerstate.ObjectGenerationReader); ok {
		if generation := generationReader.ObjectGeneration(row.APIVersion, row.Kind, row.Name); generation != 0 {
			fmt.Fprintf(w, "Last Apply Generation:\t%d\n", generation)
		}
	}
	writeDescribeStatus(w, row)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Spec:")
	writeDescribeMap(w, row.Spec, "  ")
	fmt.Fprintln(w, "Observed:")
	writeDescribeMap(w, row.Observed, "  ")
	fmt.Fprintln(w, "Ledger:")
	if len(row.Ledger) == 0 {
		fmt.Fprintln(w, "  <none>")
	} else {
		for _, artifact := range row.Ledger {
			fmt.Fprintf(w, "  %s/%s\n", artifact.Kind, artifact.Name)
		}
	}
	fmt.Fprintln(w, "Events:")
	if len(row.Events) == 0 {
		fmt.Fprintln(w, "  <none>")
	} else {
		for _, event := range row.Events {
			fmt.Fprintf(w, "  %s\t%s\t%s\tgeneration=%d\t%s\n", event.CreatedAt.Format(time.RFC3339), event.Type, event.Reason, event.Generation, event.Message)
		}
	}
	return w.Flush()
}

func writeDescribeStatus(w io.Writer, row showResource) {
	if row.Kind == "Inventory" {
		fmt.Fprintf(w, "Currently observable:\t%s\n", yesNo(len(row.State) > 0))
		fmt.Fprintf(w, "OS:\t%s\n", displayCell(nestedString(row.State, "os", "goos")))
		fmt.Fprintf(w, "Kernel:\t%s %s\n", displayCell(nestedString(row.State, "os", "kernelName")), displayCell(nestedString(row.State, "os", "kernelRelease")))
		fmt.Fprintf(w, "Virtualization:\t%s\n", displayCell(nestedString(row.State, "virtualization", "type")))
		fmt.Fprintf(w, "Service Manager:\t%s\n", displayCell(stringValue(row.State["serviceManager"])))
		return
	}
	lease, ok := describePDLease(row.State)
	if ok {
		fmt.Fprintf(w, "Currently observable:\t%s\n", yesNo(lease.CurrentPrefix != ""))
		fmt.Fprintf(w, "Current delegated prefix:\t%s\n", displayCell(lease.CurrentPrefix))
		fmt.Fprintf(w, "Last delegated prefix:\t%s\n", displayCell(lease.LastPrefix))
		fmt.Fprintf(w, "Server ID:\t%s\n", displayCell(lease.ServerID))
		fmt.Fprintf(w, "Client DUID:\t%s\n", displayCell(firstNonEmpty(lease.DUIDText, lease.DUID)))
		fmt.Fprintf(w, "Expected DUID:\t%s\n", displayCell(lease.ExpectedDUID))
		fmt.Fprintf(w, "IAID:\t%s\n", displayCell(lease.IAID))
		fmt.Fprintf(w, "Last Reply at:\t%s\n", displayCell(lease.LastReplyAt))
		fmt.Fprintf(w, "Last observed at:\t%s\n", displayCell(lease.LastObservedAt))
		fmt.Fprintf(w, "Last Solicit at:\t%s\n", displayCell(lease.LastSolicitAt))
		fmt.Fprintf(w, "Last Request at:\t%s\n", displayCell(lease.LastRequestAt))
		fmt.Fprintf(w, "Last Renew at:\t%s\n", displayCell(lease.LastRenewAt))
		fmt.Fprintf(w, "Last Rebind at:\t%s\n", displayCell(lease.LastRebindAt))
		fmt.Fprintf(w, "Last Release at:\t%s\n", displayCell(lease.LastReleaseAt))
		fmt.Fprintf(w, "T1:\t%s\n", displayLeaseSeconds(lease.T1))
		fmt.Fprintf(w, "T2:\t%s\n", displayLeaseSeconds(lease.T2))
		fmt.Fprintf(w, "Preferred lifetime:\t%s\n", displayLeaseSeconds(lease.PLTime))
		fmt.Fprintf(w, "Valid lifetime:\t%s\n", displayLeaseSeconds(lease.VLTime))
		if timing := pdLeaseTiming(lease, time.Now().UTC()); len(timing) > 0 {
			fmt.Fprintf(w, "Next T1 at:\t%s\n", displayCell(timing["t1At"]))
			fmt.Fprintf(w, "Next T2 at:\t%s\n", displayCell(timing["t2At"]))
			fmt.Fprintf(w, "Valid lifetime expires at:\t%s\n", displayCell(timing["expiresAt"]))
			fmt.Fprintf(w, "Valid lifetime remaining:\t%s\n", displayCell(timing["remaining"]))
		}
		if lease.WANObserved != nil {
			fmt.Fprintf(w, "WAN RA source:\t%s\n", displayCell(lease.WANObserved.HGWLinkLocal))
			fmt.Fprintf(w, "WAN RA derived MAC:\t%s\n", displayCell(lease.WANObserved.HGWMACDerived))
			fmt.Fprintf(w, "WAN RA flags:\tM=%s O=%s\n", displayCell(lease.WANObserved.RAMFlag), displayCell(lease.WANObserved.RAOFlag))
			fmt.Fprintf(w, "WAN RA prefix:\t%s\n", displayCell(lease.WANObserved.RAPrefix))
			fmt.Fprintf(w, "WAN RA observed at:\t%s\n", displayCell(lease.WANObserved.RAObservedAt))
		}
		if lease.Acquisition != nil {
			fmt.Fprintf(w, "Acquisition strategy:\t%s\n", displayCell(lease.Acquisition.Strategy))
			fmt.Fprintf(w, "Acquisition phase:\t%s\n", displayCell(lease.Acquisition.Phase))
			fmt.Fprintf(w, "Acquisition attempts since reply:\t%d\n", lease.Acquisition.AttemptsSinceReply)
			fmt.Fprintf(w, "Acquisition next action:\t%s\n", displayCell(lease.Acquisition.NextAction))
			fmt.Fprintf(w, "Acquisition last attempt at:\t%s\n", displayCell(lease.Acquisition.LastAttemptAt))
		}
		if lease.Hung != nil {
			fmt.Fprintf(w, "HGW hung suspected:\tyes\n")
			fmt.Fprintf(w, "HGW hung suspected at:\t%s\n", displayCell(lease.Hung.SuspectedAt))
			fmt.Fprintf(w, "HGW hung reason:\t%s\n", displayCell(lease.Hung.Reason))
		} else {
			fmt.Fprintf(w, "HGW hung suspected:\tno\n")
		}
		if len(lease.Transactions) > 0 {
			fmt.Fprintln(w, "Recent DHCPv6 transactions:")
			limit := len(lease.Transactions)
			if limit > 5 {
				limit = 5
			}
			for _, tx := range lease.Transactions[:limit] {
				fmt.Fprintf(w, "  %s\t%s\t%s\txid=%s\tiaid=%s\tprefix=%s\tt1=%s\tt2=%s\tpl=%s\tvl=%s\treconf=%s\twarning=%s\n",
					displayCell(tx.ObservedAt),
					displayCell(tx.Direction),
					displayCell(tx.MessageType),
					displayCell(tx.TransactionID),
					displayCell(tx.IAID),
					displayCell(tx.Prefix),
					displayCell(tx.T1),
					displayCell(tx.T2),
					displayCell(tx.PreferredLifetime),
					displayCell(tx.ValidLifetime),
					displayCell(tx.ReconfigureAccept),
					displayCell(tx.Warning),
				)
			}
		}
		return
	}
	observable := false
	if exists, ok := row.Observed["exists"].(bool); ok {
		observable = exists
	} else if len(row.Observed) > 0 {
		observable = true
	}
	fmt.Fprintf(w, "Currently observable:\t%s\n", yesNo(observable))
	fmt.Fprintf(w, "Last observed at:\t-\n")
}

func nestedString(values map[string]any, keys ...string) string {
	var current any = values
	for _, key := range keys {
		m, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = m[key]
	}
	return stringValue(current)
}

func stringValue(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func describePDLease(state map[string]any) (routerstate.PDLease, bool) {
	if state == nil {
		return routerstate.PDLease{}, false
	}
	lease, ok := state["lease"].(routerstate.PDLease)
	return lease, ok
}

func pdLeaseTiming(lease routerstate.PDLease, now time.Time) map[string]string {
	base, ok := parseRFC3339Time(lease.LastReplyAt)
	if !ok {
		return nil
	}
	out := map[string]string{}
	if seconds, ok := parseLeaseSeconds(lease.T1); ok {
		out["t1At"] = base.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
	}
	if seconds, ok := parseLeaseSeconds(lease.T2); ok {
		out["t2At"] = base.Add(time.Duration(seconds) * time.Second).UTC().Format(time.RFC3339)
	}
	if seconds, ok := parseLeaseSeconds(lease.VLTime); ok {
		expiresAt := base.Add(time.Duration(seconds) * time.Second).UTC()
		out["expiresAt"] = expiresAt.Format(time.RFC3339)
		if !now.IsZero() {
			remaining := expiresAt.Sub(now).Round(time.Second)
			if remaining <= 0 {
				out["remaining"] = "expired"
			} else {
				out["remaining"] = remaining.String()
			}
		}
	}
	return out
}

func parseRFC3339Time(value string) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		t, err := time.Parse(layout, value)
		if err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func parseLeaseSeconds(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, false
	}
	seconds, err := strconv.ParseInt(value, 10, 64)
	if err != nil || seconds < 0 {
		return 0, false
	}
	return seconds, true
}

func displayLeaseSeconds(value string) string {
	seconds, ok := parseLeaseSeconds(value)
	if !ok {
		return "-"
	}
	return fmt.Sprintf("%ds", seconds)
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func writeDescribeMap(w io.Writer, value any, indent string) {
	values := flattenAny(value)
	if len(values) == 0 {
		fmt.Fprintln(w, indent+"<none>")
		return
	}
	var keys []string
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		fmt.Fprintf(w, "%s%s:\t%v\n", indent, key, values[key])
	}
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
	return platformDefaults.DBFile()
}

func defaultStatePath() string {
	return platformDefaults.DBFile()
}

func defaultSocketPath() string {
	return platformDefaults.SocketFile()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  status [--socket <path>]")
	fmt.Fprintln(w, "  get <kind>[/<name>] [--list-kinds] [--config <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe <kind>/<name> [--config <path>] [--state-file <path>] [--ledger-file <path>] [--events-limit <n>]")
	fmt.Fprintln(w, "  show <kind> [--config <path>] [--state-file <path>] [--ledger-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <kind>/<name> [--diff|--ledger|--adopt|--events|--spec|--status] [-o table|json|yaml]")
	fmt.Fprintln(w, "  plan [--socket <path>]")
	fmt.Fprintln(w, "  apply [--socket <path>] [--dry-run]")
}
