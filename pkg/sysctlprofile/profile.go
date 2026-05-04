package sysctlprofile

import (
	"fmt"
	"sort"
)

type Entry struct {
	Key      string
	Value    string
	Compare  string
	Optional bool
}

func Entries(profile string, overrides map[string]string) ([]Entry, error) {
	var entries []Entry
	switch profile {
	case "router-linux":
		entries = routerLinux()
	default:
		return nil, fmt.Errorf("unknown sysctl profile %q", profile)
	}
	for key, value := range overrides {
		found := false
		for i := range entries {
			if entries[i].Key == key {
				entries[i].Value = value
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, Entry{Key: key, Value: value})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
	return entries, nil
}

func routerLinux() []Entry {
	return []Entry{
		{Key: "net.core.netdev_max_backlog", Value: "5000", Compare: "atLeast"},
		{Key: "net.core.rmem_max", Value: "16777216", Compare: "atLeast"},
		{Key: "net.core.somaxconn", Value: "4096", Compare: "atLeast"},
		{Key: "net.core.wmem_max", Value: "16777216", Compare: "atLeast"},
		{Key: "net.ipv4.conf.all.forwarding", Value: "1"},
		{Key: "net.ipv4.conf.all.rp_filter", Value: "0"},
		{Key: "net.ipv4.conf.all.src_valid_mark", Value: "1"},
		{Key: "net.ipv4.conf.default.rp_filter", Value: "0"},
		{Key: "net.ipv4.ip_forward", Value: "1"},
		{Key: "net.ipv4.ip_local_port_range", Value: "1024 65535"},
		{Key: "net.ipv4.tcp_fin_timeout", Value: "30"},
		{Key: "net.ipv4.tcp_rmem", Value: "4096 87380 16777216", Compare: "atLeast"},
		{Key: "net.ipv4.tcp_tw_reuse", Value: "1"},
		{Key: "net.ipv4.tcp_wmem", Value: "4096 65536 16777216", Compare: "atLeast"},
		{Key: "net.ipv6.conf.all.forwarding", Value: "1"},
		{Key: "net.ipv6.conf.default.forwarding", Value: "1"},
		{Key: "net.ipv6.route.max_size", Value: "16384", Compare: "atLeast"},
		{Key: "net.netfilter.nf_conntrack_buckets", Value: "65536", Compare: "atLeast", Optional: true},
		{Key: "net.netfilter.nf_conntrack_max", Value: "262144", Compare: "atLeast", Optional: true},
		{Key: "net.netfilter.nf_conntrack_tcp_timeout_established", Value: "86400", Optional: true},
		{Key: "net.netfilter.nf_conntrack_udp_timeout", Value: "30", Optional: true},
		{Key: "net.netfilter.nf_conntrack_udp_timeout_stream", Value: "180", Optional: true},
	}
}
