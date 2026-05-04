package observe

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"routerd/pkg/platform"
)

type ConnectionTable struct {
	Count    int                 `json:"count" yaml:"count"`
	Max      int                 `json:"max,omitempty" yaml:"max,omitempty"`
	ByMark   map[string]int      `json:"byMark,omitempty" yaml:"byMark,omitempty"`
	ByFamily map[string]int      `json:"byFamily,omitempty" yaml:"byFamily,omitempty"`
	Entries  []ConnectionEntry   `json:"entries,omitempty" yaml:"entries,omitempty"`
	Stats    []ConntrackCPUStats `json:"stats,omitempty" yaml:"stats,omitempty"`
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
	RawAttributes map[string]string `json:"rawAttributes,omitempty" yaml:"rawAttributes,omitempty"`
}

type ConntrackTuple struct {
	Source          string `json:"source,omitempty" yaml:"source,omitempty"`
	Destination     string `json:"destination,omitempty" yaml:"destination,omitempty"`
	SourcePort      string `json:"sourcePort,omitempty" yaml:"sourcePort,omitempty"`
	DestinationPort string `json:"destinationPort,omitempty" yaml:"destinationPort,omitempty"`
}

type ConntrackCPUStats struct {
	CPU    string         `json:"cpu" yaml:"cpu"`
	Fields map[string]int `json:"fields" yaml:"fields"`
}

func Connections(limit int) (*ConnectionTable, error) {
	_, features := platform.Current()
	if features.HasPF && !features.HasIproute2 {
		return PFStates(limit)
	}
	out, err := exec.Command("conntrack", "-L", "-o", "extended").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("conntrack -L -o extended: %w: %s", err, strings.TrimSpace(string(out)))
	}
	allEntries := parseConntrackEntries(string(out), 0)
	table := &ConnectionTable{
		Count:    readProcInt("/proc/sys/net/netfilter/nf_conntrack_count", len(allEntries)),
		Max:      readProcInt("/proc/sys/net/netfilter/nf_conntrack_max", 0),
		ByMark:   conntrackEntriesByMark(allEntries),
		ByFamily: conntrackEntriesByFamily(allEntries),
		Entries:  selectConnectionEntries(allEntries, limit),
	}
	if stats, err := conntrackStats(); err == nil {
		table.Stats = stats
	}
	return table, nil
}

func PFStates(limit int) (*ConnectionTable, error) {
	out, err := exec.Command("pfctl", "-ss", "-v").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("pfctl -ss -v: %w: %s", err, strings.TrimSpace(string(out)))
	}
	allEntries := parsePFStateEntries(string(out), 0)
	return &ConnectionTable{
		Count:    len(allEntries),
		ByMark:   map[string]int{"0": len(allEntries)},
		ByFamily: conntrackEntriesByFamily(allEntries),
		Entries:  selectConnectionEntries(allEntries, limit),
	}, nil
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

func parseConntrackEntries(output string, limit int) []ConnectionEntry {
	var entries []ConnectionEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "conntrack v") {
			continue
		}
		entry, ok := parseConntrackEntry(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries
}

func parsePFStateEntries(output string, limit int) []ConnectionEntry {
	var entries []ConnectionEntry
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		entry, ok := parsePFStateEntry(line)
		if !ok {
			continue
		}
		entries = append(entries, entry)
		if limit > 0 && len(entries) >= limit {
			break
		}
	}
	return entries
}

func parsePFStateEntry(line string) (ConnectionEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "all" {
		return ConnectionEntry{}, false
	}
	protocol := strings.ToLower(fields[1])
	arrow := -1
	for i, field := range fields {
		if field == "->" {
			arrow = i
			break
		}
	}
	if arrow < 2 || arrow+1 >= len(fields) {
		return ConnectionEntry{}, false
	}
	leftFields := fields[2:arrow]
	rightField := fields[arrow+1]
	state := ""
	if arrow+2 < len(fields) {
		state = fields[arrow+2]
	}
	visibleLeft, innerLeft := splitPFLeftEndpoint(leftFields)
	leftHost, leftPort := parsePFEndpoint(visibleLeft)
	innerHost, innerPort := parsePFEndpoint(innerLeft)
	rightHost, rightPort := parsePFEndpoint(rightField)
	if leftHost == "" || rightHost == "" {
		return ConnectionEntry{}, false
	}
	originalSource := leftHost
	originalSourcePort := leftPort
	replyDestination := leftHost
	replyDestinationPort := leftPort
	if innerHost != "" {
		originalSource = innerHost
		originalSourcePort = innerPort
		replyDestination = leftHost
		replyDestinationPort = leftPort
	}
	entry := ConnectionEntry{
		Family:   pfEndpointFamily(originalSource, rightHost),
		Protocol: protocol,
		State:    state,
		Original: ConntrackTuple{
			Source:          originalSource,
			SourcePort:      originalSourcePort,
			Destination:     rightHost,
			DestinationPort: rightPort,
		},
		Reply: ConntrackTuple{
			Source:          rightHost,
			SourcePort:      rightPort,
			Destination:     replyDestination,
			DestinationPort: replyDestinationPort,
		},
		RawAttributes: map[string]string{"source": "pf"},
	}
	return entry, true
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
	tupleIndex := 0
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
		if isTupleKey(key) && tupleIndex < 8 {
			if tupleIndex < 4 {
				setTupleField(&entry.Original, key, value)
			} else {
				setTupleField(&entry.Reply, key, value)
			}
			tupleIndex++
			continue
		}
		entry.RawAttributes[key] = value
	}
	if entry.Original.Source == "" && entry.Reply.Source == "" {
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

func conntrackStats() ([]ConntrackCPUStats, error) {
	out, err := exec.Command("conntrack", "-S").CombinedOutput()
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
