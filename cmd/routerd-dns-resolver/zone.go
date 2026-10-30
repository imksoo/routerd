// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dnsresolver"
)

type zoneTable struct {
	mu      sync.RWMutex
	zones   map[string]*zoneData
	loadErr error
}

type zoneData struct {
	ResourceName  string
	Name          string
	TTL           uint32
	DHCP          api.DNSZoneDHCPDerivedSpec
	DHCPScopes    []dnsresolver.DHCPAddressScope
	LeaseObserved map[leaseAddress]time.Time
	Records       map[string]zoneRecord
	PTR           map[string]string
	ReverseZone   map[string]bool
}

type leaseAddress struct{ Source, IP string }

type zoneRecord struct {
	Hostname         string
	IPv4             []string
	IPv6             []string
	TTL              uint32
	Source           zoneRecordSource
	LeaseSource      string
	AssignmentOrigin string
	MAC              string
	ObservedAt       time.Time
	ExpiresAt        time.Time
	Infinite         bool
}

type zoneRecordSource uint8

const (
	// Numeric order is the reload merge priority. Do not reorder without
	// updating CopyDynamicFrom's precedence contract.
	zoneRecordSourceOldSticky       zoneRecordSource = 10
	zoneRecordSourceStickySnapshot  zoneRecordSource = 20
	zoneRecordSourceActiveEvent     zoneRecordSource = 30
	zoneRecordSourceActiveLeaseFile zoneRecordSource = 40
	zoneRecordSourceReservation     zoneRecordSource = 46
	zoneRecordSourceStatic          zoneRecordSource = 50

	// A newly received event is newer than the lease file loaded into the
	// current table, but becomes a retained event below the newly read lease
	// file when it crosses a reload boundary.
	zoneRecordPriorityLiveActiveEvent zoneRecordSource = 45
)

type dhcpLeaseEvent = daemonapi.DHCPLeaseUpdate

func newZoneTable(zones []dnsresolver.RuntimeZone) *zoneTable {
	table := &zoneTable{zones: map[string]*zoneData{}}
	for _, runtime := range zones {
		spec := runtime.Spec
		name := strings.ToLower(dns.Fqdn(stringutil.FirstNonEmpty(spec.Zone, runtime.Name)))
		ttl := uint32(spec.TTL)
		if ttl == 0 {
			ttl = 300
		}
		zone := &zoneData{
			ResourceName:  strings.TrimSpace(runtime.Name),
			Name:          name,
			TTL:           ttl,
			DHCP:          spec.DHCPDerived,
			DHCPScopes:    slices.Clone(runtime.DHCPScopes),
			LeaseObserved: map[leaseAddress]time.Time{},
			Records:       map[string]zoneRecord{},
			PTR:           map[string]string{},
			ReverseZone:   map[string]bool{},
		}
		for _, reverse := range spec.ReverseZones {
			zone.ReverseZone[dns.Fqdn(reverse.Name)] = true
		}
		for _, record := range spec.Records {
			zone.addRecord(record.Hostname, record.IPv4, record.IPv6, uint32(record.TTL), zoneRecordSourceStatic)
		}
		zone.rebuildStaticPTR(spec.Records)
		if spec.DHCPDerived.LeaseFile != "" {
			table.loadErr = errors.Join(table.loadErr, zone.loadDnsmasqLeases(spec.DHCPDerived.LeaseFile))
		}
		for _, record := range runtime.DHCPHostRecords {
			zone.addRecoveredLease(record)
		}
		table.zones[zone.ResourceName] = zone
	}
	return table
}

func (t *zoneTable) LoadError() error { return t.loadErr }

func (t *zoneTable) ZoneCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.zones)
}

