// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type diagnoseOptions struct {
	Target     string
	Output     string
	ConfigPath string
	StatePath  string
	Host       bool
	Server     string
	Names      string
	Timeout    time.Duration
}

type diagnoseReport struct {
	Kind      string                 `json:"kind" yaml:"kind"`
	Name      string                 `json:"name,omitempty" yaml:"name,omitempty"`
	Summary   map[string]any         `json:"summary,omitempty" yaml:"summary,omitempty"`
	Resources []diagnoseResource     `json:"resources,omitempty" yaml:"resources,omitempty"`
	Commands  []diagnoseCommandCheck `json:"commands,omitempty" yaml:"commands,omitempty"`
}

type diagnoseResource struct {
	Kind   string         `json:"kind" yaml:"kind"`
	Name   string         `json:"name" yaml:"name"`
	Spec   map[string]any `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status map[string]any `json:"status,omitempty" yaml:"status,omitempty"`
}

type diagnoseCommandCheck struct {
	Name     string `json:"name" yaml:"name"`
	OK       bool   `json:"ok" yaml:"ok"`
	Output   string `json:"output,omitempty" yaml:"output,omitempty"`
	Error    string `json:"error,omitempty" yaml:"error,omitempty"`
	Command  string `json:"command,omitempty" yaml:"command,omitempty"`
	Stdout   string `json:"stdout,omitempty" yaml:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty" yaml:"stderr,omitempty"`
	ExitCode int    `json:"exitCode" yaml:"exitCode"`
}

func diagnoseCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("diagnose requires subcommand")
	}
	switch args[0] {
	case "egress":
		return diagnoseEgressCommand(args[1:], stdout, stderr)
	case "dns":
		return diagnoseDNSCommand(args[1:], stdout, stderr)
	case "lan-client":
		return diagnoseLANClientCommand(args[1:], stdout, stderr)
	default:
		usage(stderr)
		return fmt.Errorf("unknown diagnose subcommand %q", args[0])
	}
}

func diagnoseEgressCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseDiagnoseOptions("diagnose egress", args, stdout)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		usage(stderr)
		return err
	}
	router, store, err := loadDiagnoseInputs(opts)
	if err != nil {
		return err
	}
	policies := selectResources(router.Spec.Resources, "EgressRoutePolicy", opts.Target)
	if len(policies) == 0 {
		return resourceSelectionError(router.Spec.Resources, "EgressRoutePolicy", opts.Target)
	}
	report := diagnoseReport{Kind: "egress", Name: opts.Target, Summary: map[string]any{}}
	for _, policy := range policies {
		spec, _ := policy.EgressRoutePolicySpec()
		status := objectStatus(store, api.NetAPIVersion, "EgressRoutePolicy", policy.Metadata.Name)
		report.Resources = append(report.Resources, diagnoseResource{
			Kind:   "EgressRoutePolicy",
			Name:   policy.Metadata.Name,
			Spec:   map[string]any{"selection": defaultString(spec.Selection, "highest-weight-ready"), "hysteresis": spec.Hysteresis, "destinationCIDRs": spec.DestinationCIDRs},
			Status: status,
		})
		if report.Summary["selectedCandidate"] == nil {
			report.Summary["selectedCandidate"] = status["selectedCandidate"]
			report.Summary["selectedDevice"] = status["selectedDevice"]
			report.Summary["selectedGateway"] = status["selectedGateway"]
			report.Summary["phase"] = status["phase"]
		}
		for _, candidate := range spec.Candidates {
			if candidate.HealthCheck == "" {
				continue
			}
			report.Resources = append(report.Resources, diagnoseResource{
				Kind:   "HealthCheck",
				Name:   candidate.HealthCheck,
				Spec:   map[string]any{"candidate": firstNonEmpty(candidate.Name, candidate.Source)},
				Status: objectStatus(store, api.NetAPIVersion, "HealthCheck", candidate.HealthCheck),
			})
		}
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "IPv4Route", "NAT44Rule":
			report.Resources = append(report.Resources, diagnoseResource{Kind: resource.Kind, Name: resource.Metadata.Name, Status: objectStatus(store, api.NetAPIVersion, resource.Kind, resource.Metadata.Name)})
		}
	}
	if opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
		report.Commands = append(report.Commands,
			runDiagnosticCommand(ctx, "ip route show default", "ip", "-4", "route", "show", "default"),
			runDiagnosticCommand(ctx, "nft list table ip routerd_nat", "nft", "list", "table", "ip", "routerd_nat"),
			runDiagnosticCommand(ctx, "nft list table ip6 routerd_nat", "nft", "list", "table", "ip6", "routerd_nat"),
			runDiagnosticCommand(ctx, "conntrack summary", "conntrack", "-S"),
		)
	}
	return writeDiagnoseReport(stdout, report, opts.Output)
}

func diagnoseDNSCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseDiagnoseOptions("diagnose dns", args, stdout)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		usage(stderr)
		return err
	}
	router, store, err := loadDiagnoseInputs(opts)
	if err != nil {
		return err
	}
	resolvers := selectResources(router.Spec.Resources, "DNSResolver", opts.Target)
	if len(resolvers) == 0 {
		return resourceSelectionError(router.Spec.Resources, "DNSResolver", opts.Target)
	}
	report := diagnoseReport{Kind: "dns", Name: opts.Target, Summary: map[string]any{"server": opts.Server}}
	for _, resolver := range resolvers {
		spec, _ := resolver.DNSResolverSpec()
		report.Resources = append(report.Resources, diagnoseResource{
			Kind: "DNSResolver",
			Name: resolver.Metadata.Name,
			Spec: map[string]any{
				"listen":  len(spec.Listen),
				"sources": dnsResolverSourceNames(spec.Sources),
				"cache":   spec.Cache,
			},
			Status: objectStatus(store, api.NetAPIVersion, "DNSResolver", resolver.Metadata.Name),
		})
	}
	if opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
		server := firstNonEmpty(opts.Server, "127.0.0.1")
		for _, name := range splitCSV(firstNonEmpty(opts.Names, "example.com")) {
			report.Commands = append(report.Commands, runDiagnosticCommand(ctx, "dig "+name, "dig", "@"+server, name, "A", "+time=2", "+tries=1"))
		}
	}
	return writeDiagnoseReport(stdout, report, opts.Output)
}

func diagnoseLANClientCommand(args []string, stdout, stderr io.Writer) error {
	opts, err := parseDiagnoseOptions("diagnose lan-client", args, stdout)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		usage(stderr)
		return err
	}
	if opts.Target == "" {
		return errors.New("diagnose lan-client requires <ip>")
	}
	report := diagnoseReport{Kind: "lan-client", Name: opts.Target, Summary: map[string]any{"ip": opts.Target}}
	if opts.Host {
		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		defer cancel()
		report.Commands = append(report.Commands,
			runDiagnosticCommand(ctx, "ping "+opts.Target, "ping", "-c", "2", "-W", "1", opts.Target),
			runDiagnosticCommand(ctx, "ip neigh show "+opts.Target, "ip", "neigh", "show", opts.Target),
			runDiagnosticCommand(ctx, "conntrack for "+opts.Target, "conntrack", "-L", "-f", "ipv4", "-s", opts.Target),
		)
	}
	return writeDiagnoseReport(stdout, report, opts.Output)
}

