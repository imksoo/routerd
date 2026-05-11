// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"routerd/pkg/dpi"
	"routerd/pkg/logstore"
	"routerd/pkg/nflog"
	routerotel "routerd/pkg/otel"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer, stdin io.Reader) error {
	if len(args) > 0 {
		switch args[0] {
		case "selftest":
			return selftest(args[1:], stdout)
		case "daemon":
			return daemon(args[1:], stdin)
		case "help", "-h", "--help":
			usage(stdout)
			return nil
		}
	}
	return daemon(args, stdin)
}

func selftest(args []string, stdout io.Writer) error {
	opts, err := parseOptions("selftest", args)
	if err != nil {
		return err
	}
	log, err := logstore.OpenFirewallLog(opts.path)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := log.RecordExpiredFlow(ctx, logstore.ExpiredFlowEntry{
		Timestamp:    now.Add(-30 * time.Second),
		L3Proto:      "ipv4",
		Protocol:     "tcp",
		OrigSrc:      "172.18.0.10",
		OrigSrcPort:  53168,
		OrigDst:      "198.51.100.10",
		OrigDstPort:  443,
		ReplySrc:     "198.51.100.10",
		ReplySrcPort: 443,
		ReplyDst:     "192.0.0.2",
		ReplyDstPort: 53168,
		Bytes:        4096,
		Raw:          "selftest expired conntrack flow",
	}, opts.expiredFlowTTL, opts.expiredFlowLimit); err != nil {
		return err
	}
	entry := logstore.FirewallLogEntry{Action: "drop", SrcAddress: "192.0.2.10", DstAddress: "198.51.100.10", Protocol: "tcp", L3Proto: "ipv4", RuleName: "selftest"}
	if opts.dpiSocket != "" {
		entry = enrichEntryWithDPI(ctx, opts, entry, selftestTLSPacket("routerd-firewall-selftest.example"))
	}
	if err := recordFirewallEntry(ctx, log, entry, nil); err != nil {
		return err
	}
	orphan := logstore.FirewallLogEntry{
		Action:     "drop",
		SrcAddress: "198.51.100.10",
		SrcPort:    443,
		DstAddress: "192.0.0.2",
		DstPort:    53168,
		Protocol:   "tcp",
		L3Proto:    "ipv4",
		RuleName:   "selftest-orphan-return",
	}
	if err := recordFirewallEntry(ctx, log, orphan, nil); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"ok": true, "path": opts.path})
}

func selftestTLSPacket(host string) []byte {
	payload := dpi.MinimalTLSClientHello(host)
	packet := append([]byte{
		0x45, 0x00, 0x00, 0x00, 0, 0, 0, 0, 64, 6, 0, 0,
		192, 0, 2, 10,
		198, 51, 100, 10,
		0xcf, 0xb0, 0x01, 0xbb,
		0, 0, 0, 0,
		0, 0, 0, 0,
		0x50, 0x18, 0, 0, 0, 0, 0, 0,
	}, payload...)
	totalLen := len(packet)
	binary.BigEndian.PutUint16(packet[2:4], uint16(totalLen))
	return packet
}