func (t *zoneTable) CopyDynamicFrom(old *zoneTable) {
	if old == nil {
		return
	}
	type dynamicZone struct {
		apex, resource string
		dhcp           api.DNSZoneDHCPDerivedSpec
		records        []zoneRecord
		observations   map[leaseAddress]time.Time
	}
	var retained []dynamicZone
	old.mu.RLock()
	for _, zone := range old.zones {
		item := dynamicZone{apex: zone.Name, resource: zone.ResourceName, dhcp: zone.DHCP, observations: make(map[leaseAddress]time.Time, len(zone.LeaseObserved))}
		for key, at := range zone.LeaseObserved {
			item.observations[key] = at
		}
		for _, record := range zone.Records {
			if !record.live(time.Now().UTC()) {
				continue
			}
			switch record.Source {
			case zoneRecordSourceActiveEvent:
				item.records = append(item.records, cloneZoneRecord(record))
			case zoneRecordSourceStickySnapshot, zoneRecordSourceOldSticky:
				cloned := cloneZoneRecord(record)
				cloned.Source = zoneRecordSourceOldSticky
				item.records = append(item.records, cloned)
			}
		}
		retained = append(retained, item)
	}
	old.mu.RUnlock()

	t.mu.Lock()
	defer t.mu.Unlock()
	for _, item := range retained {
		zone := t.zones[item.resource]
		if zone == nil || zone.Name != item.apex || !sameDHCPNamespace(zone.DHCP, item.dhcp) {
			continue
		}
		for key, at := range item.observations {
			if zone.LeaseObserved[key].Before(at) {
				zone.LeaseObserved[key] = at
			}
		}
		ttl := uint32(zone.DHCP.TTL)
		if ttl == 0 {
			ttl = 60
		}
		for _, record := range item.records {
			record.TTL = ttl
			zone.mergeRecord(record)
		}
	}
}

// Retained DHCP observations belong to the same declared source and zone,
// rather than merely to a zone with the same DNS suffix after reconfiguration.
func sameDHCPNamespace(left, right api.DNSZoneDHCPDerivedSpec) bool {
	if len(left.Sources) == 0 || len(right.Sources) == 0 || left.LeaseFile != right.LeaseFile || left.HostnameSuffix != right.HostnameSuffix || left.DDNS != right.DDNS {
		return false
	}
	leftSources, rightSources := slices.Clone(left.Sources), slices.Clone(right.Sources)
	slices.Sort(leftSources)
	slices.Sort(rightSources)
	return slices.Equal(slices.Compact(leftSources), slices.Compact(rightSources))
}

func cloneZoneRecord(record zoneRecord) zoneRecord {
	record.IPv4 = append([]string(nil), record.IPv4...)
	record.IPv6 = append([]string(nil), record.IPv6...)
	return record
}

func (t *zoneTable) Answer(req *dns.Msg, refs []string) (*dns.Msg, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now().UTC()
	for _, zone := range t.zones {
		for name, record := range zone.Records {
			if !record.live(now) {
				zone.removeRecord(name)
			}
		}
	}
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
		// ZoneRef identifies a resource, never a DNS suffix that might be
		// owned by an unrelated zone with the same spelling.
		name := strings.TrimPrefix(strings.TrimSpace(ref), "DNSZone/")
		for _, zone := range t.zones {
			if zone.ResourceName != "" && zone.ResourceName == name {
				out = append(out, zone)
			}
		}
	}
	return out
}

