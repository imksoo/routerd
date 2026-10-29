// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type diagnoseOptions struct {
	Target          string
	Output          string
	ConfigPath      string
	StatePath       string
	Host            bool
	Incident        bool
	RemediationPlan bool
	Server          string
	Names           string
	Timeout         time.Duration
	Since           time.Duration
	Socket          string
	Probe           string
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
			summary = "control API 経由で EgressRoutePolicy の観測済み status を表示する。\n" +
				"現在のネットワーク疎通は未計測として表示する。\n" +
				"位置引数: [policy] (EgressRoutePolicy 名。省略時は全 policy 対象)"
			examples = "routerctl doctor --probe egress\n" +
				"routerctl doctor --probe egress ipv4-default -o json\n" +
				"routerctl doctor --probe egress -o yaml"
		case "diagnose dns":
			summary = "control API 経由で DNSResolver の観測済み status を表示する。実 DNS query は未計測。\n" +
				"位置引数: [resolver] (DNSResolver 名。省略時は全 resolver 対象)"
			examples = "routerctl doctor --probe dns\n" +
				"routerctl doctor --probe dns lan -o json\n" +
				"routerctl doctor --probe dns -o json"
		case "diagnose lan-client":
			summary = "LAN client の IP を検証し、現在のネットワーク疎通は未計測として表示する。\n" +
				"位置引数: <ip> (LAN client IP, 必須)"
			examples = "routerctl doctor --probe lan-client 192.168.1.10\n" +
				"routerctl doctor --probe lan-client 192.168.1.10 -o yaml\n" +
				"routerctl doctor --probe lan-client 192.168.1.10 -o json"
		case "doctor":
			summary = "routerd の各 area (" + strings.Join(doctorAreas, "/") + ") の\n" +
				"健全性チェックをまとめて実行する。\n" +
				"--incident を付けると host ダンプ(診断コマンド出力/イベント/オブジェクトステータス/プラグイン実行履歴)を付加する。\n" +
				"--remediation-plan を付けると federation SLO 違反に対する修復アクション計画を生成する(実行はしない)。\n" +
				"位置引数: [area] (省略時は全 area)。--probe は control API 経由の対象別 probe を実行する。"
			examples = "routerctl doctor\n" +
				"routerctl doctor wan\n" +
				"routerctl doctor --incident\n" +
				"routerctl doctor federation --remediation-plan -o json\n" +
				"routerctl doctor --probe egress ipv4-default\n" +
				"routerctl doctor --probe dns -o json"
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
	fs.DurationVar(&opts.Since, "since", 0, "doctor reconcile area: only count errors newer than this duration (e.g. 1h, 24h)")
	fs.StringVar(&opts.Socket, "status-socket", defaultStatusSocketPath(), "doctor reconcile area: routerd read-only status Unix domain socket path")
	fs.StringVar(&opts.Socket, "socket", defaultStatusSocketPath(), "routerd read-only status Unix domain socket path")
	fs.StringVar(&opts.Probe, "probe", "", "doctor probe subject (egress, dns, lan-client)")
	fs.BoolVar(&opts.Incident, "incident", false, "collect incident debug dump (status/events/object status/plugin runs and host command outputs)")
	fs.BoolVar(&opts.RemediationPlan, "remediation-plan", false, "generate a remediation plan for federation SLO violations (plan-only, no execution)")
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
		"--since": true, "--status-socket": true, "--socket": true, "--probe": true,
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
	store, err := routerstate.LoadReadOnlyLive(opts.StatePath)
	if err != nil {
		return nil, nil, err
	}
	return router, store, nil
}

func runDiagnosticCommand(ctx context.Context, label, name string, args ...string) diagnoseCommandCheck {
	stdoutData, stderrData, err := runHostCommand(ctx, name, args...)
	stdout := strings.TrimSpace(string(stdoutData))
	stderr := strings.TrimSpace(string(stderrData))
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

func compactDiagnoseMap(values map[string]any) string {
	if len(values) == 0 {
		return "-"
	}
	var parts []string
	for _, key := range mapsort.Keys(values) {
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