func daemon(args []string, stdin io.Reader) error {
	opts, err := parseOptions("daemon", args)
	if err != nil {
		return err
	}
	log, err := logstore.OpenFirewallLog(opts.path)
	if err != nil {
		return err
	}
	defer log.Close()
	ctx := context.Background()
	telemetry, err := routerotel.Setup(ctx, "routerd-firewall-logger")
	if err != nil {
		return err
	}
	defer telemetry.Shutdown(context.Background())
	startExpiredFlowWatchers(ctx, opts, log)
	if opts.group > 0 && opts.inputFile == "" && opts.pflogInterface == "" {
		return runNFLogDaemon(ctx, opts, log, telemetry)
	}
	if opts.pflogInterface != "" && opts.inputFile == "" {
		return runPflogDaemon(ctx, opts, log, telemetry)
	}
	reader := stdin
	if opts.inputFile != "" {
		file, err := os.Open(opts.inputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		entry, ok := parseFirewallLogLine(scanner.Text(), opts.inputFormat)
		if !ok {
			continue
		}
		if err := recordFirewallEntry(ctx, log, entry, telemetry); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func runNFLogDaemon(ctx context.Context, opts options, log *logstore.FirewallLog, telemetry *routerotel.Runtime) error {
	reader, err := nflog.Open(opts.group)
	if err != nil {
		return err
	}
	defer reader.Close()
	for {
		packet, err := reader.Read(ctx)
		if err != nil {
			return err
		}
		entry := firewallLogEntryFromNFLogPacket(packet)
		if opts.dpiSocket != "" && len(packet.Payload) > 0 {
			entry = enrichEntryWithDPI(ctx, opts, entry, packet.Payload)
		}
		if entry.SrcAddress == "" || entry.DstAddress == "" || entry.Protocol == "" {
			continue
		}
		if err := recordFirewallEntry(ctx, log, entry, telemetry); err != nil {
			return err
		}
	}
}

func recordFirewallEntry(ctx context.Context, log *logstore.FirewallLog, entry logstore.FirewallLogEntry, telemetry *routerotel.Runtime) error {
	entry = correlateExpiredReturn(ctx, log, entry, time.Now().UTC())
	if err := log.Record(ctx, entry); err != nil {
		return err
	}
	recordDenyMetric(ctx, telemetry, entry)
	return nil
}

type options struct {
	path             string
	group            int
	inputFile        string
	inputFormat      string
	pflogInterface   string
	tcpdumpPath      string
	dpiSocket        string
	dpiTimeout       time.Duration
	conntrackEvents  bool
	conntrackPath    string
	expiredFlowTTL   time.Duration
	expiredFlowLimit int
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.path, "path", "/var/lib/routerd/firewall-logs.db", "firewall log database path")
	fs.IntVar(&opts.group, "nflog-group", 0, "read Linux NFLOG directly from this group; 0 disables NFLOG input")
	fs.StringVar(&opts.inputFile, "input-file", "", "read firewall log lines from file")
	fs.StringVar(&opts.inputFormat, "input-format", "auto", "input format: auto, json, kv, nflog-tcpdump, pflog-tcpdump")
	fs.StringVar(&opts.pflogInterface, "pflog-interface", "", "read FreeBSD pflog directly from this interface")
	fs.StringVar(&opts.tcpdumpPath, "tcpdump", "tcpdump", "deprecated; tcpdump is no longer used for pflog input")
	fs.StringVar(&opts.dpiSocket, "dpi-socket", "", "optional routerd-dpi-classifier Unix socket")
	fs.DurationVar(&opts.dpiTimeout, "dpi-timeout", 500*time.Millisecond, "DPI classifier request timeout")
	fs.BoolVar(&opts.conntrackEvents, "conntrack-events", true, "watch conntrack destroy events for orphan return correlation")
	fs.StringVar(&opts.conntrackPath, "conntrack", "conntrack", "conntrack command path")
	fs.DurationVar(&opts.expiredFlowTTL, "expired-flow-ttl", time.Hour, "expired flow ring retention")
	fs.IntVar(&opts.expiredFlowLimit, "expired-flow-limit", 100000, "maximum expired flow ring entries")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func startExpiredFlowWatchers(ctx context.Context, opts options, log *logstore.FirewallLog) {
	if opts.conntrackEvents && opts.group > 0 && opts.inputFile == "" && opts.pflogInterface == "" {
		go watchConntrackDestroyLoop(ctx, opts, log)
	}
	if opts.pflogInterface != "" && opts.inputFile == "" {
		go watchPFStateExpireLoop(ctx, opts, log)
	}
}

func watchConntrackDestroyLoop(ctx context.Context, opts options, log *logstore.FirewallLog) {
	for {
		if err := watchConntrackDestroy(ctx, opts, log); err != nil {
			fmt.Fprintf(os.Stderr, "conntrack destroy watcher stopped: %v\n", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func watchConntrackDestroy(ctx context.Context, opts options, log *logstore.FirewallLog) error {
	command := opts.conntrackPath
	if strings.TrimSpace(command) == "" {
		command = "conntrack"
	}
	cmd := exec.CommandContext(ctx, command, "-E", "-e", "DESTROY", "-o", "timestamp,extended")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go io.Copy(io.Discard, stderr)
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		flow, ok := parseConntrackDestroyLine(scanner.Text(), time.Now().UTC())
		if !ok {
			continue
		}
		if err := log.RecordExpiredFlow(ctx, flow, opts.expiredFlowTTL, opts.expiredFlowLimit); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	return cmd.Wait()
}

func enrichEntryWithDPI(ctx context.Context, opts options, entry logstore.FirewallLogEntry, packet []byte) logstore.FirewallLogEntry {
	result, err := classifyPacket(ctx, opts.dpiSocket, opts.dpiTimeout, packet)
	if err != nil || result.AppName == "" || result.AppName == "unknown" {
		return entry
	}
	entry.DPIApp = result.AppName
	entry.DPICategory = result.AppCategory
	entry.DPITLSSNI = result.TLSSNI
	entry.DPIHTTPHost = result.HTTPHost
	entry.DPIDNSQuery = result.DNSQuery
	entry.DPIConfidence = result.AppConfidence
	entry.Hint = appendDPIHintFields(entry.Hint, result)
	return entry
}

func appendDPIHintFields(hint string, result dpi.ClassifyResult) string {
	parts := []string{}
	if strings.TrimSpace(hint) != "" {
		parts = append(parts, hint)
	}
	parts = append(parts, "dpi.app="+result.AppName)
	if result.AppCategory != "" {
		parts = append(parts, "dpi.category="+result.AppCategory)
	}
	if result.TLSSNI != "" {
		parts = append(parts, "dpi.tls_sni="+result.TLSSNI)
	}
	if result.HTTPHost != "" {
		parts = append(parts, "dpi.http_host="+result.HTTPHost)
	}
	if result.DNSQuery != "" {
		parts = append(parts, "dpi.dns_query="+result.DNSQuery)
	}
	if result.AppConfidence > 0 {
		parts = append(parts, "dpi.confidence="+strconv.Itoa(result.AppConfidence))
	}
	return strings.Join(parts, " ")
}

func recordDenyMetric(ctx context.Context, telemetry *routerotel.Runtime, entry logstore.FirewallLogEntry) {
	if telemetry == nil || !isDenyAction(entry.Action) {
		return
	}
	counter := telemetry.Counter("routerd.firewall.deny.total")
	if counter == nil {
		return
	}
	counter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("routerd.firewall.rule", entry.RuleName),
		attribute.String("routerd.firewall.action", entry.Action),
		attribute.String("network.protocol.name", firewallDPIProtocol(entry)),
		attribute.String("network.transport", entry.Protocol),
		attribute.String("network.type", entry.L3Proto),
		attribute.String("routerd.firewall.correlation", firewallCorrelation(entry)),
	))
}

func isDenyAction(action string) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "drop", "deny", "reject", "block":
		return true
	default:
		return false
	}
}

func firewallDPIProtocol(entry logstore.FirewallLogEntry) string {
	if entry.DPIApp != "" {
		return entry.DPIApp
	}
	if entry.Protocol != "" {
		return entry.Protocol
	}
	return entry.L3Proto
}

func firewallCorrelation(entry logstore.FirewallLogEntry) string {
	if entry.Correlation != "" {
		return entry.Correlation
	}
	if isDenyAction(entry.Action) {
		return "true_suspicious"
	}
	return "none"
}

func correlateExpiredReturn(ctx context.Context, log *logstore.FirewallLog, entry logstore.FirewallLogEntry, now time.Time) logstore.FirewallLogEntry {
	if !isDenyAction(entry.Action) {
		return entry
	}
	flow, ok, err := log.FindExpiredReturn(ctx, entry, now, time.Hour)
	if err != nil || !ok {
		entry.Correlation = "true_suspicious"
		if entry.CorrelationDetail == "" {
			entry.CorrelationDetail = "no expired reverse flow match"
		}
		return entry
	}
	age := now.Sub(flow.Timestamp)
	if age < 0 {
		age = 0
	}
	entry.Correlation = "orphan_return"
	entry.ExpiredAgeSeconds = int(age.Seconds())
	entry.ExpiredBytes = flow.Bytes
	entry.CorrelationDetail = fmt.Sprintf("likely orphan return from expired conn (orig: %s:%d -> %s:%d, expired %s ago, %s transferred)", flow.OrigSrc, flow.OrigSrcPort, flow.OrigDst, flow.OrigDstPort, shortDuration(age), byteCount(flow.Bytes))
	entry.Hint = appendHint(entry.Hint, entry.CorrelationDetail)
	return entry
}

func appendHint(hint, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return hint
	}
	if strings.TrimSpace(hint) == "" {
		return value
	}
	return hint + " " + value
}

func shortDuration(d time.Duration) string {
	if d < time.Minute {
		return strconv.Itoa(int(d.Seconds())) + "s"
	}
	if d < time.Hour {
		return strconv.Itoa(int(d.Minutes())) + "m"
	}
	return strconv.Itoa(int(d.Hours())) + "h"
}

func byteCount(value int64) string {
	if value <= 0 {
		return "0B"
	}
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	current := float64(value)
	unit := 0
	for current >= 1024 && unit < len(units)-1 {
		current /= 1024
		unit++
	}
	if unit == 0 {
		return strconv.FormatInt(value, 10) + "B"
	}
	return fmt.Sprintf("%.1f%s", current, units[unit])
}

func parseConntrackDestroyLine(line string, now time.Time) (logstore.ExpiredFlowEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" || !strings.Contains(line, "DESTROY") {
		return logstore.ExpiredFlowEntry{}, false
	}
	fields := strings.Fields(line)
	protocol := ""
	for i, field := range fields {
		candidate := strings.ToLower(strings.Trim(field, "[]"))
		if candidate == "tcp" || candidate == "udp" || candidate == "icmp" || candidate == "icmpv6" || candidate == "ipv6-icmp" {
			protocol = normalizeProto(candidate)
			fields = fields[i+1:]
			break
		}
	}
	if protocol == "" {
		return logstore.ExpiredFlowEntry{}, false
	}
	var tuples []map[string]string
	current := map[string]string{}
	var packets, bytes int64
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), "[]")
		switch key {
		case "src":
			if _, exists := current["src"]; exists && current["dst"] != "" {
				tuples = append(tuples, current)
				current = map[string]string{}
			}
			current["src"] = value
		case "dst", "sport", "dport":
			current[key] = value
		case "packets":
			packets += parseInt64(value)
		case "bytes":
			bytes += parseInt64(value)
		}
	}
	if len(current) > 0 {
		tuples = append(tuples, current)
	}
	if len(tuples) == 0 || tuples[0]["src"] == "" || tuples[0]["dst"] == "" {
		return logstore.ExpiredFlowEntry{}, false
	}
	reply := map[string]string{}
	if len(tuples) > 1 {
		reply = tuples[1]
	}
	flow := logstore.ExpiredFlowEntry{
		Timestamp:    now,
		Protocol:     protocol,
		L3Proto:      conntrackL3Proto(tuples[0]["src"], tuples[0]["dst"]),
		OrigSrc:      tuples[0]["src"],
		OrigSrcPort:  parseInt(tuples[0]["sport"]),
		OrigDst:      tuples[0]["dst"],
		OrigDstPort:  parseInt(tuples[0]["dport"]),
		ReplySrc:     reply["src"],
		ReplySrcPort: parseInt(reply["sport"]),
		ReplyDst:     reply["dst"],
		ReplyDstPort: parseInt(reply["dport"]),
		Packets:      packets,
		Bytes:        bytes,
		Raw:          line,
	}
	if flow.ReplySrc == "" {
		flow.ReplySrc = flow.OrigDst
		flow.ReplySrcPort = flow.OrigDstPort
		flow.ReplyDst = flow.OrigSrc
		flow.ReplyDstPort = flow.OrigSrcPort
	}
	return flow, true
}