func (t *zoneTable) ApplyLease(lease dhcpLeaseEvent) error {
	if err := lease.Validate(time.Now().UTC()); err != nil {
		return err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, zone := range t.zones {
		if !slices.Contains(zone.DHCP.Sources, lease.Source) {
			continue
		}
		key := leaseAddress{Source: lease.Source, IP: lease.IP}
		if previous := zone.LeaseObserved[key]; !previous.IsZero() && !lease.ObservedAt.After(previous) {
			continue
		}
		zone.LeaseObserved[key] = lease.ObservedAt
		switch lease.Action {
		case daemonapi.DHCPLeaseActionRemoved:
			zone.deleteLease(lease)
		default:
			if !lease.Infinite && !time.Now().UTC().Before(lease.ExpiresAt) {
				zone.deleteLease(lease)
				continue
			}
			zone.addLeaseWithPriority(lease, zoneRecordSourceActiveEvent, zoneRecordPriorityLiveActiveEvent)
		}
	}
	return nil
}

func (record zoneRecord) live(now time.Time) bool {
	return record.Source == zoneRecordSourceStatic || record.AssignmentOrigin == dnsresolver.DHCPAssignmentReservation || record.Infinite || !record.ExpiresAt.IsZero() && now.Before(record.ExpiresAt)
}

func (record zoneRecord) ttl(now time.Time) uint32 {
	if record.ExpiresAt.IsZero() {
		return record.TTL
	}
	remaining := record.ExpiresAt.Sub(now) / time.Second
	if remaining < 0 {
		return 0
	}
	return min(record.TTL, uint32(remaining))
}

func (z *zoneData) answer(req *dns.Msg, q dns.Question) (*dns.Msg, bool) {
	name := dns.Fqdn(strings.ToLower(q.Name))
	if dns.IsSubDomain(z.Name, name) {
		msg := new(dns.Msg)
		msg.SetReply(req)
		record, ok := z.Records[name]
		if !ok {
			// RFC 4592 style wildcard: with no exact match, walk up toward the
			// zone apex looking for a "*" record at the closest enclosing label.
			for rest := name; ; {
				next, end := dns.NextLabel(rest, 0)
				if end {
					break
				}
				rest = rest[next:]
				if len(rest) < len(z.Name) {
					break
				}
				if wc, found := z.Records["*."+rest]; found {
					record = wc
					ok = true
					break
				}
				if rest == z.Name {
					break
				}
			}
		}
		switch q.Qtype {
		case dns.TypeA:
			for _, ip := range record.IPv4 {
				msg.Answer = append(msg.Answer, &dns.A{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: record.ttl(time.Now().UTC())}, A: net.ParseIP(ip)})
			}
		case dns.TypeAAAA:
			for _, ip := range record.IPv6 {
				msg.Answer = append(msg.Answer, &dns.AAAA{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: record.ttl(time.Now().UTC())}, AAAA: net.ParseIP(ip)})
			}
		}
		if len(msg.Answer) > 0 {
			return msg, true
		}
		if !ok {
			// The name does not exist anywhere in this authoritative zone.
			msg.SetRcode(req, dns.RcodeNameError)
		}
		// Otherwise the name exists but has no record of the queried type
		// (NODATA): keep NOERROR with an empty answer so a dual-stack client
		// does not treat the whole name as nonexistent (e.g. an AAAA query for
		// an A-only record must not poison the A lookup via negative caching).
		return msg, true
	}
	if strings.HasSuffix(name, "in-addr.arpa.") || strings.HasSuffix(name, "ip6.arpa.") {
		target := z.PTR[name]
		if target == "" {
			return nil, false
		}
		msg := new(dns.Msg)
		msg.SetReply(req)
		record := z.Records[strings.ToLower(dns.Fqdn(target))]
		ttl := record.ttl(time.Now().UTC())
		if record.Source == zoneRecordSourceStatic {
			ttl = z.TTL
		}
		msg.Answer = append(msg.Answer, &dns.PTR{Hdr: dns.RR_Header{Name: q.Name, Rrtype: dns.TypePTR, Class: dns.ClassINET, Ttl: ttl}, Ptr: target})
		return msg, true
	}
	return nil, false
}

func (z *zoneData) addRecord(hostname, ipv4, ipv6 string, ttl uint32, source zoneRecordSource) {
	z.addRecordWithPriority(hostname, ipv4, ipv6, ttl, source, source)
}

func (z *zoneData) addRecordWithPriority(hostname, ipv4, ipv6 string, ttl uint32, source, priority zoneRecordSource) {
	if hostname == "" {
		return
	}
	if ttl == 0 {
		ttl = z.TTL
	}
	record := zoneRecord{Hostname: z.recordName(hostname), TTL: ttl, Source: source}
	if ipv4 != "" {
		record.IPv4 = append(record.IPv4, ipv4)
	}
	if ipv6 != "" {
		record.IPv6 = append(record.IPv6, ipv6)
	}
	z.mergeRecordWithPriority(record, priority)
}

