// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"routerd/internal/hostcmd"
	"routerd/pkg/api"
	"routerd/pkg/apply"
	bgpstate "routerd/pkg/bgp"
	"routerd/pkg/config"
	"routerd/pkg/controlapi"
	"routerd/pkg/ingressdrain"
	"routerd/pkg/logstore"
	"routerd/pkg/observe"
	"routerd/pkg/platform"
	"routerd/pkg/render"
	"routerd/pkg/resource"
	routerstate "routerd/pkg/state"
	"routerd/pkg/tailscale"
	routerversion "routerd/pkg/version"
	"routerd/pkg/wireguard"
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
	case "wireguard", "wg":
		return wireGuardCommand(args[1:], stdout, stderr)
	case "tailscale", "ts":
		return tailscaleCommand(args[1:], stdout, stderr)
	case "diagnose":
		return diagnoseCommand(args[1:], stdout, stderr)
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

func tailscaleCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		args = []string{"peers"}
	}
	switch args[0] {
	case "peers", "peer", "status":
		return tailscalePeersCommand(args[1:], stdout)
	case "help", "-h", "--help":
		fmt.Fprintln(stderr, "usage: routerctl tailscale peers [-o table|json|yaml] [--binary tailscale]")
		return nil
	default:
		return fmt.Errorf("unknown tailscale command %q", args[0])
	}
}

func tailscalePeersCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("tailscale peers", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	binary := fs.String("binary", "tailscale", "tailscale binary path")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
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

func statusCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultStatusSocketPath(), "routerd read-only status Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	output := "json"
	jsonOutput := fs.Bool("json", false, "output JSON")
	fs.StringVar(&output, "o", output, "output format: json, yaml")
	fs.StringVar(&output, "output", output, "output format: json, yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *jsonOutput {
		output = "json"
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	status, err := controlapi.NewUnixClient(*socketPath).Status(ctx)
	if err != nil {
		return err
	}
	switch output {
	case "", "json":
		return writeJSON(stdout, status)
	case "yaml":
		return writeYAML(stdout, status)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
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

func deleteCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("delete", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	dryRun := fs.Bool("dry-run", false, "show what would be deleted without changing host state")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("delete requires <kind>/<name>")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Delete(ctx, controlapi.DeleteRequest{Target: fs.Arg(0), DryRun: *dryRun})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
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
			return errors.New("drain requires ingress/<service> backend=<name>")
		}
		return errors.New("undrain requires ingress/<service> backend=<name>")
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

func eventsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	limit := fs.Int("limit", 50, "maximum number of events")
	since := fs.Int64("since", 0, "show events with id greater than this value")
	topic := fs.String("topic", "", "event topic")
	resource := fs.String("resource", "", "resource filter as <kind>/<name>")
	kind := fs.String("kind", "", "legacy event kind filter")
	name := fs.String("name", "", "legacy event name filter")
	if err := fs.Parse(args); err != nil {
		return err
	}
	store, err := routerstate.Open(*statePath)
	if err != nil {
		return err
	}
	if closer, ok := store.(interface{ Close() error }); ok {
		defer closer.Close()
	}
	lister, ok := store.(routerstate.EventLister)
	if !ok {
		return fmt.Errorf("state file %s does not support event listing", *statePath)
	}
	events, err := lister.ListEvents(routerstate.EventQuery{
		Limit:    *limit,
		SinceID:  *since,
		Topic:    *topic,
		Kind:     *kind,
		Name:     *name,
		Resource: *resource,
	})
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeEventsTable(stdout, events)
	case "json":
		return writeJSON(stdout, events)
	case "yaml":
		return writeYAML(stdout, events)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeEventsTable(stdout io.Writer, events []routerstate.StoredEvent) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIME\tSEVERITY\tTOPIC\tRESOURCE\tREASON\tMESSAGE")
	for _, event := range events {
		resource := event.Kind + "/" + event.Name
		if event.ResourceKind != "" && event.ResourceName != "" {
			resource = event.ResourceKind + "/" + event.ResourceName
		}
		topic := firstNonEmpty(event.Topic, event.Type)
		severity := firstNonEmpty(event.Severity, event.Type)
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			event.ID,
			event.CreatedAt.Format(time.RFC3339),
			severity,
			topic,
			resource,
			event.Reason,
			event.Message,
		)
	}
	return w.Flush()
}

func dnsQueriesCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("dns-queries", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "read a DNS query log database file directly instead of using routerd")
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	since := fs.String("since", "1h", "show queries newer than duration, for example 1h or 30m")
	client := fs.String("client", "", "client IP address")
	qname := fs.String("qname", "", "question name LIKE pattern")
	limit := fs.Int("limit", 100, "maximum number of rows")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var rows []logstore.DNSQuery
	if strings.TrimSpace(*dbPath) != "" {
		sinceTime, err := cutoffTime(*since)
		if err != nil {
			return err
		}
		store, err := logstore.OpenDNSQueryLogReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer store.Close()
		rows, err = store.List(context.Background(), logstore.DNSQueryFilter{Since: sinceTime, Client: *client, QName: *qname, Limit: *limit})
		if err != nil {
			return err
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		result, err := controlapi.NewUnixClient(*socketPath).DNSQueries(ctx, controlapi.DNSQueriesRequest{Since: *since, Client: *client, QName: *qname, Limit: *limit})
		if err != nil {
			return err
		}
		rows = result.Items
	}
	switch output {
	case "", "table":
		return writeDNSQueriesTable(stdout, rows)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeDNSQueriesTable(stdout io.Writer, rows []logstore.DNSQuery) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tCLIENT\tQTYPE\tQNAME\tRCODE\tUPSTREAM\tCACHE\tDURATION")
	for _, row := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%t\t%s\n",
			row.Timestamp.Format(time.RFC3339),
			row.ClientAddress,
			row.QuestionType,
			row.QuestionName,
			row.ResponseCode,
			displayCell(row.Upstream),
			row.CacheHit,
			row.Duration,
		)
	}
	return w.Flush()
}

func connectionsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("connections", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	limit := fs.Int("limit", 100, "maximum number of entries")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).Connections(ctx, *limit)
	if err != nil {
		return err
	}
	switch output {
	case "", "table":
		return writeConnectionsTable(stdout, result.Status)
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeConnectionsTable(stdout io.Writer, table observe.ConnectionTable) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "COUNT\t%d", table.Count)
	if table.Max > 0 {
		fmt.Fprintf(w, "/%d", table.Max)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "FAMILY\tPROTO\tSTATE\tFLOW\tRETURN\tNAT\tTIMEOUT")
	for _, row := range table.Entries {
		state := firstNonEmpty(row.State, assuredState(row.Assured))
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n",
			displayCell(row.Family),
			displayCell(row.Protocol),
			displayCell(state),
			tupleCell(row.Original),
			tupleCell(row.Reply),
			displayCell(connectionNATDelta(row)),
			row.Timeout,
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
		fmt.Fprintln(stderr, "usage: routerctl wireguard list [-o table|json|yaml]")
		fmt.Fprintln(stderr, "       routerctl wireguard show <interface> [-o table|json|yaml]")
		return nil
	default:
		return fmt.Errorf("unknown wireguard command %q", args[0])
	}
}

func wireGuardListCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("wireguard list", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
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
			return fmt.Errorf("unknown wireguard show flag %q", arg)
		default:
			if iface != "" {
				return errors.New("wireguard show requires exactly one <interface>")
			}
			iface = arg
		}
	}
	if iface == "" {
		return errors.New("wireguard show requires <interface>")
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

func assuredState(assured bool) string {
	if assured {
		return "ASSURED"
	}
	return "stateless"
}

func tupleCell(tuple observe.ConntrackTuple) string {
	src := endpointCell(tuple.Source, tuple.SourcePort)
	dst := endpointCell(tuple.Destination, tuple.DestinationPort)
	if src == "-" && dst == "-" {
		return "-"
	}
	return src + " -> " + dst
}

func endpointCell(address, port string) string {
	if strings.TrimSpace(address) == "" {
		return "-"
	}
	if strings.TrimSpace(port) == "" {
		return address
	}
	return address + ":" + port
}

func connectionNATDelta(entry observe.ConnectionEntry) string {
	var out []string
	if entry.Reply.Destination != "" && entry.Reply.Destination != entry.Original.Source {
		out = append(out, "reply-dst="+endpointCell(entry.Reply.Destination, entry.Reply.DestinationPort))
	}
	if entry.Reply.Source != "" && entry.Reply.Source != entry.Original.Destination {
		out = append(out, "reply-src="+endpointCell(entry.Reply.Source, entry.Reply.SourcePort))
	}
	return strings.Join(out, ",")
}

func trafficFlowsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("traffic-flows", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "read a traffic flow log database file directly instead of using routerd")
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	since := fs.String("since", "1h", "show flows newer than duration, for example 1h or 30m")
	client := fs.String("client", "", "client IP address")
	peer := fs.String("peer", "", "peer IP address")
	limit := fs.Int("limit", 100, "maximum number of rows")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var rows []logstore.TrafficFlow
	if strings.TrimSpace(*dbPath) != "" {
		sinceTime, err := cutoffTime(*since)
		if err != nil {
			return err
		}
		store, err := logstore.OpenTrafficFlowLogReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer store.Close()
		rows, err = store.List(context.Background(), logstore.TrafficFlowFilter{Since: sinceTime, Client: *client, Peer: *peer, Limit: *limit})
		if err != nil {
			return err
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		result, err := controlapi.NewUnixClient(*socketPath).TrafficFlows(ctx, controlapi.TrafficFlowsRequest{Since: *since, Client: *client, Peer: *peer, Limit: *limit})
		if err != nil {
			return err
		}
		rows = result.Items
	}
	switch output {
	case "", "table":
		return writeTrafficFlowsTable(stdout, rows)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeTrafficFlowsTable(stdout io.Writer, rows []logstore.TrafficFlow) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "STARTED\tENDED\tPROTO\tCLIENT\tPEER\tNAT\tBYTES_OUT\tBYTES_IN\tHOST")
	for _, row := range rows {
		ended := "-"
		if !row.EndedAt.IsZero() {
			ended = row.EndedAt.Format(time.RFC3339)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s:%d\t%s:%d\t%s\t%d\t%d\t%s\n",
			row.StartedAt.Format(time.RFC3339),
			ended,
			row.Protocol,
			row.ClientAddress,
			row.ClientPort,
			row.PeerAddress,
			row.PeerPort,
			displayCell(row.NATTranslatedAddress),
			row.BytesOut,
			row.BytesIn,
			displayCell(firstNonEmpty(row.TLSSNI, row.ResolvedHostname)),
		)
	}
	return w.Flush()
}

func firewallLogsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("firewall-logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbPath := fs.String("db", "", "read a firewall log database file directly instead of using routerd")
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	since := fs.String("since", "1h", "show logs newer than duration, for example 1h or 30m")
	action := fs.String("action", "", "filter by action: accept, drop, reject")
	src := fs.String("src", "", "source IP address")
	limit := fs.Int("limit", 100, "maximum number of rows")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var rows []logstore.FirewallLogEntry
	if strings.TrimSpace(*dbPath) != "" {
		sinceTime, err := cutoffTime(*since)
		if err != nil {
			return err
		}
		store, err := logstore.OpenFirewallLogReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer store.Close()
		rows, err = store.List(context.Background(), logstore.FirewallLogFilter{Since: sinceTime, Action: *action, Src: *src, Limit: *limit})
		if err != nil {
			return err
		}
	} else {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		result, err := controlapi.NewUnixClient(*socketPath).FirewallLogs(ctx, controlapi.FirewallLogsRequest{Since: *since, Action: *action, Src: *src, Limit: *limit})
		if err != nil {
			return err
		}
		rows = result.Items
	}
	switch output {
	case "", "table":
		return writeFirewallLogsTable(stdout, rows)
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeFirewallLogsTable(stdout io.Writer, rows []logstore.FirewallLogEntry) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TIME\tACTION\tPROTO\tSRC\tDST\tZONE\tRULE\tIFACE")
	for _, row := range rows {
		zone := displayCell(row.ZoneFrom + ">" + row.ZoneTo)
		if row.ZoneFrom == "" && row.ZoneTo == "" {
			zone = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s:%d\t%s:%d\t%s\t%s\t%s>%s\n",
			row.Timestamp.Format(time.RFC3339),
			row.Action,
			row.Protocol,
			row.SrcAddress,
			row.SrcPort,
			row.DstAddress,
			row.DstPort,
			zone,
			displayCell(row.RuleName),
			displayCell(row.InIface),
			displayCell(row.OutIface),
		)
	}
	return w.Flush()
}

func cutoffTime(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, nil
	}
	duration, err := parseHumanDuration(value)
	if err != nil {
		return time.Time{}, err
	}
	return time.Now().Add(-duration).UTC(), nil
}

func parseHumanDuration(value string) (time.Duration, error) {
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil {
			return 0, err
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

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
	ConnectionsLimit int
}

type showResource struct {
	APIVersion string              `json:"apiVersion" yaml:"apiVersion"`
	Kind       string              `json:"kind" yaml:"kind"`
	Name       string              `json:"name" yaml:"name"`
	Source     string              `json:"source,omitempty" yaml:"source,omitempty"`
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
	if kind == "FirewallPolicy" && (name == "" || name == "firewall") {
		return describeFirewall(stdout, router)
	}
	if kind == "Orphan" {
		return writeOrphans(stdout, router, ledger)
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
	rows, err := buildShowResources(router, resources, store, ledger, showOptions{Events: true, ConnectionsLimit: 20})
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

func firewallCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stderr)
		return errors.New("firewall requires subcommand")
	}
	switch args[0] {
	case "test":
		return firewallTestCommand(args[1:], stdout)
	default:
		return fmt.Errorf("unknown firewall subcommand %q", args[0])
	}
}

func describeFirewall(stdout io.Writer, router *api.Router) error {
	holes := render.InternalFirewallHoles(router)
	fmt.Fprintln(stdout, "SOURCE\tFROM\tTO\tMATCH\tACTION")
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallRule" {
			continue
		}
		spec, err := res.FirewallRuleSpec()
		if err != nil {
			return err
		}
		fmt.Fprintf(w, "user/%s\t%s\t%s\t%s/%d\t%s\n", res.Metadata.Name, spec.FromZone, spec.ToZone, defaultString(spec.Protocol, "any"), spec.Port, spec.Action)
	}
	for _, hole := range holes {
		fmt.Fprintf(w, "internal/%s\t%s\t%s\t%s/%d\taccept\n", hole.Name, hole.FromZone, hole.ToZone, defaultString(hole.Protocol, "any"), hole.Port)
	}
	for _, from := range firewallZonesForCLI(router) {
		fmt.Fprintf(w, "implicit/matrix\t%s\tself\trole=%s\t%s\n", from.Name, from.Role, firewallImplicitActionForCLI(from.Role, "self"))
		for _, to := range firewallZonesForCLI(router) {
			fmt.Fprintf(w, "implicit/matrix\t%s\t%s\t%s->%s\t%s\n", from.Name, to.Name, from.Role, to.Role, firewallImplicitActionForCLI(from.Role, to.Role))
		}
	}
	return w.Flush()
}

func firewallTestCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("firewall test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("config", defaultConfigPath(), "config path")
	from := fs.String("from", "", "source zone")
	to := fs.String("to", "self", "destination zone")
	src := fs.String("src", "", "source zone alias")
	dst := fs.String("dst", "", "destination zone alias")
	proto := fs.String("proto", "", "protocol")
	dport := fs.Int("dport", 0, "destination port")
	var normalized []string
	for _, arg := range args {
		if strings.Contains(arg, "=") && !strings.HasPrefix(arg, "-") {
			parts := strings.SplitN(arg, "=", 2)
			normalized = append(normalized, "--"+parts[0], parts[1])
			continue
		}
		normalized = append(normalized, arg)
	}
	if err := fs.Parse(normalized); err != nil {
		return err
	}
	if *from == "" && *src != "" {
		*from = *src
	}
	if *to == "self" && *dst != "" {
		*to = *dst
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	action, source := firewallDecisionForCLI(router, *from, *to, *proto, *dport)
	fmt.Fprintf(stdout, "action=%s source=%s from=%s to=%s proto=%s dport=%d\n", action, source, *from, *to, *proto, *dport)
	return nil
}

type firewallZoneCLI struct {
	Name string
	Role string
}

func firewallZonesForCLI(router *api.Router) []firewallZoneCLI {
	var out []firewallZoneCLI
	for _, res := range router.Spec.Resources {
		if res.APIVersion == api.FirewallAPIVersion && res.Kind == "FirewallZone" {
			spec, err := res.FirewallZoneSpec()
			if err == nil {
				out = append(out, firewallZoneCLI{Name: res.Metadata.Name, Role: spec.Role})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func firewallDecisionForCLI(router *api.Router, from, to, proto string, dport int) (string, string) {
	for _, res := range router.Spec.Resources {
		if res.APIVersion != api.FirewallAPIVersion || res.Kind != "FirewallRule" {
			continue
		}
		spec, err := res.FirewallRuleSpec()
		if err != nil || spec.FromZone != from || spec.ToZone != to {
			continue
		}
		if spec.Protocol != "" && proto != "" && spec.Protocol != proto {
			continue
		}
		if spec.Port != 0 && dport != 0 && spec.Port != dport {
			continue
		}
		return spec.Action, "user/" + res.Metadata.Name
	}
	for _, hole := range render.InternalFirewallHoles(router) {
		if hole.FromZone == from && hole.ToZone == to && (hole.Protocol == "" || proto == "" || hole.Protocol == proto) && (hole.Port == 0 || dport == 0 || hole.Port == dport) {
			return "accept", "internal/" + hole.Name
		}
	}
	roles := map[string]string{}
	for _, zone := range firewallZonesForCLI(router) {
		roles[zone.Name] = zone.Role
	}
	return firewallImplicitActionForCLI(roles[from], roles[to]), "implicit/matrix"
}

func firewallImplicitActionForCLI(fromRole, toRole string) string {
	if toRole == "" || toRole == "self" {
		if fromRole == "mgmt" || fromRole == "trust" {
			return "accept"
		}
		return "drop"
	}
	if fromRole == toRole {
		return "accept"
	}
	if fromRole == "mgmt" {
		return "accept"
	}
	if fromRole == "trust" && toRole != "mgmt" {
		return "accept"
	}
	return "drop"
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
		rows, err := buildDerivedShowResources(router, store)
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

func buildDerivedShowResources(router *api.Router, store routerstate.Store) ([]showResource, error) {
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
		source := firstNonEmpty(statusString(status.Status["source"]), status.Owner)
		add(showResource{
			APIVersion: status.APIVersion,
			Kind:       status.Kind,
			Name:       status.Name,
			Source:     source,
			Observed:   status.Status,
			State:      status.Status,
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
		addServiceUnit(render.DPIClassifierUnitName, "TrafficFlowLog/FirewallLog")
	}
	if render.RouterWantsNDPIAgent(router) {
		addServiceUnit(render.NDPIAgentUnitName, "TrafficFlowLog/FirewallLog")
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
			spec, err := res.HealthCheckSpec()
			if err == nil && spec.Daemon == "routerd-healthcheck" {
				addServiceUnit("routerd-healthcheck@"+res.Metadata.Name+".service", "HealthCheck/"+res.Metadata.Name)
			}
		case "FirewallLog":
			spec, err := res.FirewallLogSpec()
			if err == nil && spec.Enabled {
				addServiceUnit("routerd-firewall-logger.service", "FirewallLog/"+res.Metadata.Name)
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
		detail := firstNonEmpty(statusString(status["path"]), statusString(status["unitName"]), "-")
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

func writeBGPShowTable(stdout io.Writer, router *api.Router, resources []routerstate.ObjectStatus) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	specs := bgpRouterSpecs(router)
	fmt.Fprintln(w, "ROUTER\tASN\tROUTER_ID\tPEERS_ESTABLISHED\tPREFIXES_ACCEPTED\tGR")
	var peers []map[string]any
	for _, resource := range resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		spec := specs[resource.Name]
		totalPeers := len(statusMaps(resource.Status["peers"]))
		fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%s\n",
			resource.Name,
			spec.ASN,
			spec.RouterID,
			establishedSummary(resource.Status, totalPeers),
			statusInt(resource.Status["acceptedPrefixes"]),
			enabledString(api.BoolDefault(spec.GracefulRestart.Enabled, true)),
		)
		for _, peer := range statusMaps(resource.Status["peers"]) {
			peer["_router"] = resource.Name
			peers = append(peers, peer)
		}
	}
	if len(peers) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "PEER\tAS\tSTATE\tUP\tRCVD\tSENT\tPFX\tLAST_ERROR")
		for _, peer := range peers {
			state := defaultShowString(statusString(peer["state"]), "unknown")
			up := "-"
			if strings.EqualFold(state, "Established") {
				up = ageString(statusString(peer["lastEstablishedAt"]))
			}
			lastError := defaultShowString(statusString(peer["lastErrorReason"]), "-")
			if strings.EqualFold(state, "Established") {
				lastError = "-"
			}
			fmt.Fprintf(w, "%s\t%d\t%s\t%s\t%d\t%d\t%d\t%s\n",
				statusString(peer["address"]),
				statusInt(peer["asn"]),
				state,
				up,
				statusInt(peer["messagesReceived"]),
				statusInt(peer["messagesSent"]),
				statusInt(peer["prefixesReceived"]),
				lastError,
			)
		}
	}
	return w.Flush()
}

func withLiveBGPState(router *api.Router, resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	if router == nil || !hasBGPResources(router) {
		return resources
	}
	live := liveBGPStatuses(router)
	if len(live) == 0 {
		return resources
	}
	out := append([]routerstate.ObjectStatus(nil), resources...)
	seen := map[string]bool{}
	for i := range out {
		if out[i].APIVersion != api.NetAPIVersion || out[i].Kind != "BGPRouter" {
			continue
		}
		if status := live[out[i].Name]; status != nil {
			out[i].Status = mergeBGPStatus(out[i].Status, status)
			seen[out[i].Name] = true
		}
	}
	for name, status := range live {
		if seen[name] {
			continue
		}
		out = append(out, routerstate.ObjectStatus{APIVersion: api.NetAPIVersion, Kind: "BGPRouter", Name: name, Status: status})
	}
	return out
}

func liveBGPStatuses(router *api.Router) map[string]map[string]any {
	statuses := map[string]map[string]any{}
	vrfs := routerctlBGPVRFNames(router)
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err != nil {
			continue
		}
		vrfName := vrfs[routerctlBGPVRFRefName(spec.VRF)]
		summaryCmd, routesCmd := routerctlBGPShowCommands(vrfName)
		summary, err := runDataplaneCommand("vtysh", "-c", summaryCmd)
		if err != nil {
			continue
		}
		routes, err := runDataplaneCommand("vtysh", "-c", routesCmd)
		if err != nil {
			routes = nil
		}
		state, err := bgpstate.ParseFRRState(summary, routes)
		if err != nil {
			continue
		}
		if routerctlBGPRouterUsesIPv6(router, resource.Metadata.Name, spec) {
			if routesV6, err := runDataplaneCommand("vtysh", "-c", routerctlBGPShowIPv6RoutesCommand(vrfName)); err == nil {
				if prefixesV6, err := bgpstate.ParseFRRRoutesJSON(routesV6); err == nil {
					state.Prefixes = append(state.Prefixes, prefixesV6...)
					state = bgpstate.Normalize(state)
				}
			}
		}
		peers := routerctlPeersForRouter(router, resource.Metadata.Name, state)
		established := 0
		for _, peer := range peers {
			if peer.Established {
				established++
			}
		}
		phase := "Pending"
		if len(peers) > 0 && established == len(peers) {
			phase = "Established"
		} else if established > 0 {
			phase = "Degraded"
		} else if len(peers) > 0 {
			phase = "Down"
		}
		statuses[resource.Metadata.Name] = map[string]any{
			"phase":            phase,
			"backend":          "frr",
			"peers":            bgpPeersStatusMaps(peers),
			"prefixes":         bgpPrefixesStatusMaps(state.Prefixes),
			"establishedPeers": established,
			"acceptedPrefixes": len(state.Prefixes),
			"observedAt":       time.Now().UTC().Format(time.RFC3339Nano),
			"source":           "live-vtysh",
		}
	}
	return statuses
}

func mergeBGPStatus(current, live map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range current {
		out[key] = value
	}
	livePeers := statusMaps(live["peers"])
	currentPeers := statusMaps(current["peers"])
	if len(livePeers) > 0 || len(currentPeers) == 0 || statusInt(current["establishedPeers"]) == 0 {
		for key, value := range live {
			out[key] = value
		}
	}
	return out
}

func bgpPeersStatusMaps(peers []bgpstate.Peer) []map[string]any {
	out := make([]map[string]any, 0, len(peers))
	for _, peer := range peers {
		out = append(out, map[string]any{
			"address":           peer.Address,
			"asn":               peer.ASN,
			"state":             peer.State,
			"established":       peer.Established,
			"prefixesReceived":  peer.PrefixesReceived,
			"messagesReceived":  peer.MessagesReceived,
			"messagesSent":      peer.MessagesSent,
			"lastEstablishedAt": peer.LastEstablishedAt,
			"lastErrorAt":       peer.LastErrorAt,
			"lastErrorReason":   peer.LastErrorReason,
		})
	}
	return out
}

func bgpPrefixesStatusMaps(prefixes []bgpstate.Prefix) []map[string]any {
	out := make([]map[string]any, 0, len(prefixes))
	for _, prefix := range prefixes {
		out = append(out, map[string]any{
			"prefix":      prefix.Prefix,
			"best":        prefix.Best,
			"valid":       prefix.Valid,
			"communities": prefix.Communities,
		})
	}
	return out
}

func hasBGPResources(router *api.Router) bool {
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion == api.NetAPIVersion && (resource.Kind == "BGPRouter" || resource.Kind == "BGPPeer") {
			return true
		}
	}
	return false
}

func writeVRRPShowTable(stdout io.Writer, router *api.Router, resources []routerstate.ObjectStatus) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	specs := virtualAddressShowSpecs(router)
	fmt.Fprintln(w, "VIP\tHOSTNAME\tROLE\tPRIORITY\tBASE\tIFACE\tVRID\tPEERS\tLAST_TRANSITION")
	for _, resource := range resources {
		if resource.Kind != "VirtualAddress" {
			continue
		}
		spec := specs[resource.Name]
		if defaultShowString(spec.Mode, "static") != "vrrp" && statusString(resource.Status["virtualRouterID"]) == "" {
			continue
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%d\t%s\t%s\n",
			statusString(resource.Status["address"]),
			defaultShowString(statusString(resource.Status["hostname"]), "-"),
			defaultShowString(statusString(resource.Status["role"]), "unknown"),
			statusInt(resource.Status["priority"]),
			statusInt(resource.Status["basePriority"]),
			defaultShowString(statusString(resource.Status["interface"]), spec.Interface),
			statusInt(resource.Status["virtualRouterID"]),
			strings.Join(spec.Peers, ","),
			ageString(statusString(resource.Status["lastRoleTransitionAt"])),
		)
		tracks := statusMaps(resource.Status["track"])
		if len(tracks) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "TRACK\tSTATE\tPENALTY\tDETAIL")
			for _, track := range tracks {
				fmt.Fprintf(w, "%s\t%s\t%d\t%s/%s unhealthy=%d\n",
					statusString(track["resource"]),
					statusString(track["state"]),
					statusInt(track["penalty"]),
					statusString(track["unhealthyCount"]),
					statusString(track["confirmConsecutiveUnhealthy"]),
					statusInt(track["unhealthyConsecutive"]),
				)
			}
		}
	}
	return w.Flush()
}

