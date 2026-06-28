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
	case "federation", "fed":
		return federationCommand(args[1:], stdout, stderr)
	case "ledger":
		return ledgerCommand(args[1:], stdout, stderr)
	case "get":
		return getCommand(args[1:], stdout, stderr)
	case "describe":
		return describeCommand(args[1:], stdout, stderr)
	case "firewall":
		return firewallCommand(args[1:], stdout, stderr)
	case "dynamic":
		return dynamicCommand(args[1:], stdout, stderr)
	case "render":
		return renderCommand(args[1:], stdout)
	case "mobility":
		return mobilityCommand(args[1:], stdout, stderr)
	case "plugin":
		return pluginCommand(args[1:], stdout, stderr)
	case "action":
		return actionCommand(args[1:], stdout, stderr)
	case "vpn":
		return vpnCommand(args[1:], stdout, stderr)
	case "doctor":
		return doctorCommand(args[1:], stdout, stderr)
	case "ingress":
		return ingressCommand(args[1:], stdout, stderr)
	case "validate":
		return validateCommand(args[1:], stdout, os.Stdin)
	case "plan":
		return planCommand(args[1:], stdout, os.Stdin)
	case "apply":
		return applyCommand(args[1:], stdout)
	case "delete":
		return deleteCommand(args[1:], stdout)
	case "rollback":
		return rollbackCommand(args[1:], stdout)
	case "adopt":
		return adoptCommand(args[1:], stdout)
	case "log-level":
		return setLogLevelCommand(args[1:], stdout)
	case "restart":
		return restartCommand(args[1:], stdout, stderr)
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
	fmt.Fprintln(w, "  federation event emit --group <name> --type <topic> [--subject <s>] [--source-node <n>] [--payload k=v ...] [--ttl <dur>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  federation event list [--group <name>] [--include-expired] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  federation deliveries summary [--group <name>] [--peer <name>] [--type <type>] [--include-expired] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  ledger integrity-check [--state-file <path>] [-o table|json]")
	fmt.Fprintln(w, "  ledger vacuum [--state-file <path>]")
	fmt.Fprintln(w, "  ledger backup <dest-path> [--state-file <path>]")
	fmt.Fprintln(w, "  ledger prune-events --older-than <duration> [--state-file <path>] [--dry-run]")
	fmt.Fprintln(w, "  get <subject|kind[/name]> [--socket <path>] [--limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  get events|ledger|connections|dns-queries|traffic-flows|firewall-logs [--socket <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe <kind>/<name> [--socket <path>] [--events-limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe firewall [--config <path>]")
	fmt.Fprintln(w, "  firewall test from=<zone> to=<zone|self> proto=<tcp|udp> dport=<port> [--config <path>]")
	fmt.Fprintln(w, "  dynamic list [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  dynamic describe <source> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  dynamic render [--config <path>] [--state-file <path>] [-o yaml|json]")
	fmt.Fprintln(w, "  dynamic diff [--config <path>] [--state-file <path>] [-o text|json]")
	fmt.Fprintln(w, "  mobility owners [--pool <name>] [--address <ipv4/32>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  mobility explain --pool <name> --address <ipv4/32> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  mobility paths [--prefix <prefix>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  mobility traps [--address <ipv4/32>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  mobility enrollment-hmac --config <path> --claim <name> (--secret-file <path>|--secret-env <name>|--secret <value>) [--show-payload]")
	fmt.Fprintln(w, "  plugin list [--config <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  plugin run <name> [--dry-run] [--config <path>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action import [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  action list [--status <s>] [--provider <p>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action show <id> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action approve <id> [--by <name>] [--state-file <path>]")
	fmt.Fprintln(w, "  action execute <id> --dry-run|--approved [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  action journal [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  action rollback <id> --dry-run|--approved [--config <path>] [--state-file <path>]")
	fmt.Fprintln(w, "  vpn wireguard list [-o table|json|yaml]")
	fmt.Fprintln(w, "  vpn wireguard show <interface> [-o table|json|yaml]")
	fmt.Fprintln(w, "  vpn tailscale peers [-o table|json|yaml] [--binary tailscale]")
	fmt.Fprintln(w, "  doctor [area] [--probe <subject>] [--socket <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  ingress drain ingress/<service> backend=<name> [--duration 10m] [--state-file <path>]")
	fmt.Fprintln(w, "  ingress undrain ingress/<service> backend=<name> [--state-file <path>]")
	fmt.Fprintln(w, "  validate [-f <file|-] [--socket <path>] [--replace]")
	fmt.Fprintln(w, "  plan [-f <file|-] [--socket <path>] [--replace]")
	fmt.Fprintln(w, "  apply -f <file|-> [--socket <path>] [--replace] [--no-reconcile]")
	fmt.Fprintln(w, "  delete <kind>/<name> [--socket <path>] [--dry-run] [--force] [--api-version <version>] [--no-reconcile]")
	fmt.Fprintln(w, "  rollback [--list] [--to <generation>] [--socket <path>] [--state-file <path>] [--no-reconcile]")
	fmt.Fprintln(w, "  adopt --candidates|--apply [--config <path>] [--ledger-file <path>]")
	fmt.Fprintln(w, "  log-level <debug|info|warning|error|default> [--socket <path>]")
	fmt.Fprintln(w, "  restart dns-resolver [name] [--config <path>]")
}