func (z *zoneData) recordName(hostname string) string {
	fqdn := dns.Fqdn(hostname)
	if !dns.IsSubDomain(z.Name, fqdn) {
		fqdn = dns.Fqdn(strings.TrimSuffix(hostname, ".") + "." + strings.TrimSuffix(z.Name, "."))
	}
	return fqdn
}

func (z *zoneData) rebuildStaticPTR(records []api.DNSZoneRecordSpec) {
	// Repeated hostnames retain their existing last-record-wins behavior.
	// Rebuild after all replacements so a surviving alias still has a PTR
	// when the original PTR owner was moved to another address.
	z.PTR = map[string]string{}
	for _, declared := range records {
		record := z.Records[strings.ToLower(z.recordName(declared.Hostname))]
		for _, ip := range []string{declared.IPv4, declared.IPv6} {
			if !contains(record.IPv4, ip) && !contains(record.IPv6, ip) {
				continue
			}
			if ptr, err := dns.ReverseAddr(ip); err == nil && z.PTR[dns.Fqdn(ptr)] == "" {
				z.PTR[dns.Fqdn(ptr)] = record.Hostname
			}
		}
	}
}

func (z *zoneData) addLease(lease dhcpLeaseEvent, source zoneRecordSource) {
	z.addLeaseWithPriority(lease, source, source)
}

func (z *zoneData) addLeaseWithPriority(lease dhcpLeaseEvent, source, priority zoneRecordSource) {
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
	record := zoneRecord{Hostname: z.recordName(lease.Hostname), TTL: ttl, Source: source, LeaseSource: lease.Source, MAC: lease.MAC, ObservedAt: lease.ObservedAt, ExpiresAt: lease.ExpiresAt, Infinite: lease.Infinite}
	if ip.To4() != nil {
		record.IPv4 = []string{lease.IP}
	} else {
		record.IPv6 = []string{lease.IP}
	}
	if existing, ok := z.Records[strings.ToLower(dns.Fqdn(record.Hostname))]; ok && existing.ObservedAt.After(record.ObservedAt) {
		return
	}
	z.mergeRecordWithPriority(record, priority)
}

func (z *zoneData) addRecoveredLease(record dnsresolver.RuntimeDHCPHostRecord) {
	if record.Hostname == "" || record.MAC == "" || !slices.Contains(z.DHCP.Sources, record.Source) {
		return
	}
	ip := net.ParseIP(record.IP)
	if ip == nil || !dnsresolver.DHCPSourceKind(record.Source, ip.To4() == nil) {
		return
	}
	source := zoneRecordSourceStickySnapshot
	switch record.Origin {
	case dnsresolver.DHCPAssignmentReservation:
		if !record.ExpiresAt.IsZero() {
			return
		}
		source = zoneRecordSourceReservation
	case dnsresolver.DHCPAssignmentHeld:
		if record.ExpiresAt.IsZero() || !time.Now().Before(record.ExpiresAt) {
			return
		}
	default:
		return
	}
	ttl := uint32(z.DHCP.TTL)
	if ttl == 0 {
		ttl = 60
	}
	value := zoneRecord{Hostname: z.recordName(record.Hostname), TTL: ttl, Source: source, LeaseSource: record.Source, MAC: record.MAC, ExpiresAt: record.ExpiresAt, AssignmentOrigin: record.Origin}
	if ip.To4() != nil {
		value.IPv4 = []string{record.IP}
	} else {
		value.IPv6 = []string{record.IP}
	}
	z.mergeRecord(value)
}

func (z *zoneData) mergeRecord(record zoneRecord) {
	z.mergeRecordWithPriority(record, record.Source)
}