func normalizeProto(proto string) string {
	switch strings.ToLower(proto) {
	case "ipv6-icmp":
		return "icmpv6"
	default:
		return strings.ToLower(proto)
	}
}

func conntrackL3Proto(values ...string) string {
	for _, value := range values {
		if strings.Contains(value, ":") {
			return "ipv6"
		}
	}
	return "ipv4"
}

func parseInt(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func parseInt64(value string) int64 {
	parsed, _ := strconv.ParseInt(value, 10, 64)
	return parsed
}

func classifyPacket(ctx context.Context, socket string, timeout time.Duration, packet []byte) (dpi.ClassifyResult, error) {
	if socket == "" {
		return dpi.ClassifyResult{}, nil
	}
	request, err := json.Marshal(dpi.ClassifyRequest{Packet: packet})
	if err != nil {
		return dpi.ClassifyResult{}, err
	}
	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var dialer net.Dialer
			return dialer.DialContext(ctx, "unix", socket)
		}},
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/classify", bytes.NewReader(request))
	if err != nil {
		return dpi.ClassifyResult{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(httpReq)
	if err != nil {
		return dpi.ClassifyResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return dpi.ClassifyResult{}, fmt.Errorf("dpi classifier status %s", resp.Status)
	}
	var result dpi.ClassifyResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return dpi.ClassifyResult{}, err
	}
	return result, nil
}

