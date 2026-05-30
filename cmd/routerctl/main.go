// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/imksoo/routerd/pkg/platform"
	routerversion "github.com/imksoo/routerd/pkg/version"
)

var platformDefaults, _ = platform.Current()

var version = routerversion.String()

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
	case "version", "--version":
		fmt.Fprintf(stdout, "routerctl %s\n", version)
		return nil
	case "status":
		return statusCommand(args[1:], stdout)
	case "events":
		return eventsCommand(args[1:], stdout)
	case "federation", "fed":
		return federationCommand(args[1:], stdout, stderr)
	case "ledger":
		return ledgerCommand(args[1:], stdout, stderr)
	case "dns-queries":
		return dnsQueriesCommand(args[1:], stdout)
	case "connections":
		return connectionsCommand(args[1:], stdout)
	case "traffic-flows":
		return trafficFlowsCommand(args[1:], stdout)
	case "firewall-logs":
		return firewallLogsCommand(args[1:], stdout)
	case "get":
		return getCommand(args[1:], stdout, stderr)
	case "describe":
		return describeCommand(args[1:], stdout, stderr)
	case "firewall":
		return firewallCommand(args[1:], stdout, stderr)
	case "dynamic":
		return dynamicCommand(args[1:], stdout, stderr)
	case "plugin":
		return pluginCommand(args[1:], stdout, stderr)
	case "action":
		return actionCommand(args[1:], stdout, stderr)
	case "wireguard", "wg":
		return wireGuardCommand(args[1:], stdout, stderr)
	case "tailscale", "ts":
		return tailscaleCommand(args[1:], stdout, stderr)
	case "diagnose":
		return diagnoseCommand(args[1:], stdout, stderr)
	case "doctor":
		return doctorCommand(args[1:], stdout, stderr)
	case "show":
		return showCommand(args[1:], stdout, stderr)
	case "drain":
		return ingressDrainCommand(args[1:], stdout, true)
	case "undrain":
		return ingressDrainCommand(args[1:], stdout, false)
	case "apply":
		return applyCommand(args[1:], stdout)
	case "delete":
		return deleteCommand(args[1:], stdout)
	case "set-log-level":
		return setLogLevelCommand(args[1:], stdout)
	case "restart-dns-resolver":
		return restartDNSResolverCommand(args[1:], stdout)
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

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  status [--socket <path>] [--json|-o json|yaml]")
	fmt.Fprintln(w, "  events [--state-file <path>] [--topic <topic>] [--resource <kind>/<name>] [--limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  federation event emit --group <name> --type <topic> [--subject <s>] [--source-node <n>] [--payload k=v ...] [--ttl <dur>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  federation event list [--group <name>] [--include-expired] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  ledger integrity-check [--state-file <path>] [-o table|json]")
	fmt.Fprintln(w, "  ledger vacuum [--state-file <path>]")
	fmt.Fprintln(w, "  ledger backup <dest-path> [--state-file <path>]")
	fmt.Fprintln(w, "  ledger prune-events --older-than <duration> [--state-file <path>] [--dry-run]")
	fmt.Fprintln(w, "  dns-queries [--socket <path>] [--db <path>] [--since 1h] [--client <ip>] [--qname <pattern>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  connections [--socket <path>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  traffic-flows [--socket <path>] [--db <path>] [--since 1h] [--client <ip>] [--peer <ip>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  firewall-logs [--socket <path>] [--db <path>] [--since 1h] [--action drop] [--src <ip>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  get <kind>[/<name>] [--list-kinds] [--config <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe <kind>/<name> [--config <path>] [--state-file <path>] [--ledger-file <path>] [--events-limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe firewall [--config <path>]")
	fmt.Fprintln(w, "  firewall test from=<zone> to=<zone|self> proto=<tcp|udp> dport=<port> [--config <path>]")
	fmt.Fprintln(w, "  dynamic list [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  dynamic describe <source> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  dynamic render [--config <path>] [--state-file <path>] [-o yaml|json]")
	fmt.Fprintln(w, "  dynamic diff [--config <path>] [--state-file <path>] [-o text|json]")
	fmt.Fprintln(w, "  plugin list [--config <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  plugin run <name> [--dry-run] [--config <path>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action import [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  action list [--status <s>] [--provider <p>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action show <id> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action approve <id> [--by <name>] [--state-file <path>]")
	fmt.Fprintln(w, "  action execute <id> --dry-run|--approved [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  action journal [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action rollback <id> --dry-run|--approved [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  wireguard list [-o table|json|yaml]")
	fmt.Fprintln(w, "  wireguard show <interface> [-o table|json|yaml]")
	fmt.Fprintln(w, "  tailscale peers [-o table|json|yaml] [--binary tailscale]")
	fmt.Fprintln(w, "  diagnose egress [policy] [--config <path>] [--state-file <path>] [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  diagnose dns [resolver] [--server <addr>] [--name <fqdn>] [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  diagnose lan-client <ip> [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  doctor [area] [--config <path>] [--state-file <path>] [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show bgp|vrrp|ingress|derived-resources [--config <path>] [--state-file <path>] [--include-stale] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <kind> [--config <path>] [--state-file <path>] [--ledger-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <kind>/<name> [--diff|--ledger|--adopt|--events|--spec|--status] [-o table|json|yaml]")
	fmt.Fprintln(w, "  drain ingress/<service> backend=<name> [--duration 10m] [--state-file <path>]")
	fmt.Fprintln(w, "  undrain ingress/<service> backend=<name> [--state-file <path>]")
	fmt.Fprintln(w, "  plan [--socket <path>]")
	fmt.Fprintln(w, "  apply [--socket <path>] [--dry-run]")
	fmt.Fprintln(w, "  delete <kind>/<name> [--socket <path>] [--dry-run] [--force] [--api-version <version>]")
	fmt.Fprintln(w, "  set-log-level <debug|info|warning|error|default> [--socket <path>]")
	fmt.Fprintln(w, "  restart-dns-resolver [name] [--config <path>]")
}
