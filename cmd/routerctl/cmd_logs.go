// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/logstore"
	"github.com/imksoo/routerd/pkg/observe"
)

func emitDNSRows(stdout io.Writer, output string, rows []logstore.DNSQuery) error {
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

// fetchDNSRowsChunked fetches at most filter.Limit rows in successive chunks of
// chunkSize each, narrowing Until on each iteration so each chunk has its own
// ctx-bounded portion of work. If the caller does not impose a Limit, it stops
// once a chunk returns fewer than chunkSize rows.
func fetchDNSRowsChunked(ctx context.Context, store *logstore.DNSQueryLog, filter logstore.DNSQueryFilter, chunkSize int) ([]logstore.DNSQuery, error) {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	target := filter.Limit
	if target <= 0 {
		target = chunkSize
	}
	var out []logstore.DNSQuery
	chunkFilter := filter
	chunkFilter.Limit = chunkSize
	for len(out) < target {
		if ctx.Err() != nil {
			return out, fmt.Errorf("context deadline reached after %d rows (hint: narrow --from / --to or increase --timeout): %w", len(out), ctx.Err())
		}
		rows, err := store.List(ctx, chunkFilter)
		if err != nil {
			return out, err
		}
		if len(rows) == 0 {
			break
		}
		out = append(out, rows...)
		if len(rows) < chunkSize {
			break
		}
		// Advance Until to one-nanosecond-before the oldest row in this batch.
		oldest := rows[len(rows)-1].Timestamp
		if oldest.IsZero() {
			break
		}
		next := oldest.Add(-time.Nanosecond)
		if !chunkFilter.Since.IsZero() && !next.After(chunkFilter.Since) {
			break
		}
		chunkFilter.Until = next
	}
	if len(out) > target {
		out = out[:target]
	}
	return out, nil
}

// writeAggregateGroup renders a key->count map sorted by count desc.
func writeAggregateGroup(w io.Writer, title string, counts map[string]int, topN int) {
	if len(counts) == 0 {
		return
	}
	type kv struct {
		k string
		v int
	}
	pairs := make([]kv, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, kv{k, v})
	}
	sortPairs := func(p []kv) {
		for i := 1; i < len(p); i++ {
			for j := i; j > 0 && (p[j].v > p[j-1].v || (p[j].v == p[j-1].v && p[j].k < p[j-1].k)); j-- {
				p[j], p[j-1] = p[j-1], p[j]
			}
		}
	}
	sortPairs(pairs)
	fmt.Fprintln(w, title)
	max := topN
	if max <= 0 || max > len(pairs) {
		max = len(pairs)
	}
	for _, p := range pairs[:max] {
		key := p.k
		if key == "" {
			key = "(empty)"
		}
		fmt.Fprintf(w, "  %s\t%d\n", key, p.v)
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

func writeConnectionsTable(stdout io.Writer, table observe.ConnectionTable) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "COUNT\t%d", table.Count)
	if table.Max > 0 {
		fmt.Fprintf(w, "/%d", table.Max)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "FAMILY\tPROTO\tSTATE\tFLOW\tRETURN\tNAT\tTIMEOUT")
	for _, row := range table.Entries {
		state := stringutil.FirstNonEmpty(row.State, assuredState(row.Assured))
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

func emitTrafficRows(stdout io.Writer, output string, rows []logstore.TrafficFlow) error {
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
			displayCell(stringutil.FirstNonEmpty(row.TLSSNI, row.ResolvedHostname)),
		)
	}
	return w.Flush()
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

func emitFirewallRows(stdout io.Writer, output string, rows []logstore.FirewallLogEntry) error {
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