func writeIngressShowTable(stdout io.Writer, router *api.Router, resources []routerstate.ObjectStatus, verbose bool) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	specs := ingressServiceSpecs(router)
	var dataplane []ingressDataplaneRow
	fmt.Fprintln(w, "SERVICE\tHOSTNAME\tLISTEN\tACTIVE_BACKEND\tSELECTION\tHEALTHY/TOTAL")
	for _, resource := range resources {
		if resource.Kind != "IngressService" {
			continue
		}
		spec := specs[resource.Name]
		active := statusMap(resource.Status["activeBackend"])
		if verbose {
			dataplane = append(dataplane, observeIngressDataplane(resource.Name, spec, resource.Status))
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d/%d\n",
			resource.Name,
			defaultShowString(statusString(resource.Status["hostname"]), "-"),
			ingressListenString(spec, resource.Status),
			activeBackendString(active),
			defaultShowString(statusString(resource.Status["selection"]), "failover"),
			statusInt(resource.Status["healthyBackends"]),
			statusInt(resource.Status["totalBackends"]),
		)
		backends := statusMaps(resource.Status["backends"])
		if len(backends) > 0 {
			fmt.Fprintln(w)
			fmt.Fprintln(w, "BACKEND\tADDRESS\tSTATE\tDRAINED_UNTIL\tLAST_HEALTHY\tLAST_UNHEALTHY")
			for _, backend := range backends {
				state := "Unhealthy"
				if statusBool(backend["healthy"]) {
					state = "Healthy"
				}
				if statusBool(backend["drained"]) {
					state = "Drained"
				}
				fmt.Fprintf(w, "%s\t%s\t%s(%d/%d)\t%s\t%s\t%s\n",
					statusString(backend["name"]),
					backendAddressString(backend),
					state,
					statusInt(backend["healthyCount"]),
					statusInt(backend["unhealthyCount"]),
					defaultShowString(statusString(backend["drainedUntil"]), "-"),
					ageString(statusString(backend["lastHealthyAt"])),
					ageString(statusString(backend["lastUnhealthyAt"])),
				)
			}
		}
	}
	if verbose && len(dataplane) > 0 {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "DATAPLANE\tIPV4_FORWARD\tIPV6_FORWARD\tNFT_DNAT\tNFT_SNAT\tCONNTRACK\tDETAIL")
		for _, row := range dataplane {
			fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\t%s\n",
				row.Service,
				row.IPv4Forward,
				row.IPv6Forward,
				row.NFTDNAT,
				row.NFTSNAT,
				row.Conntrack,
				defaultShowString(row.Detail, "-"),
			)
		}
	}
	return w.Flush()
}

