package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"routerd/pkg/logstore"
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
	entry := logstore.FirewallLogEntry{Action: "drop", SrcAddress: "192.0.2.10", DstAddress: "198.51.100.10", Protocol: "tcp", L3Proto: "ipv4", RuleName: "selftest"}
	if err := log.Record(context.Background(), entry); err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"ok": true, "path": opts.path})
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
	reader := stdin
	var cmd *exec.Cmd
	if opts.inputFile != "" {
		file, err := os.Open(opts.inputFile)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	} else if opts.pflogInterface != "" {
		cmd = exec.Command(opts.tcpdumpPath, "-n", "-e", "-tttt", "-l", "-i", opts.pflogInterface)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		defer func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}()
		reader = stdout
	} else if opts.group > 0 {
		cmd = exec.Command(opts.tcpdumpPath, "-n", "-tttt", "-l", "-i", "nflog:"+strconv.Itoa(opts.group))
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		defer func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
		}()
		reader = stdout
	}
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		entry, ok := parseFirewallLogLine(scanner.Text(), opts.inputFormat)
		if !ok {
			continue
		}
		if err := log.Record(context.Background(), entry); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if cmd != nil {
		return cmd.Wait()
	}
	return nil
}

type options struct {
	path           string
	group          int
	inputFile      string
	inputFormat    string
	pflogInterface string
	tcpdumpPath    string
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.path, "path", "/var/lib/routerd/firewall-logs.db", "firewall log database path")
	fs.IntVar(&opts.group, "nflog-group", 0, "read Linux NFLOG through tcpdump on this group; 0 disables NFLOG input")
	fs.StringVar(&opts.inputFile, "input-file", "", "read firewall log lines from file")
	fs.StringVar(&opts.inputFormat, "input-format", "auto", "input format: auto, json, kv, nflog-tcpdump, pflog-tcpdump")
	fs.StringVar(&opts.pflogInterface, "pflog-interface", "", "read FreeBSD pflog through tcpdump on this interface")
	fs.StringVar(&opts.tcpdumpPath, "tcpdump", "tcpdump", "tcpdump command path for pflog input")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
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