func parseDiagnoseOptions(name string, args []string, helpOutput io.Writer) (diagnoseOptions, error) {
	opts := diagnoseOptions{
		Output:     "table",
		ConfigPath: defaultConfigPath(),
		StatePath:  defaultStatePath(),
		Host:       true,
		Timeout:    5 * time.Second,
	}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	if helpOutput == nil {
		helpOutput = io.Discard
	}
	fs.SetOutput(helpOutput)
	fs.Usage = func() {
		var summary, examples string
		switch name {
		case "diagnose egress":
			summary = "EgressRoutePolicy + HealthCheck + IPv4Route + NAT44Rule の status を集約し、\n" +
				"--host が有効なら ip route / nft / conntrack コマンドも実行して結果を表示する。\n" +
				"位置引数: [policy] (EgressRoutePolicy 名。省略時は全 policy 対象)"
			examples = "routerctl diagnose egress\n" +
				"routerctl diagnose egress ipv4-default -o json\n" +
				"routerctl diagnose egress --no-host -o yaml"
		case "diagnose dns":
			summary = "DNSResolver の status を集約し、--host が有効なら dig で実 query を投げて確認する。\n" +
				"位置引数: [resolver] (DNSResolver 名。省略時は全 resolver 対象)"
			examples = "routerctl diagnose dns\n" +
				"routerctl diagnose dns lan --server 127.0.0.1 --name example.com,routerd.io\n" +
				"routerctl diagnose dns --no-host -o json"
		case "diagnose lan-client":
			summary = "LAN client に対して ping / ip neigh / conntrack で疎通と NAT を確認する。\n" +
				"位置引数: <ip> (LAN client IP, 必須)"
			examples = "routerctl diagnose lan-client 192.168.1.10\n" +
				"routerctl diagnose lan-client 192.168.1.10 --no-host\n" +
				"routerctl diagnose lan-client 192.168.1.10 -o json"
		case "doctor":
			summary = "routerd の各 area (wan/dns/dslite/dhcpv6-pd/nat/firewall/rollback/disk/mgmt) の\n" +
				"健全性チェックをまとめて実行する。\n" +
				"位置引数: [area] (省略時は全 area)"
			examples = "routerctl doctor\n" +
				"routerctl doctor wan\n" +
				"routerctl doctor --no-host -o json"
		default:
			summary = "routerd の resource 状態と host 実情を突き合わせて診断する。"
			examples = "routerctl " + name + " --help"
		}
		printSubcommandHelp(fs, summary, examples)
	}
	fs.StringVar(&opts.Output, "o", opts.Output, "output format: table, json, yaml")
	fs.StringVar(&opts.Output, "output", opts.Output, "output format: table, json, yaml")
	fs.StringVar(&opts.ConfigPath, "config", opts.ConfigPath, "config path")
	fs.StringVar(&opts.StatePath, "state-file", opts.StatePath, "routerd state database file")
	fs.BoolVar(&opts.Host, "host", opts.Host, "run host commands")
	noHost := fs.Bool("no-host", false, "skip host commands")
	fs.StringVar(&opts.Server, "server", "", "DNS server for diagnose dns")
	fs.StringVar(&opts.Names, "name", "", "comma-separated DNS names for diagnose dns")
	fs.DurationVar(&opts.Timeout, "timeout", opts.Timeout, "host command timeout")
	normalized, err := normalizeDiagnoseArgs(args)
	if err != nil {
		return opts, err
	}
	if err := fs.Parse(normalized); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return opts, err
		}
		return opts, err
	}
	if *noHost {
		opts.Host = false
	}
	if fs.NArg() > 1 {
		return opts, fmt.Errorf("unexpected diagnose argument %q", fs.Arg(1))
	}
	if fs.NArg() == 1 {
		opts.Target = fs.Arg(0)
	}
	return opts, nil
}

