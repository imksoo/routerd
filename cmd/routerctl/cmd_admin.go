// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/ingressdrain"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/servicemgr"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func validateCommand(args []string, stdout io.Writer, stdin io.Reader) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"routerd の canonical config、または -f の candidate を静的検証する。host 状態は変更しない。\n"+
				"Exit codes: 0=valid, 1=invalid config, 2=execution or transport error.",
			"routerctl validate\n"+
				"routerctl validate -f candidate.yaml\n"+
				"routerctl validate -f -")
	}
	socketPath := fs.String("socket", defaultStatusSocketPath(), "routerd read-only status Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	filePath := fs.String("f", "", "candidate YAML path; use - for stdin")
	replace := fs.Bool("replace", false, "validate candidate as full replacement instead of partial upsert")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return commandExitError{Code: 2, Err: err}
	}
	candidate, err := readCandidateYAML(*filePath, stdin)
	if err != nil {
		return commandExitError{Code: 2, Err: err}
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Validate(ctx, controlapi.ValidateRequest{CandidateYAML: candidate, Replace: *replace})
	if err != nil {
		return commandExitError{Code: 2, Err: fmt.Errorf("routerd serve is not reachable for validate; start routerd serve or check --socket: %w", err)}
	}
	if err := writeJSON(stdout, result); err != nil {
		return commandExitError{Code: 2, Err: err}
	}
	if !result.Valid {
		return commandExitError{Code: 1, Err: errors.New("validate failed: config is invalid")}
	}
	return nil
}

func planCommand(args []string, stdout io.Writer, stdin io.Reader) error {
	fs := flag.NewFlagSet("plan", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"routerd の canonical config、または -f の candidate を plan する。host 状態は変更しない。",
			"routerctl plan\n"+
				"routerctl plan -f candidate.yaml\n"+
				"routerctl plan -f - --replace")
	}
	socketPath := fs.String("socket", defaultStatusSocketPath(), "routerd read-only status Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	filePath := fs.String("f", "", "candidate YAML path; use - for stdin")
	replace := fs.Bool("replace", false, "plan candidate as full replacement instead of partial upsert")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	candidate, err := readCandidateYAML(*filePath, stdin)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Plan(ctx, controlapi.PlanRequest{CandidateYAML: candidate, Replace: *replace})
	if err != nil {
		return fmt.Errorf("routerd serve is not reachable for plan; start routerd serve or check --socket: %w", err)
	}
	return writeJSON(stdout, result)
}

func applyCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"-f の candidate を canonical config へ upsert し、既定で即時 reconcile する。\n"+
				"入力は必須。--replace は全置換、--no-reconcile は canonical 書込のみ。",
			"routerctl apply -f candidate.yaml\n"+
				"routerctl apply -f - --replace\n"+
				"routerctl apply -f candidate.yaml --no-reconcile")
	}
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	filePath := fs.String("f", "", "candidate YAML path; use - for stdin")
	replace := fs.Bool("replace", false, "replace canonical config instead of partial upsert")
	noReconcile := fs.Bool("no-reconcile", false, "write canonical config without immediate reconcile")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if strings.TrimSpace(*filePath) == "" {
		return errors.New("apply requires -f <file> or -f -")
	}
	candidate, err := readCandidateYAML(*filePath, os.Stdin)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Apply(ctx, controlapi.ApplyRequest{CandidateYAML: candidate, Replace: *replace, NoReconcile: *noReconcile})
	if err != nil {
		return fmt.Errorf("routerd serve is not reachable for apply; start routerd serve or check --socket: %w", err)
	}
	return writeJSON(stdout, result)
}

func deleteCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"指定 <kind>/<name> resource を routerd の state から削除する。\n"+
				"位置引数: <kind>/<name> (必須)",
			"routerctl delete DSLiteTunnel/home\n"+
				"routerctl delete --dry-run NAT44Rule/lan-to-wan\n"+
				"routerctl delete --force --api-version net.routerd.io/v1alpha1 LegacyKind/old")
	}
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	dryRun := fs.Bool("dry-run", false, "show what would be deleted without changing host state")
	force := fs.Bool("force", false, "delete stale state even when the kind is no longer in the current schema")
	apiVersion := fs.String("api-version", "", "apiVersion to use with --force when a stale kind is ambiguous")
	noReconcile := fs.Bool("no-reconcile", false, "write canonical config without immediate reconcile")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("delete requires <kind>/<name>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Delete(ctx, controlapi.DeleteRequest{Target: fs.Arg(0), TargetAPIVersion: *apiVersion, DryRun: *dryRun, Force: *force, NoReconcile: *noReconcile})
	if err != nil {
		return fmt.Errorf("routerd serve is not reachable for delete; start routerd serve or check --socket: %w", err)
	}
	return writeJSON(stdout, result)
}

func readCandidateYAML(path string, stdin io.Reader) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	var data []byte
	var err error
	if path == "-" {
		if stdin == nil {
			stdin = os.Stdin
		}
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", fmt.Errorf("candidate %s is empty", path)
	}
	return string(data), nil
}

func setLogLevelCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("log-level", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"動作中の routerd の slog ログレベルを動的に変更する。\n"+
				"位置引数: <debug|info|warning|error|default> (default は起動時の値に戻す)",
			"routerctl log-level debug\n"+
				"routerctl log-level info\n"+
				"routerctl log-level default")
	}
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("log-level requires <debug|info|warning|error|default>")
	}
	level := fs.Arg(0)
	switch level {
	case "debug", "info", "warning", "error", "default":
	default:
		return fmt.Errorf("unsupported log level %q", level)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).SetLogLevel(ctx, controlapi.LogLevelRequest{Level: level})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func restartCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: routerctl restart <daemon> [options]")
		return errors.New("restart requires <daemon>")
	}
	switch args[0] {
	case "dns-resolver", "dnsresolver", "dns":
		return restartDNSResolverCommand(args[1:], stdout)
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  routerctl restart dns-resolver [name] [--config <path>]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Examples:")
		fmt.Fprintln(stdout, "  routerctl restart dns-resolver")
		fmt.Fprintln(stdout, "  routerctl restart dns-resolver lan")
		return nil
	default:
		return fmt.Errorf("unknown restart daemon %q", args[0])
	}
}

func restartDNSResolverCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("restart dns-resolver", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"DNSResolver resource ごとに紐付く systemd unit (routerd-dns-resolver@<name>) を restart する。\n"+
				"位置引数: [name] (DNSResolver 名。1 個しかない場合は省略可)",
			"routerctl restart dns-resolver\n"+
				"routerctl restart dns-resolver lan\n"+
				"routerctl restart dns-resolver --config /etc/routerd/config.yaml")
	}
	configPath := fs.String("config", defaultConfigPath(), "config path")
	timeout := fs.Duration("timeout", 30*time.Second, "restart timeout")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() > 1 {
		return errors.New("restart dns-resolver accepts at most one resolver name")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	name := ""
	if fs.NArg() == 1 {
		name = strings.TrimSpace(fs.Arg(0))
	}
	names, err := dnsResolverResourceNames(router)
	if err != nil {
		return err
	}
	if name == "" {
		switch len(names) {
		case 0:
			return errors.New("no DNSResolver resources found")
		case 1:
			name = names[0]
		default:
			return fmt.Errorf("multiple DNSResolver resources found; specify one of: %s", strings.Join(names, ", "))
		}
	}
	if !containsString(names, name) {
		return fmt.Errorf("DNSResolver %q not found", name)
	}
	_, features := platform.Current()
	manager := servicemgr.ForPlatform(features)
	service := servicemgr.Service{SystemdName: "routerd-dns-resolver@" + name + ".service"}
	command := manager.Command(servicemgr.OperationRestart, service)
	if command.Name == "" {
		return fmt.Errorf("restart unsupported for %s service manager", manager.Name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, hostcmd.Resolve(command.Name), command.Args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", command.Name, strings.Join(command.Args, " "), err, strings.TrimSpace(string(out)))
	}
	fmt.Fprintf(stdout, "restarted DNSResolver/%s via %s\n", name, manager.Name())
	return nil
}

func dnsResolverResourceNames(router *api.Router) ([]string, error) {
	if router == nil {
		return nil, nil
	}
	var names []string
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSResolver" {
			continue
		}
		if _, err := resource.DNSResolverSpec(); err != nil {
			return nil, err
		}
		names = append(names, resource.Metadata.Name)
	}
	sort.Strings(names)
	return names, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func ingressCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: routerctl ingress <drain|undrain> ingress/<service> backend=<name>")
		return errors.New("ingress requires <drain|undrain>")
	}
	switch args[0] {
	case "drain":
		if len(args) > 1 && isHelpArg(args[1]) {
			printIngressHelp(stdout)
			return nil
		}
		return ingressDrainCommand(args[1:], stdout, true)
	case "undrain":
		if len(args) > 1 && isHelpArg(args[1]) {
			printIngressHelp(stdout)
			return nil
		}
		return ingressDrainCommand(args[1:], stdout, false)
	case "help", "-h", "--help":
		printIngressHelp(stdout)
		return nil
	default:
		return fmt.Errorf("unknown ingress command %q", args[0])
	}
}

