// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
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
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"DNS 問い合わせ履歴 (DNSResolver の query log) を表示する。\n"+
				"--since には Go の duration 形式 (例: 1h, 30m, 24h)。\n"+
				"--from / --to は絶対時刻 (RFC3339 形式 例 2026-05-27T20:00:00+09:00、\n"+
				"または 2026-05-27T20:00:00 / 2026-05-27 のような短縮形。短縮形は UTC 解釈)。\n"+
				"--agg を付けると集計のみ出力する。\n"+
				"--chunk-size を指定すると分割取得 (deadline 単位を縮める) になる。",
			"routerctl dns-queries --since 1h --limit 500 -o json\n"+
				"routerctl dns-queries --from 2026-05-27T00:00:00+09:00 --to 2026-05-27T06:00:00+09:00\n"+
				"routerctl dns-queries --rcode NXDOMAIN --upstream 9.9.9.9 --duration-min 100ms\n"+
				"routerctl dns-queries --qname-suffix example.com --agg\n"+
				"routerctl dns-queries --db /var/log/routerd/dns-queries.db --since 24h --chunk-size 1000")
	}
	dbPath := fs.String("db", "", "read a DNS query log database file directly instead of using routerd")
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout (Issue #36 raised default 5s->30s)")
	since := fs.String("since", "1h", "show queries newer than duration, for example 1h or 30m")
	fromStr := fs.String("from", "", "absolute lower bound (RFC3339 or yyyy-mm-ddThh:mm:ss [tz]); overrides --since")
	toStr := fs.String("to", "", "absolute upper bound (RFC3339 or yyyy-mm-ddThh:mm:ss [tz])")
	client := fs.String("client", "", "client IP address")
	qname := fs.String("qname", "", "question name LIKE pattern")
	qnameSuffix := fs.String("qname-suffix", "", "match qname as suffix (e.g. example.com matches www.example.com)")
	rcode := fs.String("rcode", "", "exact response code filter (NOERROR / NXDOMAIN / SERVFAIL ...)")
	upstream := fs.String("upstream", "", "exact upstream filter")
	durationMin := fs.String("duration-min", "", "minimum duration (Go duration, e.g. 100ms)")
	agg := fs.Bool("agg", false, "show aggregate summary (p50/p95/p99 and by-X counts) instead of rows")
	fs.BoolVar(agg, "stats", false, "alias of --agg")
	chunkSize := fs.Int("chunk-size", 1000, "rows per chunk for direct-DB mode (--db); applies per-chunk deadline")
	limit := fs.Int("limit", 500, "maximum number of rows (Issue #36 raised default 100->500)")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	var durMinUS int64
	if dms := strings.TrimSpace(*durationMin); dms != "" {
		d, err := time.ParseDuration(dms)
		if err != nil {
			return fmt.Errorf("--duration-min: %w", err)
		}
		if d < 0 {
			return fmt.Errorf("--duration-min must be non-negative")
		}
		durMinUS = d.Microseconds()
	}

	if strings.TrimSpace(*dbPath) != "" {
		filter, err := dnsQueryFilterFromFlags(*since, *fromStr, *toStr, *client, *qname, *qnameSuffix, *rcode, *upstream, durMinUS, *limit)
		if err != nil {
			return err
		}
		store, err := logstore.OpenDNSQueryLogReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer store.Close()
		if *agg {
			a, err := store.Aggregate(context.Background(), filter)
			if err != nil {
				return err
			}
			return emitDNSAggregate(stdout, output, a)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		rows, err := fetchDNSRowsChunked(ctx, store, filter, *chunkSize)
		if err != nil {
			return err
		}
		return emitDNSRows(stdout, output, rows)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req := controlapi.DNSQueriesRequest{
		Since:         *since,
		From:          *fromStr,
		To:            *toStr,
		Client:        *client,
		QName:         *qname,
		QNameSuffix:   *qnameSuffix,
		ResponseCode:  *rcode,
		Upstream:      *upstream,
		DurationMinUS: durMinUS,
		Limit:         *limit,
	}
	if *agg {
		res, err := controlapi.NewUnixClient(*socketPath).DNSQueriesAggregate(ctx, req)
		if err != nil {
			return err
		}
		return emitDNSAggregate(stdout, output, res.Aggregate)
	}
	result, err := controlapi.NewUnixClient(*socketPath).DNSQueries(ctx, req)
	if err != nil {
		return err
	}
	return emitDNSRows(stdout, output, result.Items)
}

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

func emitDNSAggregate(stdout io.Writer, output string, agg logstore.DNSQueryAggregate) error {
	switch output {
	case "", "table":
		return writeDNSAggregateTable(stdout, agg)
	case "json":
		return writeJSON(stdout, agg)
	case "yaml":
		return writeYAML(stdout, agg)
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

func dnsQueryFilterFromFlags(since, fromStr, toStr, client, qname, qnameSuffix, rcode, upstream string, durationMinUS int64, limit int) (logstore.DNSQueryFilter, error) {
	sinceT, untilT, err := resolveSinceUntilFlags(since, fromStr, toStr)
	if err != nil {
		return logstore.DNSQueryFilter{}, err
	}
	return logstore.DNSQueryFilter{
		Since:         sinceT,
		Until:         untilT,
		Client:        client,
		QName:         qname,
		QNameSuffix:   qnameSuffix,
		ResponseCode:  rcode,
		Upstream:      upstream,
		DurationMinUS: durationMinUS,
		Limit:         limit,
	}, nil
}

func resolveSinceUntilFlags(since, fromStr, toStr string) (time.Time, time.Time, error) {
	var sinceT, untilT time.Time
	if v := strings.TrimSpace(fromStr); v != "" {
		t, err := parseAbsTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--from: %w", err)
		}
		sinceT = t
	} else if v := strings.TrimSpace(since); v != "" {
		t, err := cutoffTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, err
		}
		sinceT = t
	}
	if v := strings.TrimSpace(toStr); v != "" {
		t, err := parseAbsTime(v)
		if err != nil {
			return time.Time{}, time.Time{}, fmt.Errorf("--to: %w", err)
		}
		untilT = t
	}
	return sinceT, untilT, nil
}

func parseAbsTime(value string) (time.Time, error) {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05Z07:00", "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, value); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("could not parse %q (expected RFC3339 like 2026-05-27T20:00:00+09:00; bare layouts without timezone are UTC)", value)
}

func writeDNSAggregateTable(stdout io.Writer, agg logstore.DNSQueryAggregate) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SUMMARY")
	if !agg.Since.IsZero() {
		fmt.Fprintf(w, "  since\t%s\n", agg.Since.Format(time.RFC3339))
	}
	if !agg.Until.IsZero() {
		fmt.Fprintf(w, "  until\t%s\n", agg.Until.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "  total\t%d\n", agg.Total)
	fmt.Fprintf(w, "  p50\t%s\n", time.Duration(agg.DurationP50US)*time.Microsecond)
	fmt.Fprintf(w, "  p95\t%s\n", time.Duration(agg.DurationP95US)*time.Microsecond)
	fmt.Fprintf(w, "  p99\t%s\n", time.Duration(agg.DurationP99US)*time.Microsecond)
	writeAggregateGroup(w, "BY RESPONSE CODE", agg.ByResponseCode, 10)
	writeAggregateGroup(w, "BY CLIENT (top 10)", agg.ByClient, 10)
	writeAggregateGroup(w, "BY UPSTREAM (top 10)", agg.ByUpstream, 10)
	writeAggregateGroup(w, "BY QNAME SUFFIX (top 10)", agg.ByQNameSuffix, 10)
	return w.Flush()
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

func connectionsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("connections", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"アクティブな conntrack エントリ一覧を表示する。",
			"routerctl connections --limit 200\n"+
				"routerctl connections -o json")
	}
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 5*time.Second, "request timeout")
	limit := fs.Int("limit", 100, "maximum number of entries")
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
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"NAT44 / DPI flow log を表示する。\n"+
				"--since は Go の duration 形式 (例: 1h, 30m, 24h)。\n"+
				"--from / --to は絶対時刻 (RFC3339 形式 例 2026-05-27T20:00:00+09:00、\n"+
				"または 2026-05-27T20:00:00 / 2026-05-27 のような短縮形。短縮形は UTC 解釈)。\n"+
				"--agg を付けると集計のみ出力する。\n"+
				"--asymmetric は片方向通信のみ (rx==0 OR tx==0) を抽出する。",
			"routerctl traffic-flows --since 1h --client 192.168.1.10 -o json\n"+
				"routerctl traffic-flows --from 2026-05-27T00:00:00+09:00 --to 2026-05-27T06:00:00+09:00\n"+
				"routerctl traffic-flows --protocol tcp --peer-suffix amazonaws.com --agg\n"+
				"routerctl traffic-flows --db /var/log/routerd/traffic-flows.db --since 24h --chunk-size 1000")
	}
	dbPath := fs.String("db", "", "read a traffic flow log database file directly instead of using routerd")
	socketPath := fs.String("socket", defaultSocketPath(), "routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout (Issue #36 raised default 5s->30s)")
	since := fs.String("since", "1h", "show flows newer than duration, for example 1h or 30m")
	fromStr := fs.String("from", "", "absolute lower bound (RFC3339 or yyyy-mm-ddThh:mm:ss [tz]); overrides --since")
	toStr := fs.String("to", "", "absolute upper bound (RFC3339 or yyyy-mm-ddThh:mm:ss [tz])")
	client := fs.String("client", "", "client IP address")
	peer := fs.String("peer", "", "peer IP address")
	peerSuffix := fs.String("peer-suffix", "", "match peer address or resolved hostname as suffix")
	protocol := fs.String("protocol", "", "exact protocol filter (tcp / udp / ...)")
	asymmetric := fs.Bool("asymmetric", false, "only flows with rx==0 OR tx==0")
	agg := fs.Bool("agg", false, "show aggregate summary instead of rows")
	fs.BoolVar(agg, "stats", false, "alias of --agg")
	chunkSize := fs.Int("chunk-size", 1000, "rows per chunk for direct-DB mode (--db); applies per-chunk deadline")
	limit := fs.Int("limit", 500, "maximum number of rows (Issue #36 raised default 100->500)")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	if strings.TrimSpace(*dbPath) != "" {
		filter, err := trafficFlowFilterFromFlags(*since, *fromStr, *toStr, *client, *peer, *peerSuffix, *protocol, *asymmetric, *limit)
		if err != nil {
			return err
		}
		store, err := logstore.OpenTrafficFlowLogReadOnly(*dbPath)
		if err != nil {
			return err
		}
		defer store.Close()
		if *agg {
			a, err := store.Aggregate(context.Background(), filter)
			if err != nil {
				return err
			}
			return emitTrafficAggregate(stdout, output, a)
		}
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		rows, err := fetchTrafficRowsChunked(ctx, store, filter, *chunkSize)
		if err != nil {
			return err
		}
		return emitTrafficRows(stdout, output, rows)
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	req := controlapi.TrafficFlowsRequest{
		Since:      *since,
		From:       *fromStr,
		To:         *toStr,
		Client:     *client,
		Peer:       *peer,
		PeerSuffix: *peerSuffix,
		Protocol:   *protocol,
		Asymmetric: *asymmetric,
		Limit:      *limit,
	}
	if *agg {
		res, err := controlapi.NewUnixClient(*socketPath).TrafficFlowsAggregate(ctx, req)
		if err != nil {
			return err
		}
		return emitTrafficAggregate(stdout, output, res.Aggregate)
	}
	result, err := controlapi.NewUnixClient(*socketPath).TrafficFlows(ctx, req)
	if err != nil {
		return err
	}
	return emitTrafficRows(stdout, output, result.Items)
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

func emitTrafficAggregate(stdout io.Writer, output string, agg logstore.TrafficFlowAggregate) error {
	switch output {
	case "", "table":
		return writeTrafficAggregateTable(stdout, agg)
	case "json":
		return writeJSON(stdout, agg)
	case "yaml":
		return writeYAML(stdout, agg)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func fetchTrafficRowsChunked(ctx context.Context, store *logstore.TrafficFlowLog, filter logstore.TrafficFlowFilter, chunkSize int) ([]logstore.TrafficFlow, error) {
	if chunkSize <= 0 {
		chunkSize = 1000
	}
	target := filter.Limit
	if target <= 0 {
		target = chunkSize
	}
	var out []logstore.TrafficFlow
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
		oldest := rows[len(rows)-1].StartedAt
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

func trafficFlowFilterFromFlags(since, fromStr, toStr, client, peer, peerSuffix, protocol string, asymmetric bool, limit int) (logstore.TrafficFlowFilter, error) {
	sinceT, untilT, err := resolveSinceUntilFlags(since, fromStr, toStr)
	if err != nil {
		return logstore.TrafficFlowFilter{}, err
	}
	return logstore.TrafficFlowFilter{
		Since:      sinceT,
		Until:      untilT,
		Client:     client,
		Peer:       peer,
		PeerSuffix: peerSuffix,
		Protocol:   protocol,
		Asymmetric: asymmetric,
		Limit:      limit,
	}, nil
}

func writeTrafficAggregateTable(stdout io.Writer, agg logstore.TrafficFlowAggregate) error {
	w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SUMMARY")
	if !agg.Since.IsZero() {
		fmt.Fprintf(w, "  since\t%s\n", agg.Since.Format(time.RFC3339))
	}
	if !agg.Until.IsZero() {
		fmt.Fprintf(w, "  until\t%s\n", agg.Until.Format(time.RFC3339))
	}
	fmt.Fprintf(w, "  total\t%d\n", agg.Total)
	fmt.Fprintf(w, "  bytesIn\t%d\n", agg.TotalBytesIn)
	fmt.Fprintf(w, "  bytesOut\t%d\n", agg.TotalBytesOut)
	writeAggregateGroup(w, "BY PROTOCOL", agg.ByProtocol, 10)
	writeAggregateGroup(w, "BY CLIENT (top 10)", agg.ByClient, 10)
	writeAggregateGroup(w, "BY PEER (top 10)", agg.ByPeer, 10)
	return w.Flush()
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
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"firewall (nftables / pf) のログを表示する。\n"+
				"--since は Go の duration 形式 (例: 1h, 30m, 24h)。\n"+
				"絶対時刻 (--from / --to) の指定は別途 issue #36 で対応予定。",
			"routerctl firewall-logs --since 24h --action drop\n"+
				"routerctl firewall-logs --src 192.168.1.10 -o json\n"+
				"routerctl firewall-logs --db /var/log/routerd/firewall-logs.db --since 7d")
	}
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
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
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