func firewallLogEntryFromIPPacket(timestamp time.Time, payload []byte, hint string) (logstore.FirewallLogEntry, bool) {
	if len(payload) < 1 {
		return logstore.FirewallLogEntry{}, false
	}
	version := payload[0] >> 4
	switch version {
	case 4:
		return firewallLogEntryFromIPv4Packet(timestamp, payload, hint)
	case 6:
		return firewallLogEntryFromIPv6Packet(timestamp, payload, hint)
	default:
		return logstore.FirewallLogEntry{}, false
	}
}

func firewallLogEntryFromIPv4Packet(timestamp time.Time, payload []byte, hint string) (logstore.FirewallLogEntry, bool) {
	if len(payload) < 20 {
		return logstore.FirewallLogEntry{}, false
	}
	ihl := int(payload[0]&0x0f) * 4
	if ihl < 20 || len(payload) < ihl {
		return logstore.FirewallLogEntry{}, false
	}
	proto := ipProtocolName(payload[9])
	src := netip.AddrFrom4([4]byte(payload[12:16])).String()
	dst := netip.AddrFrom4([4]byte(payload[16:20])).String()
	srcPort, dstPort := transportPorts(proto, payload[ihl:])
	return logstore.FirewallLogEntry{
		Timestamp:  timestamp,
		Action:     "drop",
		SrcAddress: src,
		SrcPort:    srcPort,
		DstAddress: dst,
		DstPort:    dstPort,
		Protocol:   proto,
		L3Proto:    "ipv4",
		Hint:       hint,
	}, true
}

