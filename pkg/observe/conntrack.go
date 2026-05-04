package observe

import (
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type NAPTTable struct {
	Count   int                 `json:"count" yaml:"count"`
	Max     int                 `json:"max,omitempty" yaml:"max,omitempty"`
	ByMark  map[string]int      `json:"byMark,omitempty" yaml:"byMark,omitempty"`
	Entries []NAPTTableEntry    `json:"entries,omitempty" yaml:"entries,omitempty"`
	Stats   []ConntrackCPUStats `json:"stats,omitempty" yaml:"stats,omitempty"`
}

type NAPTTableEntry struct {
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

func NAPT(limit int) (*NAPTTable, error) {
	out, err := exec.Command("conntrack", "-L", "-o", "extended").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("conntrack -L -o extended: %w: %s", err, strings.TrimSpace(string(out)))
	}
	allEntries := parseConntrackEntries(string(out), 0)
	sortNAPTEntries(allEntries)
	entries := allEntries
	if limit == 0 {
		entries = nil
	} else if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	table := &NAPTTable{
		Count:   readProcInt("/proc/sys/net/netfilter/nf_conntrack_count", len(allEntries)),
		Max:     readProcInt("/proc/sys/net/netfilter/nf_conntrack_max", 0),
		ByMark:  conntrackEntriesByMark(allEntries),
		Entries: entries,
	}
	if stats, err := conntrackStats(); err == nil {
		table.Stats = stats
	}
	return table, nil
}

func sortNAPTEntries(entries []NAPTTableEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		return naptSortKey(entries[i]) < naptSortKey(entries[j])
	})
}

func naptSortKey(entry NAPTTableEntry) string {
	return strings.Join([]string{
		entry.Family,
		entry.Protocol,
		entry.State,
		entry.Original.Source,
		entry.Original.Destination,
		entry.Original.SourcePort,
		entry.Original.DestinationPort,
		entry.Reply.Source,
		entry.Reply.Destination,
		entry.Reply.SourcePort,
		entry.Reply.DestinationPort,
		entry.Mark,
	}, "\x00")
}

func parseConntrackEntries(output string, limit int) []NAPTTableEntry {
	var entries []NAPTTableEntry
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

func parseConntrackEntry(line string) (NAPTTableEntry, bool) {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return NAPTTableEntry{}, false
	}
	entry := NAPTTableEntry{
		Family:        fields[0],
		Protocol:      fields[2],
		RawAttributes: map[string]string{},
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
		return NAPTTableEntry{}, false
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

func conntrackEntriesByMark(entries []NAPTTableEntry) map[string]int {
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