func printIngressHelp(stdout io.Writer) {
	fmt.Fprintln(stdout, "Usage:")
	fmt.Fprintln(stdout, "  routerctl ingress drain ingress/<service> backend=<name> [--duration 10m] [--state-file <path>]")
	fmt.Fprintln(stdout, "  routerctl ingress undrain ingress/<service> backend=<name> [--state-file <path>]")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Examples:")
	fmt.Fprintln(stdout, "  routerctl ingress drain ingress/kubernetes-api backend=cp-01 --duration 10m")
	fmt.Fprintln(stdout, "  routerctl ingress undrain ingress/kubernetes-api --backend cp-01")
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "-h" || arg == "--help"
}

func ingressDrainCommand(args []string, stdout io.Writer, drain bool) error {
	statePath := defaultStatePath()
	var duration time.Duration
	var backend string
	var target string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--state-file":
			i++
			if i >= len(args) {
				return errors.New("--state-file requires a value")
			}
			statePath = args[i]
		case strings.HasPrefix(arg, "--state-file="):
			statePath = strings.TrimPrefix(arg, "--state-file=")
		case arg == "--duration":
			i++
			if i >= len(args) {
				return errors.New("--duration requires a value")
			}
			parsed, err := time.ParseDuration(args[i])
			if err != nil {
				return err
			}
			duration = parsed
		case strings.HasPrefix(arg, "--duration="):
			parsed, err := time.ParseDuration(strings.TrimPrefix(arg, "--duration="))
			if err != nil {
				return err
			}
			duration = parsed
		case arg == "--backend":
			i++
			if i >= len(args) {
				return errors.New("--backend requires a value")
			}
			backend = args[i]
		case strings.HasPrefix(arg, "--backend="):
			backend = strings.TrimPrefix(arg, "--backend=")
		case strings.HasPrefix(arg, "backend="):
			backend = strings.TrimPrefix(arg, "backend=")
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown option %q", arg)
		default:
			if target != "" {
				return fmt.Errorf("unexpected argument %q", arg)
			}
			target = arg
		}
	}
	if target == "" {
		if drain {
			return errors.New("ingress drain requires ingress/<service> backend=<name>")
		}
		return errors.New("ingress undrain requires ingress/<service> backend=<name>")
	}
	kind, service, err := parseResourceTarget("drain", target)
	if err != nil {
		return err
	}
	if kind != "IngressService" || strings.TrimSpace(service) == "" {
		return fmt.Errorf("drain target must be ingress/<service>")
	}
	if strings.TrimSpace(backend) == "" {
		return errors.New("backend=<name> is required")
	}
	store, err := routerstate.Load(statePath)
	if err != nil {
		return err
	}
	if drain {
		state, err := ingressdrain.Drain(store, service, backend, duration)
		if err != nil {
			return err
		}
		if err := store.Save(statePath); err != nil {
			return err
		}
		return writeJSON(stdout, state)
	}
	if err := ingressdrain.Undrain(store, service, backend); err != nil {
		return err
	}
	if err := store.Save(statePath); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]any{"service": service, "backend": backend, "drained": false})
}