func (z *zoneData) mergeRecordWithPriority(record zoneRecord, priority zoneRecordSource) {
	if record.Hostname == "" {
		return
	}
	record.Hostname = dns.Fqdn(record.Hostname)
	name := strings.ToLower(record.Hostname)
	conflicts := map[string]zoneRecord{}
	sharedStaticPTR := map[string]bool{}
	if existing, exists := z.Records[name]; exists {
		if existing.Source > priority ||
			(existing.Source == priority && record.Source == zoneRecordSourceStickySnapshot) {
			return
		}
		conflicts[name] = existing
	}
	for _, ip := range append(append([]string(nil), record.IPv4...), record.IPv6...) {
		ptr, err := dns.ReverseAddr(ip)
		if err != nil {
			continue
		}
		target := z.PTR[dns.Fqdn(ptr)]
		if target == "" || strings.EqualFold(target, record.Hostname) {
			continue
		}
		existingName := strings.ToLower(dns.Fqdn(target))
		existing, exists := z.Records[existingName]
		if exists && existing.Source == zoneRecordSourceStatic && record.Source == zoneRecordSourceStatic {
			// Explicit static names may share an address. Keep both forward
			// records, with the first declared static name owning the PTR.
			sharedStaticPTR[dns.Fqdn(ptr)] = true
			continue
		}
		if !exists || existing.Source >= priority {
			return
		}
		conflicts[existingName] = existing
	}
	for conflictName := range conflicts {
		z.removeRecord(conflictName)
	}
	z.Records[name] = record
	for _, ip := range append(append([]string(nil), record.IPv4...), record.IPv6...) {
		if ptr, err := dns.ReverseAddr(ip); err == nil {
			if !sharedStaticPTR[dns.Fqdn(ptr)] {
				z.PTR[dns.Fqdn(ptr)] = record.Hostname
			}
		}
	}
}

func (z *zoneData) removeRecord(name string) {
	record, exists := z.Records[name]
	if !exists {
		return
	}
	delete(z.Records, name)
	for _, ip := range append(append([]string(nil), record.IPv4...), record.IPv6...) {
		ptr, err := dns.ReverseAddr(ip)
		if err != nil {
			continue
		}
		ptrName := dns.Fqdn(ptr)
		if strings.EqualFold(z.PTR[ptrName], record.Hostname) {
			delete(z.PTR, ptrName)
		}
	}
}

func (z *zoneData) deleteDynamic(ip string) {
	for name, record := range z.Records {
		if record.Source == zoneRecordSourceStatic {
			continue
		}
		if contains(record.IPv4, ip) || contains(record.IPv6, ip) {
			delete(z.Records, name)
		}
	}
	if ptr, err := dns.ReverseAddr(ip); err == nil {
		ptrName := dns.Fqdn(ptr)
		if target := z.PTR[ptrName]; target != "" {
			if record, exists := z.Records[strings.ToLower(dns.Fqdn(target))]; exists && record.Source == zoneRecordSourceStatic {
				return
			}
		}
		delete(z.PTR, ptrName)
	}
}

func (z *zoneData) deleteLease(lease dhcpLeaseEvent) {
	for name, record := range z.Records {
		if record.Source == zoneRecordSourceStatic || record.AssignmentOrigin != "" || record.LeaseSource != lease.Source || !strings.EqualFold(record.MAC, lease.MAC) || record.ObservedAt.After(lease.ObservedAt) {
			continue
		}
		if contains(record.IPv4, lease.IP) || contains(record.IPv6, lease.IP) {
			z.removeRecord(name)
		}
	}
}