func firewallLogEntryFromIPv6Packet(timestamp time.Time, payload []byte, hint string) (logstore.FirewallLogEntry, bool) {
	if len(payload) < 40 {
		return logstore.FirewallLogEntry{}, false
	}
	proto := ipProtocolName(payload[6])
	src, ok := netip.AddrFromSlice(payload[8:24])
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	dst, ok := netip.AddrFromSlice(payload[24:40])
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	srcPort, dstPort := transportPorts(proto, payload[40:])
	return logstore.FirewallLogEntry{
		Timestamp:  timestamp,
		Action:     "drop",
		SrcAddress: src.String(),
		SrcPort:    srcPort,
		DstAddress: dst.String(),
		DstPort:    dstPort,
		Protocol:   proto,
		L3Proto:    "ipv6",
		Hint:       hint,
	}, true
}

func transportPorts(proto string, payload []byte) (int, int) {
	if proto != "tcp" && proto != "udp" {
		return 0, 0
	}
	if len(payload) < 4 {
		return 0, 0
	}
	return int(binary.BigEndian.Uint16(payload[0:2])), int(binary.BigEndian.Uint16(payload[2:4]))
}

func ipProtocolName(proto byte) string {
	switch proto {
	case 1:
		return "icmp"
	case 6:
		return "tcp"
	case 17:
		return "udp"
	case 58:
		return "icmpv6"
	default:
		return strconv.Itoa(int(proto))
	}
}