type ingressDataplaneRow struct {
	Service     string
	IPv4Forward string
	IPv6Forward string
	NFTDNAT     int
	NFTSNAT     int
	Conntrack   string
	Detail      string
}

func observeIngressDataplane(name string, spec api.IngressServiceSpec, status map[string]any) ingressDataplaneRow {
	row := ingressDataplaneRow{
		Service:     name,
		IPv4Forward: dataplaneSysctlValue("net.ipv4.ip_forward"),
		IPv6Forward: dataplaneSysctlValue("net.ipv6.conf.all.forwarding"),
	}
	nftDNAT, nftSNAT, nftDetail := ingressNFTRuleCounts(name)
	row.NFTDNAT = nftDNAT
	row.NFTSNAT = nftSNAT
	if nftDetail != "" {
		row.appendDetail(nftDetail)
	}
	if hairpinDetail := ingressHairpinDataplaneDetail(spec, status, nftSNAT); hairpinDetail != "" {
		row.appendDetail(hairpinDetail)
	}
	count, detail := ingressConntrackCount(spec, status)
	row.Conntrack = count
	if detail != "" {
		row.appendDetail(detail)
	}
	return row
}

func (r *ingressDataplaneRow) appendDetail(detail string) {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return
	}
	if r.Detail != "" {
		r.Detail += "; "
	}
	r.Detail += detail
}

func dataplaneSysctlValue(key string) string {
	out, err := runDataplaneCommand("sysctl", "-n", key)
	if err != nil {
		return "unavailable"
	}
	value := strings.TrimSpace(string(out))
	if value == "" {
		return "unknown"
	}
	return value
}

func ingressNFTRuleCounts(name string) (int, int, string) {
	out, err := runDataplaneCommand("nft", "-a", "list", "table", "ip", "routerd_nat")
	if err != nil {
		return 0, 0, "nft unavailable"
	}
	needle := "routerd IngressService " + name
	var dnat, snat int
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, needle) {
			continue
		}
		if strings.Contains(line, "dnat to") || strings.Contains(line, " vmap ") {
			dnat++
		}
		if strings.Contains(line, " masquerade") || strings.Contains(line, " snat ") {
			snat++
		}
	}
	return dnat, snat, ""
}

func ingressHairpinDataplaneDetail(spec api.IngressServiceSpec, status map[string]any, nftSNAT int) string {
	mode := strings.TrimSpace(spec.Hairpin.Mode)
	if mode == "" {
		mode = "auto"
	}
	required := false
	switch mode {
	case "off":
		required = false
	case "manual":
		required = spec.Hairpin.Enabled || len(spec.Hairpin.Interfaces) > 0
	case "auto":
		required = ingressShowAutoHairpinRequired(spec, status)
	default:
		return "hairpinMode=" + mode
	}
	state := "nft_snat=not-required"
	if required && nftSNAT == 0 {
		state = "nft_snat=missing"
	} else if required {
		state = "nft_snat=present"
	} else if nftSNAT > 0 {
		state = "nft_snat=present"
	}
	return fmt.Sprintf("hairpinMode=%s hairpinRequired=%t %s", mode, required, state)
}

