// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/platform"
)

func neighborList() ([]NeighborEntry, error) {
	if platform.CurrentOS() == platform.OSFreeBSD {
		return freeBSDNeighborList()
	}
	out, err := exec.Command("ip", "-j", "neigh", "show").Output()
	if err != nil {
		return nil, err
	}
	return parseIPNeighborJSON(out)
}

func freeBSDNeighborList() ([]NeighborEntry, error) {
	var combined []NeighborEntry
	var errs []string
	if out, err := exec.Command("arp", "-an").Output(); err == nil {
		combined = append(combined, parseFreeBSDARP(out)...)
	} else {
		errs = append(errs, err.Error())
	}
	if out, err := exec.Command("ndp", "-an").Output(); err == nil {
		combined = append(combined, parseFreeBSDNDP(out)...)
	} else {
		errs = append(errs, err.Error())
	}
	if len(combined) == 0 && len(errs) > 0 {
		return nil, fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	sort.Slice(combined, func(i, j int) bool {
		if combined[i].IfName != combined[j].IfName {
			return combined[i].IfName < combined[j].IfName
		}
		return combined[i].IP < combined[j].IP
	})
	return combined, nil
}

func parseFreeBSDARP(data []byte) []NeighborEntry {
	var out []NeighborEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 6 || fields[0] == "?" && !strings.HasPrefix(fields[1], "(") {
			continue
		}
		ip := strings.Trim(fields[1], "()")
		mac := strings.ToLower(fields[3])
		if ip == "" || mac == "" || mac == "(incomplete)" {
			continue
		}
		ifname := ""
		for i, field := range fields {
			if field == "on" && i+1 < len(fields) {
				ifname = fields[i+1]
				break
			}
		}
		out = append(out, NeighborEntry{IP: ip, IfName: ifname, MAC: mac, State: "REACHABLE", Source: "arp", Vendor: macVendor(mac)})
	}
	return out
}

func parseFreeBSDNDP(data []byte) []NeighborEntry {
	var out []NeighborEntry
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || strings.EqualFold(fields[0], "Neighbor") {
			continue
		}
		ip := strings.TrimSuffix(fields[0], "%"+fields[2])
		mac := strings.ToLower(fields[1])
		if ip == "" || mac == "" || mac == "(incomplete)" || strings.EqualFold(mac, "Linklayer") {
			continue
		}
		state := ""
		if len(fields) >= 5 {
			state = fields[4]
		}
		out = append(out, NeighborEntry{IP: ip, IfName: fields[2], MAC: mac, State: state, Source: "ndp", Vendor: macVendor(mac)})
	}
	return out
}

func parseIPNeighborJSON(data []byte) ([]NeighborEntry, error) {
	var raw []struct {
		Dst    string          `json:"dst"`
		Dev    string          `json:"dev"`
		LLAddr string          `json:"lladdr"`
		State  json.RawMessage `json:"state"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	seen := map[string]NeighborEntry{}
	for _, item := range raw {
		ip := strings.TrimSpace(item.Dst)
		if ip == "" {
			continue
		}
		mac := strings.ToLower(strings.TrimSpace(item.LLAddr))
		entry := NeighborEntry{
			IP:     ip,
			IfName: strings.TrimSpace(item.Dev),
			MAC:    mac,
			State:  parseNeighborState(item.State),
			Source: "ip-neigh",
			Vendor: macVendor(mac),
		}
		seen[ip+"|"+entry.IfName] = entry
	}
	out := make([]NeighborEntry, 0, len(seen))
	for _, entry := range seen {
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IfName != out[j].IfName {
			return out[i].IfName < out[j].IfName
		}
		return out[i].IP < out[j].IP
	})
	return out, nil
}

func parseNeighborState(raw json.RawMessage) string {
	var values []string
	if err := json.Unmarshal(raw, &values); err == nil {
		return strings.Join(values, ",")
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}
