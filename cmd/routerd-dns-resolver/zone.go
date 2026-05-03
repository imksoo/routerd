package main

import (
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"routerd/pkg/api"
	"routerd/pkg/dnsresolver"
)

type zoneTable struct {
	mu    sync.RWMutex
	zones map[string]*zoneData
}

type zoneData struct {
	ResourceName string
	Name         string
	TTL          uint32
	DHCP         api.DNSZoneDHCPDerivedSpec
	DNSSEC       api.DNSZoneDNSSECSpec
	Records      map[string]zoneRecord
	PTR          map[string]string
	ReverseZone  map[string]bool
}

type zoneRecord struct {
	Hostname string
	IPv4     []string
	IPv6     []string
	TTL      uint32
	Dynamic  bool
}

type dhcpLeaseEvent struct {
	Action   string `json:"action"`
	MAC      string `json:"mac,omitempty"`
	IP       string `json:"ip"`
	Hostname string `json:"hostname,omitempty"`
}

func newZoneTable(zones []dnsresolver.RuntimeZone) *zoneTable {
	table := &zoneTable{zones: map[string]*zoneData{}}
	for _, runtime := range zones {
		spec := runtime.Spec
		name := dns.Fqdn(firstNonEmpty(spec.Zone, runtime.Name))
		ttl := uint32(spec.TTL)
		if ttl == 0 {
			ttl = 300
		}
		zone := &zoneData{
			ResourceName: strings.TrimSpace(runtime.Name),
			Name:         name,
			TTL:          ttl,
			DHCP:         spec.DHCPDerived,
			DNSSEC:       spec.DNSSEC,
			Records:      map[string]zoneRecord{},
			PTR:          map[string]string{},
			ReverseZone:  map[string]bool{},
		}
		for _, reverse := range spec.ReverseZones {
			zone.ReverseZone[dns.Fqdn(reverse.Name)] = true
		}
		for _, record := range spec.Records {
			zone.addRecord(record.Hostname, record.IPv4, record.IPv6, uint32(record.TTL), false)
		}
		if spec.DHCPDerived.LeaseFile != "" {
			zone.loadDnsmasqLeases(spec.DHCPDerived.LeaseFile)
		}
		table.zones[name] = zone
	}
	return table
}

func (t *zoneTable) ZoneCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.zones)
}

func (t *zoneTable) Answer(req *dns.Msg, refs []string) (*dns.Msg, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(req.Question) == 0 {
		return nil, false
	}
	q := req.Question[0]
	for _, zone := range t.selectedZones(refs) {
		if msg, ok := zone.answer(req, q); ok {
			return msg, true
		}
	}
	return nil, false
}

func (t *zoneTable) selectedZones(refs []string) []*zoneData {
	if len(refs) == 0 {
		out := make([]*zoneData, 0, len(t.zones))
		for _, zone := range t.zones {
			out = append(out, zone)
		}
		return out
	}
	var out []*zoneData
	for _, ref := range refs {
		name := refName(ref)
		for _, zone := range t.zones {
			if zone.matchesRef(ref, name) {
				out = append(out, zone)
			}
		}
	}
	return out
}

func (z *zoneData) matchesRef(ref, name string) bool {
	zoneName := strings.TrimSuffix(z.Name, ".")
	refName := strings.TrimSuffix(name, ".")
	rawRef := strings.TrimSuffix(ref, ".")
	resourceName := strings.TrimSuffix(z.ResourceName, ".")
	return zoneName == refName || zoneName == rawRef || resourceName == refName || rawRef == "DNSZone/"+resourceName
}

func (t *zoneTable) ApplyLease(lease dhcpLeaseEvent) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, zone := range t.zones {
		if len(zone.DHCP.Sources) == 0 {
			continue
		}
		switch lease.Action {
		case "del", "remove", "removed":
			zone.deleteDynamic(lease.IP)
		default:
			zone.addLease(lease)
		}
	}
}

func (z *zoneData) answer(req *dns.Msg, q dns.Question) (*dns.Msg, bool) {
	name := dns.Fqdn(strings.ToLower(q.Name))
	if strings.HasSuffix(name, z.Name) {
		msg := new(dns.Msg)
		msg.SetReply(req)
		record := z.Records[name]
		switch q.Qtype {
		case dns.TypeA:
			for _, ip := range record.IPv4 {
				msg.Answer = append(msg.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: record.TTL}, A: net.ParseIP(ip)})
			}
		case dns.TypeAAAA:
			for _, ip := range record.IPv6 {
				msg.Answer = append(msg.Answer, &dns.AAAA{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: record.TTL}, AAAA: net.ParseIP(ip)})
			}
		}
		if len(msg.Answer) > 0 {
			if z.DNSSEC.Enabled {
				msg.AuthenticatedData = true
			}
			return msg, true
		}
		msg.SetRcode(req, dns.RcodeNameError)
		if z.DNSSEC.Enabled {
			msg.AuthenticatedData = true
		}
		return msg, true
	}
	if strings.HasSuffix(name, "in-addr.arpa.") || strings.HasSuffix(name, "ip6.arpa.") {
		target := z.PTR[name]
		if target == "" {
			return nil, false
		}
		msg := new(dns.Msg)
		msg.SetReply(req)
		msg.Answer = append(msg.Answer, &dns.PTR{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: z.TTL}, Ptr: target})
		if z.DNSSEC.Enabled {
			msg.AuthenticatedData = true
		}
		return msg, true
	}
	return nil, false
}