func parseFirewallLogLine(line, format string) (logstore.FirewallLogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return logstore.FirewallLogEntry{}, false
	}
	switch format {
	case "", "auto":
	case "json":
		return parseJSONFirewallLogLine(line)
	case "kv":
		return parseKeyValueFirewallLogLine(line)
	case "pflog-tcpdump":
		return parsePflogTCPDumpLine(line)
	case "nflog-tcpdump":
		return parseNFLogTCPDumpLine(line)
	default:
		return logstore.FirewallLogEntry{}, false
	}
	if strings.HasPrefix(line, "{") {
		return parseJSONFirewallLogLine(line)
	}
	if strings.Contains(line, "rule ") && strings.Contains(line, " on ") && strings.Contains(line, " > ") {
		if entry, ok := parsePflogTCPDumpLine(line); ok {
			return entry, true
		}
	}
	if strings.Contains(line, " > ") {
		if entry, ok := parseNFLogTCPDumpLine(line); ok {
			return entry, true
		}
	}
	return parseKeyValueFirewallLogLine(line)
}

func parseJSONFirewallLogLine(line string) (logstore.FirewallLogEntry, bool) {
	var entry logstore.FirewallLogEntry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return logstore.FirewallLogEntry{}, false
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	return entry, entry.Action != "" && entry.SrcAddress != "" && entry.DstAddress != "" && entry.Protocol != ""
}

func firewallLogEntryFromNFLogPacket(packet nflog.Packet) logstore.FirewallLogEntry {
	prefix := strings.TrimSpace(packet.Prefix)
	action := actionFromFirewallPrefix(prefix)
	if action == "" {
		action = "drop"
	}
	packetBytes := packet.PacketBytes
	if packetBytes == 0 {
		packetBytes = len(packet.Payload)
	}
	return logstore.FirewallLogEntry{
		Timestamp:   packet.Timestamp,
		RuleName:    prefix,
		Action:      action,
		SrcAddress:  packet.SrcAddress,
		SrcPort:     packet.SrcPort,
		DstAddress:  packet.DstAddress,
		DstPort:     packet.DstPort,
		Protocol:    packet.Protocol,
		L3Proto:     packet.L3Proto,
		InIface:     packet.InIface,
		OutIface:    packet.OutIface,
		PacketBytes: packetBytes,
		Hint:        "nflog-netlink",
	}
}

func parseKeyValueFirewallLogLine(line string) (logstore.FirewallLogEntry, bool) {
	fields := map[string]string{}
	for _, part := range strings.Fields(line) {
		key, value, ok := strings.Cut(part, "=")
		if ok {
			fields[key] = strings.Trim(value, `"`)
		}
	}
	entry := logstore.FirewallLogEntry{
		Timestamp:   time.Now().UTC(),
		ZoneFrom:    fields["zone_from"],
		ZoneTo:      fields["zone_to"],
		RuleName:    fields["rule_name"],
		Action:      firstNonEmpty(fields["action"], "drop"),
		SrcAddress:  firstNonEmpty(fields["src_address"], fields["src"]),
		DstAddress:  firstNonEmpty(fields["dst_address"], fields["dst"]),
		SrcPort:     atoi(fields["src_port"]),
		DstPort:     atoi(fields["dst_port"]),
		Protocol:    firstNonEmpty(fields["protocol"], fields["proto"]),
		L3Proto:     firstNonEmpty(fields["l3_proto"], fields["family"], "ipv4"),
		InIface:     fields["in_iface"],
		OutIface:    fields["out_iface"],
		PacketBytes: atoi(fields["packet_bytes"]),
		Hint:        fields["hint"],
	}
	return entry, entry.SrcAddress != "" && entry.DstAddress != "" && entry.Protocol != ""
}

