// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

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