func ingressShowAutoHairpinRequired(spec api.IngressServiceSpec, status map[string]any) bool {
	listen := statusString(status["listenAddress"])
	if listen == "" {
		listen = spec.Listen.Address
	}
	listenAddr, err := netip.ParseAddr(strings.TrimSpace(listen))
	if err != nil || !listenAddr.Is4() {
		return false
	}
	for _, backend := range ingressShowBackendAddresses(spec, status) {
		addr, err := netip.ParseAddr(backend)
		if err == nil && addr.Is4() && ingressShowSamePrivateIPv4Slash24(listenAddr, addr) {
			return true
		}
	}
	return false
}

func ingressShowBackendAddresses(spec api.IngressServiceSpec, status map[string]any) []string {
	seen := map[string]bool{}
	var out []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		out = append(out, value)
	}
	for _, backend := range statusMaps(status["backends"]) {
		add(statusString(backend["resolvedAddress"]))
		if address := statusString(backend["address"]); net.ParseIP(address) != nil {
			add(address)
		}
	}
	if active := statusMap(status["activeBackend"]); len(active) > 0 {
		add(statusString(active["resolvedAddress"]))
		if address := statusString(active["address"]); net.ParseIP(address) != nil {
			add(address)
		}
	}
	for _, backend := range spec.Backends {
		if net.ParseIP(backend.Address) != nil {
			add(backend.Address)
		}
	}
	return out
}

func ingressShowSamePrivateIPv4Slash24(a, b netip.Addr) bool {
	if !a.Is4() || !b.Is4() || !a.IsPrivate() || !b.IsPrivate() {
		return false
	}
	return netip.PrefixFrom(a, 24).Contains(b)
}

func ingressConntrackCount(spec api.IngressServiceSpec, status map[string]any) (string, string) {
	out, err := runDataplaneCommand("conntrack", "-L")
	if err != nil {
		return "unavailable", "conntrack unavailable"
	}
	needles := ingressConntrackNeedles(spec, status)
	if len(needles) == 0 {
		return "unknown", "conntrack no match keys"
	}
	var count int
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				count++
				break
			}
		}
	}
	return strconv.Itoa(count), ""
}

func ingressConntrackNeedles(spec api.IngressServiceSpec, status map[string]any) []string {
	seen := map[string]bool{}
	var needles []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		needles = append(needles, value)
	}
	if listen := statusString(status["listenAddress"]); listen != "" {
		add("dst=" + listen)
		add("reply_src=" + listen)
	} else if spec.Listen.Address != "" {
		add("dst=" + spec.Listen.Address)
		add("reply_src=" + spec.Listen.Address)
	}
	for _, backend := range statusMaps(status["backends"]) {
		if resolved := statusString(backend["resolvedAddress"]); resolved != "" {
			add("dst=" + resolved)
			add("src=" + resolved)
			continue
		}
		if address := statusString(backend["address"]); net.ParseIP(address) != nil {
			add("dst=" + address)
			add("src=" + address)
		}
	}
	for _, backend := range spec.Backends {
		if net.ParseIP(backend.Address) != nil {
			add("dst=" + backend.Address)
			add("src=" + backend.Address)
		}
	}
	return needles
}

func runDataplaneCommand(name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, hostcmd.Resolve(name), args...).CombinedOutput()
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
		"if":                     "Interface",
		"iface":                  "Interface",
		"interface":              "Interface",
		"interfaces":             "Interface",
		"br":                     "Bridge",
		"bridge":                 "Bridge",
		"bridges":                "Bridge",
		"vxlan":                  "VXLANSegment",
		"vxlans":                 "VXLANSegment",
		"vxlansegment":           "VXLANSegment",
		"wireguard":              "WireGuardInterface",
		"wg":                     "WireGuardInterface",
		"wireguardinterface":     "WireGuardInterface",
		"wireguardpeer":          "WireGuardPeer",
		"wgpeer":                 "WireGuardPeer",
		"tailscale":              "TailscaleNode",
		"tailscalenode":          "TailscaleNode",
		"ts":                     "TailscaleNode",
		"ipsec":                  "IPsecConnection",
		"ipsecconnection":        "IPsecConnection",
		"vrf":                    "VRF",
		"vxlantunnel":            "VXLANTunnel",
		"pd":                     "DHCPv6PrefixDelegation",
		"dhcpv6pd":               "DHCPv6PrefixDelegation",
		"prefixdelegation":       "DHCPv6PrefixDelegation",
		"dhcpv6prefixdelegation": "DHCPv6PrefixDelegation",
		"ipv4static":             "IPv4StaticAddress",
		"ipv4staticaddress":      "IPv4StaticAddress",
		"vip":                    "VirtualAddress",
		"vips":                   "VirtualAddress",
		"virtualip":              "VirtualAddress",
		"virtualipv4":            "VirtualAddress",
		"virtualipv4address":     "VirtualAddress",
		"virtualipv6":            "VirtualAddress",
		"virtualipv6address":     "VirtualAddress",
		"vrrp":                   "VirtualAddress",
		"bgp":                    "BGPRouter",
		"bgprouter":              "BGPRouter",
		"bgppeer":                "BGPPeer",
		"bgppeers":               "BGPPeer",
		"ingress":                "IngressService",
		"ingressservice":         "IngressService",
		"ingressservices":        "IngressService",
		"dhcpv4client":           "DHCPv4Client",
		"dhcpv4server":           "DHCPv4Server",
		"dhcpv4reservation":      "DHCPv4Reservation",
		"dhcpv4relay":            "DHCPv4Relay",
		"dhcprelay":              "DHCPv4Relay",
		"dhcpv6address":          "DHCPv6Address",
		"dhcpv6server":           "DHCPv6Server",
		"dhcpv6information":      "DHCPv6Information",
		"ipv6ra":                 "IPv6RAAddress",
		"ipv4staticroute":        "IPv4StaticRoute",
		"clusternetworkroute":    "ClusterNetworkRoute",
		"k8sroutes":              "ClusterNetworkRoute",
		"ipv6route":              "IPv6StaticRoute",
		"ipv6staticroute":        "IPv6StaticRoute",
		"ipv6raaddress":          "IPv6RAAddress",
		"slaac":                  "IPv6RAAddress",
		"nat":                    "NAT44Rule",
		"snat":                   "NAT44Rule",
		"ipv4nat":                "NAT44Rule",
		"nat44":                  "NAT44Rule",
		"nat44rule":              "NAT44Rule",
		"portforward":            "PortForward",
		"portforwards":           "PortForward",
		"portnat":                "PortForward",
		"addressset":             "IPAddressSet",
		"ipset":                  "IPAddressSet",
		"localserviceredirect":   "LocalServiceRedirect",
		"serviceredirect":        "LocalServiceRedirect",
		"dslite":                 "DSLiteTunnel",
		"dslitetunnel":           "DSLiteTunnel",
		"dnszone":                "DNSZone",
		"dnsresolver":            "DNSResolver",
		"dns":                    "DNSResolver",
		"pppoe":                  "PPPoESession",
		"pppoesession":           "PPPoESession",
		"pppoeclient":            "PPPoESession",
		"fw":                     "FirewallRule",
		"firewall":               "FirewallPolicy",
		"firewallzone":           "FirewallZone",
		"firewallpolicy":         "FirewallPolicy",
		"firewallrule":           "FirewallRule",
		"zone":                   "FirewallZone",
		"zones":                  "FirewallZone",
		"hostname":               "Hostname",
		"host":                   "Hostname",
		"observabilitypipeline":  "ObservabilityPipeline",
		"obspipeline":            "ObservabilityPipeline",
		"routerdcluster":         "RouterdCluster",
		"cluster":                "RouterdCluster",
		"kernelmodule":           "KernelModule",
		"kernelmodules":          "KernelModule",
		"kmod":                   "KernelModule",
		"inventory":              "Inventory",
		"inv":                    "Inventory",
		"orphan":                 "Orphan",
		"orphans":                "Orphan",
		"route":                  "EgressRoutePolicy",
		"routeset":               "EgressRoutePolicy",
		"ipv4route":              "EgressRoutePolicy",
		"ipv4policyrouteset":     "EgressRoutePolicy",
	}
	if canonical, ok := aliases[key]; ok {
		return canonical
	}
	if kind == "" {
		return ""
	}
	return kind
}

