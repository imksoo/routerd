// SPDX-License-Identifier: BSD-3-Clause

package observe

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/hostcmd"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/subprocessio"
)

type ConnectionTable struct {
	Count    int                        `json:"count" yaml:"count"`
	Max      int                        `json:"max,omitempty" yaml:"max,omitempty"`
	ByMark   map[string]int             `json:"byMark,omitempty" yaml:"byMark,omitempty"`
	ByFamily map[string]int             `json:"byFamily,omitempty" yaml:"byFamily,omitempty"`
	BySNAT   *map[string]NATEntryCounts `json:"bySNAT,omitempty" yaml:"bySNAT,omitempty"`
	Entries  []ConnectionEntry          `json:"entries,omitempty" yaml:"entries,omitempty"`
	Stats    []ConntrackCPUStats        `json:"stats,omitempty" yaml:"stats,omitempty"`
}

// NATEntryCounts counts local IPv4 SNAT state, not provider-side AFTR state.
type NATEntryCounts struct {
	Total int `json:"total" yaml:"total"`
	TCP   int `json:"tcp" yaml:"tcp"`
	UDP   int `json:"udp" yaml:"udp"`
	Other int `json:"other" yaml:"other"`
}

func conntrackEntriesBySNAT(entries []ConnectionEntry) map[string]NATEntryCounts {
	counts := make(map[string]NATEntryCounts)
	for _, entry := range entries {
		address := entry.Reply.Destination
		if entry.Family != "ipv4" || address == "" || entry.Original.Source == "" || address == entry.Original.Source {
			continue
		}
		count := counts[address]
		count.Total++
		switch entry.Protocol {
		case "tcp":
			count.TCP++
		case "udp":
			count.UDP++
		default:
			count.Other++
		}
		counts[address] = count
	}
	return counts
}

