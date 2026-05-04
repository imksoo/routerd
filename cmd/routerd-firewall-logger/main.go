package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
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
	// Phase 2.9 keeps the daemon buildable without CGO. Production NFLOG
	// ingestion will plug into this writer; stdin/input-file mode lets tests
	// and dry-run deployments exercise the database path now.
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
		entry, ok := parseFirewallLogLine(scanner.Text())
		if !ok {
			continue
		}
		if err := log.Record(context.Background(), entry); err != nil {
			return err
		}
	}
	return scanner.Err()
}

type options struct {
	path      string
	group     int
	inputFile string
}

func parseOptions(name string, args []string) (options, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	opts := options{}
	fs.StringVar(&opts.path, "path", "/var/lib/routerd/firewall-logs.db", "firewall log database path")
	fs.IntVar(&opts.group, "nflog-group", 1, "NFLOG group number reserved for production ingestion")
	fs.StringVar(&opts.inputFile, "input-file", "", "read JSON or key=value firewall log lines from file")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	return opts, nil
}

func parseFirewallLogLine(line string) (logstore.FirewallLogEntry, bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return logstore.FirewallLogEntry{}, false
	}
	var entry logstore.FirewallLogEntry
	if strings.HasPrefix(line, "{") {
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return logstore.FirewallLogEntry{}, false
		}
		if entry.Timestamp.IsZero() {
			entry.Timestamp = time.Now().UTC()
		}
		return entry, entry.Action != "" && entry.SrcAddress != "" && entry.DstAddress != "" && entry.Protocol != ""
	}
	fields := map[string]string{}
	for _, part := range strings.Fields(line) {
		key, value, ok := strings.Cut(part, "=")
		if ok {
			fields[key] = strings.Trim(value, `"`)
		}
	}
	entry = logstore.FirewallLogEntry{
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
	fmt.Fprintln(w, "usage: routerd-firewall-logger daemon --path /var/lib/routerd/firewall-logs.db --nflog-group 1")
}