func bgpRouterSpecs(router *api.Router) map[string]api.BGPRouterSpec {
	out := map[string]api.BGPRouterSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "BGPRouter" {
			continue
		}
		spec, err := resource.BGPRouterSpec()
		if err == nil {
			out[resource.Metadata.Name] = spec
		}
	}
	return out
}

type virtualAddressShowSpec struct {
	Interface string
	Address   string
	Family    string
	Mode      string
	Peers     []string
}

func virtualAddressShowSpecs(router *api.Router) map[string]virtualAddressShowSpec {
	out := map[string]virtualAddressShowSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "VirtualAddress":
			spec, err := resource.VirtualAddressSpec()
			if err == nil {
				out[resource.Metadata.Name] = virtualAddressShowSpec{Interface: spec.Interface, Address: spec.Address, Family: spec.Family, Mode: spec.Mode, Peers: spec.VRRP.Peers}
			}
		}
	}
	return out
}

func routerctlPeersForRouter(router *api.Router, routerName string, state bgpstate.State) []bgpstate.Peer {
	byAddress := map[string]bgpstate.Peer{}
	for _, peer := range state.Peers {
		byAddress[peer.Address] = peer
	}
	var out []bgpstate.Peer
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		spec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(spec.RouterRef), "/")
		if !ok || name != routerName {
			continue
		}
		for _, address := range spec.Peers {
			peer, ok := byAddress[strings.TrimSpace(address)]
			if !ok {
				peer = bgpstate.Peer{Address: strings.TrimSpace(address), ASN: spec.PeerASN, State: "Missing"}
			} else if peer.ASN == 0 {
				peer.ASN = spec.PeerASN
			}
			out = append(out, peer)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })
	return out
}

func routerctlBGPVRFNames(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "VRF" {
			continue
		}
		spec, err := resource.VRFSpec()
		if err != nil {
			continue
		}
		out[resource.Metadata.Name] = defaultString(spec.IfName, resource.Metadata.Name)
	}
	return out
}

func routerctlBGPVRFRefName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if kind, name, ok := strings.Cut(value, "/"); ok && kind == "VRF" {
		return strings.TrimSpace(name)
	}
	return value
}

func routerctlBGPRouterUsesIPv6(router *api.Router, routerName string, spec api.BGPRouterSpec) bool {
	prefixes := append([]string{}, spec.ImportPolicy.AllowedPrefixes...)
	prefixes = append(prefixes, spec.ExportPolicy.AllowedPrefixes...)
	prefixes = append(prefixes, spec.Redistribute.Connected.AllowedPrefixes...)
	prefixes = append(prefixes, spec.Redistribute.Static.AllowedPrefixes...)
	for _, prefix := range prefixes {
		if parsed, err := netip.ParsePrefix(strings.TrimSpace(prefix)); err == nil && parsed.Addr().Is6() {
			return true
		}
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "BGPPeer" {
			continue
		}
		peerSpec, err := resource.BGPPeerSpec()
		if err != nil {
			continue
		}
		_, name, ok := strings.Cut(strings.TrimSpace(peerSpec.RouterRef), "/")
		if !ok || name != routerName {
			continue
		}
		for _, address := range peerSpec.Peers {
			if parsed, err := netip.ParseAddr(strings.TrimSpace(address)); err == nil && parsed.Is6() {
				return true
			}
		}
	}
	return false
}

func routerctlBGPShowCommands(vrfName string) (string, string) {
	if strings.TrimSpace(vrfName) == "" {
		return "show bgp summary json", "show bgp ipv4 unicast json"
	}
	vrfName = strings.TrimSpace(vrfName)
	return "show bgp vrf " + vrfName + " summary json", "show bgp vrf " + vrfName + " ipv4 unicast json"
}

func routerctlBGPShowIPv6RoutesCommand(vrfName string) string {
	if strings.TrimSpace(vrfName) == "" {
		return "show bgp ipv6 unicast json"
	}
	return "show bgp vrf " + strings.TrimSpace(vrfName) + " ipv6 unicast json"
}