type ConnectionEntry struct {
	Family        string            `json:"family" yaml:"family"`
	Protocol      string            `json:"protocol" yaml:"protocol"`
	State         string            `json:"state,omitempty" yaml:"state,omitempty"`
	Timeout       int               `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Original      ConntrackTuple    `json:"original" yaml:"original"`
	Reply         ConntrackTuple    `json:"reply" yaml:"reply"`
	Mark          string            `json:"mark,omitempty" yaml:"mark,omitempty"`
	Assured       bool              `json:"assured,omitempty" yaml:"assured,omitempty"`
	LocalRedirect *LocalRedirect    `json:"localRedirect,omitempty" yaml:"localRedirect,omitempty"`
	AppName       string            `json:"appName,omitempty" yaml:"appName,omitempty"`
	AppCategory   string            `json:"appCategory,omitempty" yaml:"appCategory,omitempty"`
	AppConfidence int               `json:"appConfidence,omitempty" yaml:"appConfidence,omitempty"`
	TLSSNI        string            `json:"tlsSNI,omitempty" yaml:"tlsSNI,omitempty"`
	HTTPHost      string            `json:"httpHost,omitempty" yaml:"httpHost,omitempty"`
	DNSQuery      string            `json:"dnsQuery,omitempty" yaml:"dnsQuery,omitempty"`
	RawAttributes map[string]string `json:"rawAttributes,omitempty" yaml:"rawAttributes,omitempty"`
}

type LocalRedirect struct {
	ResourceName      string `json:"resourceName,omitempty" yaml:"resourceName,omitempty"`
	RuleName          string `json:"ruleName,omitempty" yaml:"ruleName,omitempty"`
	DestinationSetRef string `json:"destinationSetRef,omitempty" yaml:"destinationSetRef,omitempty"`
	OriginalAddress   string `json:"originalAddress,omitempty" yaml:"originalAddress,omitempty"`
	RedirectAddress   string `json:"redirectAddress,omitempty" yaml:"redirectAddress,omitempty"`
	RedirectPort      int    `json:"redirectPort,omitempty" yaml:"redirectPort,omitempty"`
	Match             string `json:"match,omitempty" yaml:"match,omitempty"`
}

type ConntrackTuple struct {
	Source              string `json:"source,omitempty" yaml:"source,omitempty"`
	Destination         string `json:"destination,omitempty" yaml:"destination,omitempty"`
	SourcePort          string `json:"sourcePort,omitempty" yaml:"sourcePort,omitempty"`
	DestinationPort     string `json:"destinationPort,omitempty" yaml:"destinationPort,omitempty"`
	SourceHostname      string `json:"sourceHostname,omitempty" yaml:"sourceHostname,omitempty"`
	DestinationHostname string `json:"destinationHostname,omitempty" yaml:"destinationHostname,omitempty"`
	SourceService       string `json:"sourceService,omitempty" yaml:"sourceService,omitempty"`
	DestinationService  string `json:"destinationService,omitempty" yaml:"destinationService,omitempty"`
	Packets             *int64 `json:"packets,omitempty" yaml:"packets,omitempty"`
	Bytes               *int64 `json:"bytes,omitempty" yaml:"bytes,omitempty"`
}

type ConntrackCPUStats struct {
	CPU    string         `json:"cpu" yaml:"cpu"`
	Fields map[string]int `json:"fields" yaml:"fields"`
}

func Connections(ctx context.Context, limit int) (*ConnectionTable, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, features := platform.Current()
	if features.HasPF && !features.HasIproute2 {
		return PFStates(ctx, limit)
	}
	command := hostcmd.ResolveConntrack("conntrack")
	out, err := connectionCommandOutput(ctx, command, "-L", "-o", "extended")
	if err != nil {
		return nil, err
	}
	allEntries, err := parseConntrackEntries(string(out), 0)
	if err != nil {
		return nil, err
	}
	bySNAT := conntrackEntriesBySNAT(allEntries)
	table := &ConnectionTable{
		Count:    readProcInt("/proc/sys/net/netfilter/nf_conntrack_count", len(allEntries)),
		Max:      readProcInt("/proc/sys/net/netfilter/nf_conntrack_max", 0),
		ByMark:   conntrackEntriesByMark(allEntries),
		BySNAT:   &bySNAT,
		ByFamily: conntrackEntriesByFamily(allEntries),
		Entries:  selectConnectionEntries(allEntries, limit),
	}
	if stats, err := conntrackStats(ctx); err == nil {
		table.Stats = stats
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return table, nil
}

func PFStates(ctx context.Context, limit int) (*ConnectionTable, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := connectionCommandOutput(ctx, hostcmd.Resolve("pfctl"), "-ss", "-v")
	if err != nil {
		return nil, err
	}
	allEntries, err := parsePFStateEntries(string(out), 0)
	if err != nil {
		return nil, err
	}
	return &ConnectionTable{
		Count:    len(allEntries),
		ByMark:   map[string]int{"0": len(allEntries)},
		ByFamily: conntrackEntriesByFamily(allEntries),
		Entries:  selectConnectionEntries(allEntries, limit),
	}, nil
}

// PFStateSnapshot returns the PF state count and its configured hard limit.
// PF exposes the count through the state table and the capacity through the
// memory limits, unlike Linux conntrack's procfs counters.
func PFStateSnapshot(ctx context.Context) (*ConnectionTable, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	table, err := PFStates(ctx, 0)
	if err != nil {
		return nil, err
	}
	limitOut, err := connectionCommandOutput(ctx, hostcmd.Resolve("pfctl"), "-sm")
	if err != nil {
		return nil, err
	}
	limit, err := parsePFStateLimit(string(limitOut))
	if err != nil {
		return nil, fmt.Errorf("pfctl -sm: %w", err)
	}
	table.Max = limit
	return table, nil
}

func parsePFStateLimit(output string) (int, error) {
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "states" {
			continue
		}
		for index := len(fields) - 1; index > 0; index-- {
			limit, err := strconv.Atoi(fields[index])
			if err == nil && limit >= 0 {
				return limit, nil
			}
		}
	}
	return 0, fmt.Errorf("states hard limit not found")
}

func selectConnectionEntries(entries []ConnectionEntry, limit int) []ConnectionEntry {
	if limit == 0 {
		return nil
	}
	if limit < 0 || len(entries) <= limit {
		return entries
	}
	groups := map[string][]ConnectionEntry{}
	for _, entry := range entries {
		key := connectionSelectionGroupKey(entry)
		groups[key] = append(groups[key], entry)
	}
	for key := range groups {
		sort.SliceStable(groups[key], func(i, j int) bool {
			return connectionTrafficBytes(groups[key][i]) > connectionTrafficBytes(groups[key][j])
		})
	}
	groupKeys := sortedConnectionGroupKeys(groups)
	quota := map[string]int{}
	base := limit / len(groupKeys)
	remainder := limit % len(groupKeys)
	allocated := 0
	for _, key := range groupKeys {
		n := base
		if remainder > 0 {
			n++
			remainder--
		}
		if n > len(groups[key]) {
			n = len(groups[key])
		}
		quota[key] = n
		allocated += n
	}
	for allocated < limit {
		progress := false
		for _, key := range groupKeys {
			if allocated >= limit {
				break
			}
			if quota[key] >= len(groups[key]) {
				continue
			}
			quota[key]++
			allocated++
			progress = true
		}
		if !progress {
			break
		}
	}
	selected := make([]ConnectionEntry, 0, allocated)
	for _, key := range groupKeys {
		selected = append(selected, groups[key][:quota[key]]...)
	}
	return selected
}

func connectionTrafficBytes(entry ConnectionEntry) int64 {
	var observed int64
	if entry.Original.Bytes != nil {
		observed += *entry.Original.Bytes
	}
	if entry.Reply.Bytes != nil {
		observed += *entry.Reply.Bytes
	}
	return observed
}

func connectionSelectionGroupKey(entry ConnectionEntry) string {
	return normalizedConnectionFamily(entry.Family) + "/" + normalizedConnectionProtocol(entry.Protocol)
}

func sortedConnectionGroupKeys(groups map[string][]ConnectionEntry) []string {
	var keys []string
	for _, family := range []string{"ipv4", "ipv6", "other"} {
		for _, protocol := range []string{"tcp", "udp", "icmp", "icmpv6", "gre", "esp", "other"} {
			key := family + "/" + protocol
			if len(groups[key]) > 0 {
				keys = append(keys, key)
			}
		}
	}
	seen := map[string]struct{}{}
	for _, key := range keys {
		seen[key] = struct{}{}
	}
	var remaining []string
	for key, entries := range groups {
		if len(entries) == 0 {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		remaining = append(remaining, key)
	}
	sort.Strings(remaining)
	return append(keys, remaining...)
}

func normalizedConnectionFamily(family string) string {
	family = strings.ToLower(strings.TrimSpace(family))
	switch family {
	case "ipv4", "ipv6":
		return family
	default:
		return "other"
	}
}

func normalizedConnectionProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "tcp", "udp", "gre", "esp":
		return protocol
	case "icmp":
		return "icmp"
	case "icmpv6", "ipv6-icmp", "ipv6_icmp":
		return "icmpv6"
	case "":
		return "other"
	default:
		return protocol
	}
}

func parseConntrackEntries(output string, limit int) ([]ConnectionEntry, error) {
	var entries []ConnectionEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "conntrack v") {
			continue
		}
		entry, ok := parseConntrackEntry(line)
		if !ok {
			return nil, fmt.Errorf("conntrack dump contains an unrecognized state row")
		}
		entries = append(entries, entry)
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries, nil
}

func parsePFStateEntries(output string, limit int) ([]ConnectionEntry, error) {
	var entries []ConnectionEntry
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		entry, ok := parsePFStateEntry(line)
		if !ok {
			// pfctl -v prints indented sequence/age/counter metadata below
			// each state. An unrecognized state row cannot be counted empty.
			if len(raw) > 0 && (raw[0] == ' ' || raw[0] == '\t') {
				continue
			}
			return nil, fmt.Errorf("pfctl dump contains an unrecognized state row")
		}
		entries = append(entries, entry)
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries, nil
}

func parsePFStateEntry(line string) (ConnectionEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return ConnectionEntry{}, false
	}
	protocol := strings.ToLower(fields[1])
	arrow := -1
	for i, field := range fields {
		if field == "->" || field == "<-" {
			arrow = i
			break
		}
	}
	if arrow < 2 || arrow+1 >= len(fields) {
		return ConnectionEntry{}, false
	}
	rightEnd := arrow + 2
	if rightEnd < len(fields) && strings.HasPrefix(fields[rightEnd], "(") {
		for rightEnd < len(fields) {
			closed := strings.HasSuffix(fields[rightEnd], ")")
			rightEnd++
			if closed {
				break
			}
		}
	}
	left, leftAlternate := splitPFLeftEndpoint(fields[2:arrow])
	right, rightAlternate := splitPFLeftEndpoint(fields[arrow+1 : rightEnd])
	leftHost, leftPort := parsePFEndpoint(left)
	rightHost, rightPort := parsePFEndpoint(right)
	if leftHost == "" || rightHost == "" {
		return ConnectionEntry{}, false
	}
	if leftAlternate == "" {
		leftAlternate = left
	}
	if rightAlternate == "" {
		rightAlternate = right
	}
	originalSource, originalSourcePort := parsePFEndpoint(leftAlternate)
	originalDestination, originalDestinationPort := parsePFEndpoint(rightAlternate)
	replySource, replySourcePort := rightHost, rightPort
	replyDestination, replyDestinationPort := leftHost, leftPort
	if fields[arrow] == "<-" {
		originalSource, originalSourcePort = parsePFEndpoint(rightAlternate)
		originalDestination, originalDestinationPort = parsePFEndpoint(leftAlternate)
		replySource, replySourcePort = leftHost, leftPort
		replyDestination, replyDestinationPort = rightHost, rightPort
	}
	state := ""
	if rightEnd < len(fields) {
		state = fields[rightEnd]
	}
	return ConnectionEntry{
		Family:        pfEndpointFamily(originalSource, originalDestination),
		Protocol:      protocol,
		State:         state,
		Original:      ConntrackTuple{Source: originalSource, SourcePort: originalSourcePort, Destination: originalDestination, DestinationPort: originalDestinationPort},
		Reply:         ConntrackTuple{Source: replySource, SourcePort: replySourcePort, Destination: replyDestination, DestinationPort: replyDestinationPort},
		RawAttributes: map[string]string{"source": "pf", "interface": fields[0]},
	}, true
}

func parseConntrackEntry(line string) (ConnectionEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return ConnectionEntry{}, false
	}
	entry := ConnectionEntry{
		Family:        fields[0],
		Protocol:      fields[2],
		RawAttributes: map[string]string{},
	}
	if normalizedConnectionFamily(entry.Family) == "other" {
		return ConnectionEntry{}, false
	}
	if timeout, err := strconv.Atoi(fields[4]); err == nil {
		entry.Timeout = timeout
	}
	start := 5
	if start < len(fields) && !strings.Contains(fields[start], "=") && !strings.HasPrefix(fields[start], "[") {
		entry.State = fields[start]
		start++
	}
	tuple := &entry.Original
	for _, field := range fields[start:] {
		if field == "[ASSURED]" {
			entry.Assured = true
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		if key == "mark" {
			entry.Mark = value
			continue
		}
		if isTupleKey(key) {
			// Each tuple starts with src. ICMP, GRE and other protocols do
			// not have TCP/UDP's four address/port fields per direction.
			if key == "src" && entry.Original.Source != "" {
				tuple = &entry.Reply
			}
			setTupleField(tuple, key, value)
			continue
		}
		if key == "packets" || key == "bytes" {
			setTupleCounter(tuple, key, value)
			continue
		}
		entry.RawAttributes[key] = value
	}
	if entry.Original.Source == "" || entry.Original.Destination == "" || entry.Reply.Source == "" || entry.Reply.Destination == "" {
		return ConnectionEntry{}, false
	}
	if len(entry.RawAttributes) == 0 {
		entry.RawAttributes = nil
	}
	return entry, true
}

func isTupleKey(key string) bool {
	switch key {
	case "src", "dst", "sport", "dport":
		return true
	default:
		return false
	}
}

func setTupleField(tuple *ConntrackTuple, key, value string) {
	switch key {
	case "src":
		tuple.Source = value
	case "dst":
		tuple.Destination = value
	case "sport":
		tuple.SourcePort = value
	case "dport":
		tuple.DestinationPort = value
	}
}

func setTupleCounter(tuple *ConntrackTuple, key, value string) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed < 0 {
		return
	}
	switch key {
	case "packets":
		tuple.Packets = &parsed
	case "bytes":
		tuple.Bytes = &parsed
	}
}

func splitPFLeftEndpoint(fields []string) (string, string) {
	if len(fields) == 0 {
		return "", ""
	}
	joined := strings.Join(fields, "")
	open := strings.Index(joined, "(")
	close := strings.LastIndex(joined, ")")
	if open >= 0 && close > open {
		return strings.TrimSpace(joined[:open]), strings.TrimSpace(joined[open+1 : close])
	}
	return joined, ""
}

func parsePFEndpoint(value string) (string, string) {
	value = strings.TrimSpace(strings.Trim(value, "()"))
	if value == "" {
		return "", ""
	}
	if strings.HasPrefix(value, "[") {
		end := strings.Index(value, "]")
		if end > 0 {
			host := value[1:end]
			port := strings.TrimPrefix(value[end+1:], ":")
			return host, port
		}
	}
	if open := strings.LastIndex(value, "["); open > 0 && strings.HasSuffix(value, "]") {
		return value[:open], strings.TrimSuffix(value[open+1:], "]")
	}
	if colon := strings.LastIndex(value, ":"); colon > 0 && strings.Count(value, ":") == 1 {
		return value[:colon], value[colon+1:]
	}
	return value, ""
}

func pfEndpointFamily(values ...string) string {
	for _, value := range values {
		if strings.Contains(value, ":") {
			return "ipv6"
		}
	}
	return "ipv4"
}

func conntrackEntriesByMark(entries []ConnectionEntry) map[string]int {
	out := map[string]int{}
	for _, entry := range entries {
		mark := entry.Mark
		if mark == "" {
			mark = "0"
		}
		out[mark]++
	}
	return out
}

func conntrackEntriesByFamily(entries []ConnectionEntry) map[string]int {
	out := map[string]int{}
	for _, entry := range entries {
		out[normalizedConnectionFamily(entry.Family)]++
	}
	return out
}

func conntrackStats(ctx context.Context) ([]ConntrackCPUStats, error) {
	command := hostcmd.ResolveConntrack("conntrack")
	out, err := connectionCommandOutput(ctx, command, "-S")
	if err != nil {
		return nil, err
	}
	var stats []ConntrackCPUStats
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) == 0 {
			continue
		}
		stat := ConntrackCPUStats{CPU: strings.TrimSuffix(fields[0], "\t"), Fields: map[string]int{}}
		stat.CPU = strings.TrimSuffix(stat.CPU, " ")
		for _, field := range fields[1:] {
			key, value, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			parsed, err := strconv.Atoi(value)
			if err == nil {
				stat.Fields[key] = parsed
			}
		}
		stats = append(stats, stat)
	}
	return stats, nil
}

// A complete dump is required for identity and NAT counts even when the UI
// selects fewer rows. Exceeding this bound is an unavailable snapshot, never
// a successful partial table that could end unobserved flows.
const connectionOutputLimit = 64 << 20

func connectionCommandOutput(ctx context.Context, command string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	stdout := subprocessio.NewCapture(connectionOutputLimit)
	stderr := subprocessio.NewCapture(subprocessio.StderrLimit)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = 100 * time.Millisecond
	err := cmd.Run()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("%s: %w", command, ctx.Err())
	}
	if stdout.Exceeded() {
		return nil, &subprocessio.OutputLimitError{Stream: "stdout", Limit: connectionOutputLimit}
	}
	if stderr.Exceeded() {
		return nil, &subprocessio.OutputLimitError{Stream: "stderr", Limit: subprocessio.StderrLimit}
	}
	if err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", command, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func readProcInt(path string, fallback int) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return fallback
	}
	return value
}