func parsePflogTCPDumpLine(line string) (logstore.FirewallLogEntry, bool) {
	// tcpdump timestamps also contain colons. Keep the header starting at
	// "rule " so the parser stays independent of the selected time format.
	if idx := strings.Index(line, "rule "); idx >= 0 {
		line = line[idx:]
	}
	onIdx := strings.Index(line, " on ")
	if onIdx < 0 {
		return logstore.FirewallLogEntry{}, false
	}
	relSep := strings.Index(line[onIdx:], ": ")
	if relSep < 0 {
		return logstore.FirewallLogEntry{}, false
	}
	sep := onIdx + relSep
	beforePacket := line[:sep]
	packet := line[sep+2:]
	headerFields := strings.Fields(beforePacket)
	if len(headerFields) < 5 || headerFields[0] != "rule" {
		return logstore.FirewallLogEntry{}, false
	}
	ruleName := "rule " + strings.TrimSuffix(headerFields[1], ":")
	action := ""
	direction := ""
	if len(headerFields) >= 3 {
		action = strings.TrimSuffix(headerFields[2], ":")
	}
	for i, field := range headerFields {
		if (field == "in" || field == "out") && i+2 < len(headerFields) && headerFields[i+1] == "on" {
			direction = field
			break
		}
	}
	iface := ""
	if marker := " on "; strings.Contains(beforePacket, marker) {
		after := strings.SplitN(beforePacket, marker, 2)[1]
		iface = strings.TrimSpace(strings.TrimSuffix(after, ":"))
	}
	endpoints, rest, ok := splitTCPDumpPacket(packet)
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	src, dst, ok := strings.Cut(endpoints, " > ")
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	srcAddress, srcPort := splitEndpoint(src)
	dstAddress, dstPort := splitEndpoint(dst)
	protocol := protocolFromPflogPayload(rest)
	l3 := "ipv4"
	if strings.Contains(srcAddress, ":") || strings.Contains(dstAddress, ":") {
		l3 = "ipv6"
	}
	entry := logstore.FirewallLogEntry{
		Timestamp:   time.Now().UTC(),
		RuleName:    ruleName,
		Action:      normalizeFirewallAction(action),
		SrcAddress:  srcAddress,
		SrcPort:     atoi(srcPort),
		DstAddress:  dstAddress,
		DstPort:     atoi(dstPort),
		Protocol:    protocol,
		L3Proto:     l3,
		PacketBytes: packetBytesFromPflogPayload(rest),
		Hint:        "pflog-tcpdump",
	}
	if direction == "in" {
		entry.InIface = iface
	} else if direction == "out" {
		entry.OutIface = iface
	}
	return entry, entry.SrcAddress != "" && entry.DstAddress != "" && entry.Protocol != ""
}

func parseNFLogTCPDumpLine(line string) (logstore.FirewallLogEntry, bool) {
	packet, l3, ok := nflogPacketPayload(line)
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	endpoints, rest, ok := splitTCPDumpPacket(packet)
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	src, dst, ok := strings.Cut(endpoints, " > ")
	if !ok {
		return logstore.FirewallLogEntry{}, false
	}
	srcAddress, srcPort := splitEndpoint(src)
	dstAddress, dstPort := splitEndpoint(dst)
	protocol := protocolFromPflogPayload(rest)
	prefix := firewallPrefixFromNFLog(line)
	action := actionFromFirewallPrefix(prefix)
	if action == "" {
		action = "drop"
	}
	entry := logstore.FirewallLogEntry{
		Timestamp:   time.Now().UTC(),
		RuleName:    prefix,
		Action:      action,
		SrcAddress:  srcAddress,
		SrcPort:     atoi(srcPort),
		DstAddress:  dstAddress,
		DstPort:     atoi(dstPort),
		Protocol:    protocol,
		L3Proto:     l3,
		PacketBytes: packetBytesFromPflogPayload(rest),
		Hint:        "nflog-tcpdump",
	}
	return entry, entry.SrcAddress != "" && entry.DstAddress != "" && entry.Protocol != ""
}