func withLiveVRRPRoles(router *api.Router, resources []routerstate.ObjectStatus) []routerstate.ObjectStatus {
	if router == nil {
		return resources
	}
	specs := virtualAddressShowSpecs(router)
	aliases := interfaceAliases(router.Spec.Resources)
	out := make([]routerstate.ObjectStatus, len(resources))
	copy(out, resources)
	for i := range out {
		if out[i].Kind != "VirtualAddress" {
			continue
		}
		spec := specs[out[i].Name]
		if defaultShowString(spec.Mode, "static") != "vrrp" {
			continue
		}
		role, ok := liveVRRPRole(out[i].Status, spec, aliases)
		if !ok {
			continue
		}
		status := map[string]any{}
		for key, value := range out[i].Status {
			status[key] = value
		}
		if previous := statusString(status["role"]); previous != role {
			status["role"] = role
			status["lastRoleTransitionAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		}
		out[i].Status = status
	}
	return out
}

func liveVRRPRole(status map[string]any, spec virtualAddressShowSpec, aliases map[string]string) (string, bool) {
	ifname := statusString(status["ifname"])
	if ifname == "" {
		ifname = aliases[spec.Interface]
	}
	address := statusString(status["address"])
	if address == "" {
		address = spec.Address
	}
	if strings.TrimSpace(ifname) == "" || strings.TrimSpace(address) == "" {
		return "", false
	}
	family := spec.Family
	if family == "" {
		family = "ipv4"
		if strings.Contains(address, ":") {
			family = "ipv6"
		}
	}
	ipFamily := "-4"
	if family == "ipv6" {
		ipFamily = "-6"
	}
	out, err := exec.Command("ip", ipFamily, "addr", "show", "dev", ifname).CombinedOutput()
	if err != nil {
		return "", false
	}
	if ipOutputHasAddress(string(out), address, family) {
		return "master", true
	}
	return "backup", true
}

func ipOutputHasAddress(output, address, family string) bool {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
	if err != nil {
		addr, addrErr := netip.ParseAddr(strings.TrimSpace(address))
		if addrErr != nil {
			return false
		}
		bits := 32
		if family == "ipv6" {
			bits = 128
		}
		prefix = netip.PrefixFrom(addr, bits)
	}
	token := "inet "
	if family == "ipv6" {
		token = "inet6 "
	}
	needle := token + prefix.Addr().String() + "/" + strconv.Itoa(prefix.Bits())
	return strings.Contains(output, needle)
}

func ingressServiceSpecs(router *api.Router) map[string]api.IngressServiceSpec {
	out := map[string]api.IngressServiceSpec{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		if resource.Kind != "IngressService" {
			continue
		}
		spec, err := resource.IngressServiceSpec()
		if err == nil {
			out[resource.Metadata.Name] = spec
		}
	}
	return out
}

func establishedSummary(status map[string]any, total int) string {
	if total == 0 {
		return "0/0"
	}
	return fmt.Sprintf("%d/%d", statusInt(status["establishedPeers"]), total)
}

func enabledString(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func ingressListenString(spec api.IngressServiceSpec, status map[string]any) string {
	address := statusString(status["listenAddress"])
	if address == "" {
		address = spec.Listen.Address
	}
	if address == "" && spec.Listen.AddressFrom.Resource != "" {
		address = spec.Listen.AddressFrom.Resource
	}
	return fmt.Sprintf("%s:%s:%d", spec.Listen.Interface, defaultShowString(address, "*"), spec.Listen.Port)
}

func activeBackendString(active map[string]any) string {
	name := statusString(active["name"])
	address := statusString(active["address"])
	port := statusInt(active["port"])
	if name == "" && address == "" {
		return "-"
	}
	if port > 0 {
		return fmt.Sprintf("%s/%s:%d", defaultShowString(name, "-"), address, port)
	}
	return fmt.Sprintf("%s/%s", defaultShowString(name, "-"), address)
}

func backendAddressString(backend map[string]any) string {
	address := statusString(backend["address"])
	resolved := statusString(backend["resolvedAddress"])
	port := statusInt(backend["port"])
	if resolved != "" && resolved != address {
		return fmt.Sprintf("%s -> %s:%d", address, resolved, port)
	}
	if port > 0 {
		return fmt.Sprintf("%s:%d", defaultShowString(resolved, address), port)
	}
	return defaultShowString(resolved, address)
}

func ageString(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	if ts, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return humanDuration(time.Since(ts))
	}
	if ts, err := strconv.ParseInt(value, 10, 64); err == nil && ts > 0 {
		return humanDuration(time.Since(time.Unix(ts, 0)))
	}
	return value
}

func humanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd%dh", int(d/(24*time.Hour)), int(d%(24*time.Hour)/time.Hour))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh%dm", int(d/time.Hour), int(d%time.Hour/time.Minute))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm%ds", int(d/time.Minute), int(d%time.Minute/time.Second))
	}
	return fmt.Sprintf("%ds", int(d/time.Second))
}

func statusMaps(value any) []map[string]any {
	switch typed := value.(type) {
	case []map[string]any:
		return typed
	case []any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	default:
		return nil
	}
}

func statusMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func statusString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func statusInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case uint:
		return int(typed)
	case uint32:
		return int(typed)
	case uint64:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, _ := strconv.Atoi(strings.TrimSpace(typed))
		return parsed
	default:
		return 0
	}
}

func statusBool(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func defaultShowString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) != "" {
		return value
	}
	return fallback
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
	if connections, ok := observed["connections"]; ok {
		if table, ok := connections.(*observe.ConnectionTable); ok {
			return fmt.Sprintf("conntrack=%d", table.Count)
		}
	}
	if err, ok := observed["connectionsError"]; ok {
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

func defaultStatusSocketPath() string {
	return platformDefaults.StatusSocketFile()
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  status [--socket <path>] [--json|-o json|yaml]")
	fmt.Fprintln(w, "  events [--state-file <path>] [--topic <topic>] [--resource <kind>/<name>] [--limit <n>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  dns-queries [--socket <path>] [--db <path>] [--since 1h] [--client <ip>] [--qname <pattern>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  connections [--socket <path>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  traffic-flows [--socket <path>] [--db <path>] [--since 1h] [--client <ip>] [--peer <ip>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  firewall-logs [--socket <path>] [--db <path>] [--since 1h] [--action drop] [--src <ip>] [--limit 100] [-o table|json|yaml]")
	fmt.Fprintln(w, "  get <kind>[/<name>] [--list-kinds] [--config <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  describe <kind>/<name> [--config <path>] [--state-file <path>] [--ledger-file <path>] [--events-limit <n>]")
	fmt.Fprintln(w, "  describe firewall [--config <path>]")
	fmt.Fprintln(w, "  firewall test from=<zone> to=<zone|self> proto=<tcp|udp> dport=<port> [--config <path>]")
	fmt.Fprintln(w, "  wireguard list [-o table|json|yaml]")
	fmt.Fprintln(w, "  wireguard show <interface> [-o table|json|yaml]")
	fmt.Fprintln(w, "  tailscale peers [-o table|json|yaml] [--binary tailscale]")
	fmt.Fprintln(w, "  diagnose egress [policy] [--config <path>] [--state-file <path>] [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  diagnose dns [resolver] [--server <addr>] [--name <fqdn>] [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  diagnose lan-client <ip> [--no-host] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show bgp|vrrp|ingress [--config <path>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <kind> [--config <path>] [--state-file <path>] [--ledger-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  show <kind>/<name> [--diff|--ledger|--adopt|--events|--spec|--status] [-o table|json|yaml]")
	fmt.Fprintln(w, "  drain ingress/<service> backend=<name> [--duration 10m] [--state-file <path>]")
	fmt.Fprintln(w, "  undrain ingress/<service> backend=<name> [--state-file <path>]")
	fmt.Fprintln(w, "  plan [--socket <path>]")
	fmt.Fprintln(w, "  apply [--socket <path>] [--dry-run]")
	fmt.Fprintln(w, "  delete <kind>/<name> [--socket <path>] [--dry-run]")
}