func normalizeDiagnoseArgs(args []string) ([]string, error) {
	valueFlags := map[string]bool{
		"-o": true, "--output": true, "--config": true, "--state-file": true,
		"--server": true, "--name": true, "--timeout": true,
	}
	var flags []string
	var positionals []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
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

func loadDiagnoseInputs(opts diagnoseOptions) (*api.Router, routerstate.Store, error) {
	router, err := config.Load(opts.ConfigPath)
	if err != nil {
		return nil, nil, err
	}
	store, err := routerstate.Open(opts.StatePath)
	if err != nil {
		return nil, nil, err
	}
	return router, store, nil
}

func objectStatus(store routerstate.Store, apiVersion, kind, name string) map[string]any {
	objectStore, ok := store.(routerstate.ObjectStatusStore)
	if !ok {
		return nil
	}
	return objectStore.ObjectStatus(apiVersion, kind, name)
}

func runDiagnosticCommand(ctx context.Context, label, name string, args ...string) diagnoseCommandCheck {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf
	err := cmd.Run()
	stdout := strings.TrimSpace(stdoutBuf.String())
	stderr := strings.TrimSpace(stderrBuf.String())
	check := diagnoseCommandCheck{
		Name:    label,
		OK:      err == nil,
		Command: diagnoseCommandLine(name, args),
		Stdout:  stdout,
		Stderr:  stderr,
	}
	switch {
	case err == nil:
		check.ExitCode = 0
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			check.ExitCode = exitErr.ExitCode()
		} else {
			check.ExitCode = -1
		}
		check.Error = err.Error()
	}
	check.Output = combineDiagnosticOutput(stdout, stderr)
	return check
}

func diagnoseCommandLine(name string, args []string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}

func combineDiagnosticOutput(stdout, stderr string) string {
	switch {
	case stdout == "" && stderr == "":
		return ""
	case stderr == "":
		return stdout
	case stdout == "":
		return stderr
	default:
		return stdout + "\n--- stderr ---\n" + stderr
	}
}

func writeDiagnoseReport(stdout io.Writer, report diagnoseReport, output string) error {
	switch output {
	case "", "table":
		return writeDiagnoseTable(stdout, report)
	case "json":
		return writeJSON(stdout, report)
	case "yaml":
		return writeYAML(stdout, report)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeDiagnoseTable(stdout io.Writer, report diagnoseReport) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "DIAGNOSE\t%s", report.Kind)
	if report.Name != "" {
		fmt.Fprintf(w, "\t%s", report.Name)
	}
	fmt.Fprintln(w)
	if len(report.Summary) > 0 {
		fmt.Fprintln(w, "SUMMARY\tKEY\tVALUE")
		for _, key := range sortedMapKeys(report.Summary) {
			fmt.Fprintf(w, "SUMMARY\t%s\t%v\n", key, report.Summary[key])
		}
	}
	if len(report.Resources) > 0 {
		fmt.Fprintln(w, "RESOURCE\tNAME\tPHASE\tDETAIL")
		for _, resource := range report.Resources {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", resource.Kind, resource.Name, displayCell(fmt.Sprint(resource.Status["phase"])), compactDiagnoseMap(resource.Status))
		}
	}
	if len(report.Commands) > 0 {
		fmt.Fprintln(w, "COMMAND\tOK\tDETAIL")
		for _, command := range report.Commands {
			detail := command.Output
			if detail == "" {
				detail = command.Error
			}
			detail = strings.ReplaceAll(detail, "\n", " | ")
			fmt.Fprintf(w, "%s\t%t\t%s\n", command.Name, command.OK, detail)
		}
	}
	return w.Flush()
}

func dnsResolverSourceNames(sources []api.DNSResolverSourceSpec) []string {
	var out []string
	for _, source := range sources {
		out = append(out, source.Name)
	}
	sort.Strings(out)
	return out
}

func splitCSV(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func compactDiagnoseMap(values map[string]any) string {
	if len(values) == 0 {
		return "-"
	}
	var parts []string
	for _, key := range sortedMapKeys(values) {
		if key == "conditions" || key == "updatedAt" || key == "lastCheckedAt" {
			continue
		}
		parts = append(parts, key+"="+fmt.Sprint(values[key]))
		if len(parts) >= 4 {
			break
		}
	}
	if len(parts) == 0 {
		return "status"
	}
	return strings.Join(parts, ",")
}
