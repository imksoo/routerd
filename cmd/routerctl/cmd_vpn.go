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
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/tailscale"
	"github.com/imksoo/routerd/pkg/wireguard"
)

func vpnCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: routerctl vpn <wireguard|tailscale> ...")
		return errors.New("vpn requires <wireguard|tailscale>")
	}
	switch args[0] {
	case "wireguard", "wg":
		return wireGuardCommand(args[1:], stdout, stderr)
	case "tailscale", "ts":
		return tailscaleCommand(args[1:], stdout, stderr)
	case "help", "-h", "--help":
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  routerctl vpn wireguard list [-o table|json|yaml]")
		fmt.Fprintln(stdout, "  routerctl vpn wireguard show <interface> [-o table|json|yaml]")
		fmt.Fprintln(stdout, "  routerctl vpn tailscale peers [-o table|json|yaml] [--binary tailscale]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "Examples:")
		fmt.Fprintln(stdout, "  routerctl vpn wireguard list")
		fmt.Fprintln(stdout, "  routerctl vpn tailscale peers -o json")
		return nil
	default:
		return fmt.Errorf("unknown vpn command %q", args[0])
	}
}

func tailscaleCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"peers"}
	}
	switch args[0] {
	case "peers", "peer", "status":
		return tailscalePeersCommand(args[1:], stdout)
	case "help", "-h", "--help":
		fmt.Fprintln(stderr, "usage: routerctl vpn tailscale peers [-o table|json|yaml] [--binary tailscale]")
		return nil
	default:
		return fmt.Errorf("unknown tailscale command %q", args[0])
	}
}

func tailscalePeersCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vpn tailscale peers", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"tailscale peers の status を 'tailscale status --json' から取得して整形表示する。",
			"routerctl vpn tailscale peers\n"+
				"routerctl vpn tailscale peers -o json\n"+
				"routerctl vpn tailscale peers --binary /usr/local/bin/tailscale")
	}
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	binary := fs.String("binary", "tailscale", "tailscale binary path")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, hostcmd.Resolve(*binary), "status", "--json").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s status --json: %w: %s", *binary, err, strings.TrimSpace(string(out)))
	}
	status, err := tailscale.ParseStatusJSON(out)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeTailscalePeersTable(stdout, status)
	case "json":
		return writeJSON(stdout, status)
	case "yaml":
		return writeYAML(stdout, status)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeTailscalePeersTable(stdout io.Writer, status tailscale.Status) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PEER\tSTATUS\tTAILSCALE IP\tALLOWED ROUTES\tRELAY\tLAST SEEN")
	for _, peer := range status.Peers {
		state := "offline"
		if peer.Online {
			state = "online"
		}
		if peer.Active {
			state += ",active"
		}
		lastSeen := "-"
		if seen, err := time.Parse(time.RFC3339Nano, peer.LastSeen); err == nil && !seen.IsZero() {
			lastSeen = time.Since(seen).Round(time.Second).String() + " ago"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			displayCell(firstNonEmpty(peer.HostName, peer.DNSName, peer.ID)),
			state,
			displayCell(strings.Join(peer.TailscaleIPs, ",")),
			displayCell(strings.Join(peer.AllowedIPs, ",")),
			displayCell(peer.Relay),
			lastSeen,
		)
	}
	return w.Flush()
}

func wireGuardCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list", "ls":
		return wireGuardListCommand(args[1:], stdout)
	case "show":
		return wireGuardShowCommand(args[1:], stdout)
	case "help", "-h", "--help":
		fmt.Fprintln(stderr, "usage: routerctl vpn wireguard list [-o table|json|yaml]")
		fmt.Fprintln(stderr, "       routerctl vpn wireguard show <interface> [-o table|json|yaml]")
		return nil
	default:
		return fmt.Errorf("unknown wireguard command %q", args[0])
	}
}

func wireGuardListCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vpn wireguard list", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"WireGuard 全 interface / peer の状態を 'wg show all dump' から取得して表示する。",
			"routerctl vpn wireguard list\n"+
				"routerctl vpn wireguard list -o json")
	}
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, hostcmd.Resolve("wg"), "show", "all", "dump").CombinedOutput()
	if err != nil {
		return fmt.Errorf("wg show all dump: %w: %s", err, strings.TrimSpace(string(out)))
	}
	status, err := wireguard.ParseAllDump(out)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeWireGuardTable(stdout, status)
	case "json":
		return writeJSON(stdout, status)
	case "yaml":
		return writeYAML(stdout, status)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func wireGuardShowCommand(args []string, stdout io.Writer) error {
	output := "table"
	timeout := 5 * time.Second
	var iface string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-o" || arg == "--output":
			i++
			if i >= len(args) {
				return fmt.Errorf("%s requires a value", arg)
			}
			output = args[i]
		case strings.HasPrefix(arg, "-o="):
			output = strings.TrimPrefix(arg, "-o=")
		case strings.HasPrefix(arg, "--output="):
			output = strings.TrimPrefix(arg, "--output=")
		case arg == "--timeout":
			i++
			if i >= len(args) {
				return errors.New("--timeout requires a value")
			}
			parsed, err := time.ParseDuration(args[i])
			if err != nil {
				return err
			}
			timeout = parsed
		case strings.HasPrefix(arg, "--timeout="):
			parsed, err := time.ParseDuration(strings.TrimPrefix(arg, "--timeout="))
			if err != nil {
				return err
			}
			timeout = parsed
		case strings.HasPrefix(arg, "-"):
			return fmt.Errorf("unknown vpn wireguard show flag %q", arg)
		default:
			if iface != "" {
				return errors.New("vpn wireguard show requires exactly one <interface>")
			}
			iface = arg
		}
	}
	if iface == "" {
		return errors.New("vpn wireguard show requires <interface>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, hostcmd.Resolve("wg"), "show", iface, "dump").CombinedOutput()
	if err != nil {
		return fmt.Errorf("wg show %s dump: %w: %s", iface, err, strings.TrimSpace(string(out)))
	}
	status, err := wireguard.ParseInterfaceDump(iface, out)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeWireGuardTable(stdout, []wireguard.InterfaceStatus{status})
	case "json":
		return writeJSON(stdout, status)
	case "yaml":
		return writeYAML(stdout, status)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeWireGuardTable(stdout io.Writer, interfaces []wireguard.InterfaceStatus) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "INTERFACE\tLISTEN\tPUBLIC KEY\tPEERS")
	for _, iface := range interfaces {
		fmt.Fprintf(w, "%s\t%d\t%s\t%d\n", displayCell(iface.Name), iface.ListenPort, truncateKey(iface.PublicKey), len(iface.Peers))
		for _, peer := range iface.Peers {
			handshake := "-"
			if !peer.LatestHandshake.IsZero() {
				handshake = time.Since(peer.LatestHandshake).Round(time.Second).String() + " ago"
			}
			fmt.Fprintf(w, "  peer\t%s\t%s\t%s rx=%d tx=%d\n", truncateKey(peer.PublicKey), displayCell(peer.LatestEndpoint), handshake, peer.TransferRxBytes, peer.TransferTxBytes)
		}
	}
	return w.Flush()
}

func truncateKey(key string) string {
	if len(key) <= 16 {
		return displayCell(key)
	}
	return key[:8] + "..." + key[len(key)-6:]
}