func (z *zoneData) loadDnsmasqLeases(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read DHCP lease file: %w", err)
	}
	defer file.Close()
	const maxLeaseFile = 8 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxLeaseFile+1))
	if err != nil {
		return err
	}
	if len(data) > maxLeaseFile {
		return fmt.Errorf("DHCP lease file exceeds %d bytes", maxLeaseFile)
	}
	now := time.Now().UTC()
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 || fields[0] == "duid" {
			continue
		}
		if len(fields) < 4 {
			return fmt.Errorf("malformed DHCP lease file row")
		}
		deadline, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil || deadline < 0 {
			return fmt.Errorf("invalid DHCP lease expiry %q", fields[0])
		}
		ip := net.ParseIP(fields[2])
		if ip == nil {
			return fmt.Errorf("invalid DHCP lease address %q", fields[2])
		}
		if fields[3] == "*" || deadline != 0 && deadline <= now.Unix() {
			continue
		}
		source := dnsresolver.DHCPSourceForAddress(z.DHCPScopes, fields[2])
		if source == "" || !slices.Contains(z.DHCP.Sources, source) {
			continue
		}
		// IPv6 rows carry IAID in column two and the client DUID in column five.
		client := fields[1]
		if ip.To4() == nil {
			if len(fields) < 5 || fields[4] == "*" {
				return fmt.Errorf("IPv6 DHCP lease has no client DUID")
			}
			client = fields[4]
		}
		lease := dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, Source: source, MAC: client, IP: fields[2], Hostname: fields[3], ObservedAt: now, Infinite: deadline == 0}
		if deadline != 0 {
			lease.ExpiresAt = time.Unix(deadline, 0).UTC()
		}
		if err := lease.Validate(now); err != nil {
			return err
		}
		z.addLease(lease, zoneRecordSourceActiveLeaseFile)
		z.LeaseObserved[leaseAddress{Source: lease.Source, IP: lease.IP}] = now
	}
	return nil
}

func sourceMatches(matches []string, qname string) bool {
	if len(matches) == 0 {
		return true
	}
	name := dns.Fqdn(qname)
	for _, match := range matches {
		if match == "" || dns.IsSubDomain(dns.Fqdn(match), name) {
			return true
		}
	}
	return false
}

func dnsCacheKey(localAddr string, req *dns.Msg) string {
	if len(req.Question) != 1 || req.Opcode != dns.OpcodeQuery || req.Response || req.IsTsig() != nil {
		return ""
	}
	query := req.Copy()
	query.Id = 0
	query.Question[0].Name = strings.ToLower(query.Question[0].Name)
	wire, err := query.Pack()
	if err != nil {
		return ""
	}
	// Keep class, header flags and all EDNS parameters in the cache identity.
	return localAddr + "\x00" + string(wire)
}

func dnsMessageTTL(msg *dns.Msg, spec api.DNSResolverCacheSpec) time.Duration {
	// Transient failures are not negative answers about a name's existence.
	// In particular, never retain a downstream resolver's SERVFAIL using the
	// configured NXDOMAIN/NODATA lifetime.
	if msg == nil || msg.Truncated || msg.IsTsig() != nil || (msg.Rcode != dns.RcodeSuccess && msg.Rcode != dns.RcodeNameError) {
		return 0
	}
	ttl := ^uint32(0)
	negative := msg.Rcode == dns.RcodeNameError || len(msg.Answer) == 0
	hasSOA := false
	for _, section := range [][]dns.RR{msg.Answer, msg.Ns, msg.Extra} {
		for _, rr := range section {
			if rr.Header().Rrtype == dns.TypeOPT {
				continue
			}
			ttl = min(ttl, rr.Header().Ttl)
			if soa, ok := rr.(*dns.SOA); ok && negative {
				hasSOA = true
				ttl = min(ttl, soa.Minttl)
			}
		}
	}
	if ttl == ^uint32(0) || (negative && !hasSOA) {
		return 0
	}
	out := time.Duration(ttl) * time.Second
	if spec.MinTTL != "" {
		if min, err := time.ParseDuration(spec.MinTTL); err == nil && out < min {
			// A minimum cache lifetime cannot extend an authoritative TTL.
			return 0
		}
	}
	if spec.MaxTTL != "" {
		if max, err := time.ParseDuration(spec.MaxTTL); err == nil && out > max {
			out = max
		}
	}
	if negative && spec.NegativeTTL != "" {
		if negative, err := time.ParseDuration(spec.NegativeTTL); err == nil {
			out = min(out, negative)
		}
	}
	return out
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