func nflogPacketPayload(line string) (string, string, bool) {
	for _, marker := range []struct {
		Text string
		L3   string
	}{
		{Text: " IP6 ", L3: "ipv6"},
		{Text: " IP ", L3: "ipv4"},
		{Text: "IP6 ", L3: "ipv6"},
		{Text: "IP ", L3: "ipv4"},
	} {
		if idx := strings.Index(line, marker.Text); idx >= 0 {
			payload := strings.TrimSpace(line[idx+len(marker.Text):])
			return payload, marker.L3, payload != ""
		}
	}
	return "", "", false
}

func firewallPrefixFromNFLog(line string) string {
	start := strings.Index(line, "routerd firewall ")
	if start < 0 {
		return ""
	}
	prefix := strings.TrimSpace(line[start:])
	for _, marker := range []string{" IP6 ", " IP ", "IP6 ", "IP "} {
		if idx := strings.Index(prefix, marker); idx > 0 {
			prefix = strings.TrimSpace(prefix[:idx])
			break
		}
	}
	return strings.Trim(prefix, `"'`)
}

func actionFromFirewallPrefix(prefix string) string {
	text := strings.ToLower(prefix)
	switch {
	case strings.Contains(text, "deny"), strings.Contains(text, "drop"):
		return "drop"
	case strings.Contains(text, "reject"):
		return "reject"
	case strings.Contains(text, "accept"), strings.Contains(text, "pass"):
		return "accept"
	default:
		return ""
	}
}

func splitEndpoint(value string) (string, string) {
	value = strings.TrimSpace(strings.TrimSuffix(value, ","))
	if value == "" {
		return "", ""
	}
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end > 0 {
			return value[1:end], strings.TrimPrefix(value[end+1:], ".")
		}
	}
	if open := strings.LastIndex(value, "."); open > 0 {
		return value[:open], value[open+1:]
	}
	return value, ""
}

func splitTCPDumpPacket(packet string) (string, string, bool) {
	if idx := strings.LastIndex(packet, ": "); idx >= 0 {
		return packet[:idx], packet[idx+2:], true
	}
	return strings.Cut(packet, ":")
}

func protocolFromPflogPayload(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	upper := strings.ToUpper(value)
	switch {
	case strings.HasPrefix(upper, "UDP"):
		return "udp"
	case strings.HasPrefix(upper, "ICMP6"):
		return "icmpv6"
	case strings.HasPrefix(upper, "ICMP"):
		return "icmp"
	case strings.Contains(upper, "FLAGS"):
		return "tcp"
	default:
		fields := strings.Fields(value)
		if len(fields) > 0 {
			return strings.ToLower(strings.Trim(fields[0], ","))
		}
		return ""
	}
}

func normalizeFirewallAction(value string) string {
	switch strings.ToLower(strings.Trim(value, ":")) {
	case "pass":
		return "accept"
	case "block", "drop":
		return "drop"
	default:
		return value
	}
}

func packetBytesFromPflogPayload(value string) int {
	fields := strings.Fields(strings.ReplaceAll(value, ",", ""))
	for i, field := range fields {
		if (field == "length" || field == "len") && i+1 < len(fields) {
			return atoi(fields[i+1])
		}
	}
	return 0
}

func atoi(value string) int {
	parsed, _ := strconv.Atoi(value)
	return parsed
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db [--nflog-group 1 | --pflog-interface pflog0]")
}