func (z *zoneData) addRecord(hostname, ipv4, ipv6 string, ttl uint32, dynamic bool) {
	if hostname == "" {
		return
	}
	if ttl == 0 {
		ttl = z.TTL
	}
	fqdn := dns.Fqdn(hostname)
	if !strings.HasSuffix(fqdn, z.Name) {
		fqdn = dns.Fqdn(strings.TrimSuffix(hostname, ".") + "." + strings.TrimSuffix(z.Name, "."))
	}
	record := zoneRecord{Hostname: fqdn, TTL: ttl, Dynamic: dynamic}
	if ipv4 != "" {
		record.IPv4 = append(record.IPv4, ipv4)
		if ptr, err := dns.ReverseAddr(ipv4); err == nil {
			z.PTR[dns.Fqdn(ptr)] = fqdn
		}
	}
	if ipv6 != "" {
		record.IPv6 = append(record.IPv6, ipv6)
		if ptr, err := dns.ReverseAddr(ipv6); err == nil {
			z.PTR[dns.Fqdn(ptr)] = fqdn
		}
	}
	z.Records[strings.ToLower(fqdn)] = record
}

func (z *zoneData) addLease(lease dhcpLeaseEvent) {
	if lease.IP == "" || lease.Hostname == "" {
		return
	}
	ttl := uint32(z.DHCP.TTL)
	if ttl == 0 {
		ttl = 60
	}
	ip := net.ParseIP(lease.IP)
	if ip == nil {
		return
	}
	if ip.To4() != nil {
		z.addRecord(lease.Hostname, lease.IP, "", ttl, true)
		return
	}
	z.addRecord(lease.Hostname, "", lease.IP, ttl, true)
}

func (z *zoneData) deleteDynamic(ip string) {
	for name, record := range z.Records {
		if !record.Dynamic {
			continue
		}
		if contains(record.IPv4, ip) || contains(record.IPv6, ip) {
			delete(z.Records, name)
		}
	}
	if ptr, err := dns.ReverseAddr(ip); err == nil {
		delete(z.PTR, dns.Fqdn(ptr))
	}
}

func (z *zoneData) loadDnsmasqLeases(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 || fields[3] == "*" {
			continue
		}
		z.addLease(dhcpLeaseEvent{Action: "add", MAC: fields[1], IP: fields[2], Hostname: fields[3]})
	}
}

func sourceMatches(matches []string, qname string) bool {
	if len(matches) == 0 {
		return true
	}
	name := strings.TrimSuffix(strings.ToLower(qname), ".")
	for _, match := range matches {
		m := strings.TrimSuffix(strings.ToLower(match), ".")
		if m == "" || m == "." || name == m || strings.HasSuffix(name, "."+m) {
			return true
		}
	}
	return false
}

func dnsCacheKey(localAddr string, req *dns.Msg) string {
	if len(req.Question) == 0 {
		return localAddr
	}
	q := req.Question[0]
	return strings.ToLower(localAddr + "|" + q.Name + "|" + dns.TypeToString[q.Qtype])
}

func dnsMessageTTL(msg *dns.Msg, spec api.DNSResolverCacheSpec) time.Duration {
	ttl := uint32(60)
	if len(msg.Answer) > 0 {
		ttl = msg.Answer[0].Header().Ttl
	}
	out := time.Duration(ttl) * time.Second
	if spec.MinTTL != "" {
		if min, err := time.ParseDuration(spec.MinTTL); err == nil && out < min {
			out = min
		}
	}
	if spec.MaxTTL != "" {
		if max, err := time.ParseDuration(spec.MaxTTL); err == nil && out > max {
			out = max
		}
	}
	if len(msg.Answer) == 0 && spec.NegativeTTL != "" {
		if negative, err := time.ParseDuration(spec.NegativeTTL); err == nil {
			out = negative
		}
	}
	return out
}

func refName(ref string) string {
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		return ref[i+1:]
	}
	return ref
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
