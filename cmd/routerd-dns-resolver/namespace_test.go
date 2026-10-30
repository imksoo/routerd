// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/miekg/dns"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	resolvercfg "github.com/imksoo/routerd/pkg/dnsresolver"
)

func namespaceQuery(t *testing.T, address, name string) *dns.Msg {
	t.Helper()
	query := new(dns.Msg)
	query.SetQuestion(name, dns.TypeA)
	response, _, err := (&dns.Client{Net: "udp", Timeout: time.Second}).ExchangeContext(t.Context(), query, address)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestResolverNamespaceUsesDNSLabelsAndSelectedZoneRefsOnWire(t *testing.T) {
	upstream := failoverDualDNSServer(t, dns.HandlerFunc(func(w dns.ResponseWriter, query *dns.Msg) {
		if err := w.WriteMsg(failoverSuccessfulReply(query)); err != nil {
			t.Error(err)
		}
	}))
	config := testResolverConfig(nil)
	// An unrestricted zone source must still answer only its own DNS labels
	// and references; unrelated names continue to the configured upstream.
	config.Spec.Sources[0].Match = []string{"."}
	config.Spec.Sources = append(config.Spec.Sources, api.DNSResolverSourceSpec{
		Kind: "forward", Match: []string{"."}, Upstreams: []string{"udp://" + upstream},
	})
	config.Zones = append(config.Zones, resolvercfg.RuntimeZone{Name: "private-zone", Spec: api.DNSZoneSpec{
		Zone: "private.example", Records: []api.DNSZoneRecordSpec{{Hostname: "secret", IPv4: "192.0.2.99"}},
	}})
	d := newTestDaemon(t, "", config, true)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	for _, query := range []struct{ name, want string }{
		{"RoUtEr.LaB.ExAmPlE.", "192.0.2.1"},
		{"notlab.example.", "192.0.2.53"},
		{`router\.lab.example.`, "192.0.2.53"},
		{"secret.private.example.", "192.0.2.53"},
	} {
		response := namespaceQuery(t, address, query.name)
		if response.Rcode != dns.RcodeSuccess || len(response.Answer) != 1 {
			t.Fatalf("%s escaped its configured namespace: %v", query.name, response)
		}
		if answer, ok := response.Answer[0].(*dns.A); !ok || answer.A.String() != query.want {
			t.Fatalf("%s: answer=%v, want %s", query.name, response.Answer, query.want)
		}
	}
}

func TestZoneResourceReferenceCannotSelectUnreferencedSameNamedSuffix(t *testing.T) {
	config := testResolverConfig(nil)
	config.Spec.Sources[0].Match = []string{"."}
	config.Spec.Sources[0].ZoneRef = []string{"DNSZone/selected"}
	config.Zones = []resolvercfg.RuntimeZone{
		{Name: "selected", Spec: api.DNSZoneSpec{Zone: "public.example", Records: []api.DNSZoneRecordSpec{{Hostname: "router", IPv4: "192.0.2.1"}}}},
		{Name: "private", Spec: api.DNSZoneSpec{Zone: "selected", Records: []api.DNSZoneRecordSpec{{Hostname: "secret", IPv4: "192.0.2.99"}}}},
	}
	d := newTestDaemon(t, "", config, true)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	if answer := namespaceQuery(t, address, "router.public.example."); len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.1" {
		t.Fatalf("referenced zone resource did not serve its own suffix: %v", answer)
	}
	if answer := namespaceQuery(t, address, "secret.selected."); answer.Rcode != dns.RcodeNameError || len(answer.Answer) != 0 {
		t.Fatalf("unselected DNSZone/private answered through DNSZone/selected: %v", answer)
	}
}

func TestWildcardTraversalHonorsEscapedDNSLabelOnWire(t *testing.T) {
	config := testResolverConfig(nil)
	config.Zones[0].Spec.Records = []api.DNSZoneRecordSpec{{Hostname: "*.child", IPv4: "192.0.2.99"}}
	d := newTestDaemon(t, "", config, true)
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	if answer := namespaceQuery(t, address, "x.child.lab.example."); len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.99" {
		t.Fatalf("wildcard no longer answers its actual child labels: %v", answer)
	}
	if answer := namespaceQuery(t, address, `x\.child.lab.example.`); answer.Rcode != dns.RcodeNameError || len(answer.Answer) != 0 {
		t.Fatalf("escaped single label was split during wildcard traversal: %v", answer)
	}
}

func TestListenersSelectDistinctZoneResourcesWithTheSameApex(t *testing.T) {
	config := testResolverConfig(nil)
	config.Spec.Sources = []api.DNSResolverSourceSpec{
		{Name: "view-one", Kind: "zone", Match: []string{"."}, ZoneRef: []string{"DNSZone/one"}},
		{Name: "view-two", Kind: "zone", Match: []string{"."}, ZoneRef: []string{"DNSZone/two"}},
	}
	config.Zones = []resolvercfg.RuntimeZone{
		{Name: "one", Spec: api.DNSZoneSpec{Zone: "shared.example", Records: []api.DNSZoneRecordSpec{{Hostname: "router", IPv4: "192.0.2.1"}}}},
		{Name: "two", Spec: api.DNSZoneSpec{Zone: "shared.example", Records: []api.DNSZoneRecordSpec{{Hostname: "router", IPv4: "192.0.2.2"}}}},
	}
	d := newTestDaemon(t, "", config, true)
	addresses := []string{
		failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS)),
		failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS)),
	}
	var listen []api.DNSResolverListenSpec
	for i, address := range addresses {
		host, textPort, err := net.SplitHostPort(address)
		if err != nil {
			t.Fatal(err)
		}
		port, err := strconv.Atoi(textPort)
		if err != nil {
			t.Fatal(err)
		}
		listen = append(listen, api.DNSResolverListenSpec{Addresses: []string{host}, Port: port, Sources: []string{config.Spec.Sources[i].Name}})
	}
	d.stateMu.Lock()
	d.config.Spec.Listen = listen
	d.stateMu.Unlock()
	for i, address := range addresses {
		answer := namespaceQuery(t, address, "router.shared.example.")
		want := net.IPv4(192, 0, 2, byte(i+1)).String()
		if len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != want {
			t.Fatalf("listener %s selected another resource at the same apex: %v, want %s", address, answer, want)
		}
	}
}

func TestWorkerRejectsForeignResourceConfigWithoutAcknowledgingItsDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig(nil)
	writeRuntimeConfig(t, path, initial)
	d := newTestDaemon(t, path, initial, true)
	if _, err := d.reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	oldDigest, oldGeneration := d.configDigest, d.generation
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	for _, resource := range []string{"", "other-worker"} {
		foreign := testResolverConfig(nil)
		foreign.Resource = resource
		foreign.Spec = resolvercfg.NormalizeSpec(foreign.Spec)
		foreign.Zones[0].Spec.Records[0].IPv4 = "192.0.2.99"
		writeRuntimeConfig(t, path, foreign)
		if _, err := loadConfig(d.opts); err == nil {
			t.Fatalf("config resource %q entered worker %q", resource, d.opts.resource)
		}
		if _, err := newDaemon(d.opts, foreign); err == nil {
			t.Fatalf("constructor accepted config resource %q", resource)
		}
		digest, err := foreign.Digest()
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, "/v1/reload", nil)
		request.Header.Set(resolvercfg.ConfigDigestHeader, digest)
		response := httptest.NewRecorder()
		d.routes().ServeHTTP(response, request)
		if response.Code != http.StatusBadRequest || d.configDigest != oldDigest || d.generation != oldGeneration {
			t.Fatalf("foreign configuration was acknowledged: status=%d digest=%s generation=%d", response.Code, d.configDigest, d.generation)
		}
		answer := namespaceQuery(t, address, "router.lab.example.")
		if len(answer.Answer) != 1 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.1" {
			t.Fatalf("rejected configuration replaced the serving namespace: %v", answer)
		}
	}
}

func TestReloadRetainsDHCPObservationsOnlyInTheirDeclaredNamespace(t *testing.T) {
	for _, change := range []string{"DHCP source", "zone resource", "zone apex"} {
		t.Run(change, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "resolver.json")
			initial := testResolverConfig(nil)
			initial.Spec.Sources[0].Match = []string{"."}
			initial.Zones[0].Spec.DHCPDerived.Sources = []string{"DHCPv4Server/old"}
			writeRuntimeConfig(t, path, initial)
			d := newTestDaemon(t, path, initial, true)
			d.zones.ApplyLease(dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, IP: "192.0.2.55", Hostname: "client"})
			address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
			if answer := namespaceQuery(t, address, "client.lab.example."); len(answer.Answer) != 1 {
				t.Fatalf("initial DHCP observation was not served: %v", answer)
			}
			updated := testResolverConfig(nil)
			updated.Spec.Sources[0].Match = []string{"."}
			updated.Zones[0].Spec.DHCPDerived.Sources = []string{"DHCPv4Server/old"}
			if change == "DHCP source" {
				updated.Zones[0].Spec.DHCPDerived.Sources = []string{"DHCPv4Server/new"}
			} else if change == "zone resource" {
				updated.Zones[0].Name = "new-zone"
				updated.Spec.Sources[0].ZoneRef = []string{"DNSZone/new-zone"}
			} else {
				updated.Zones[0].Spec.Zone = "other.example"
			}
			writeRuntimeConfig(t, path, updated)
			if _, err := d.reload(t.Context()); err != nil {
				t.Fatal(err)
			}
			if answer := namespaceQuery(t, address, "client.lab.example."); answer.Rcode != dns.RcodeNameError || len(answer.Answer) != 0 {
				t.Fatalf("DHCP observation crossed %s replacement: %v", change, answer)
			}
			if answer := namespaceQuery(t, address, "router."+updated.Zones[0].Spec.Zone+"."); len(answer.Answer) != 1 {
				t.Fatalf("namespace replacement removed its configured static record: %v", answer)
			}
			ptr := new(dns.Msg)
			ptr.SetQuestion("55.2.0.192.in-addr.arpa.", dns.TypePTR)
			answer, _, err := (&dns.Client{Net: "udp", Timeout: time.Second}).ExchangeContext(t.Context(), ptr, address)
			if err != nil || answer == nil || len(answer.Answer) != 0 {
				t.Fatalf("old DHCP PTR crossed %s replacement: %v, %v", change, answer, err)
			}
		})
	}
}

func TestReloadRetainedDHCPObservationUsesCurrentTTL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver.json")
	initial := testResolverConfig(nil)
	initial.Zones[0].Spec.DHCPDerived.Sources = []string{"DHCPv4Server/one", "DHCPv4Server/two"}
	d := newTestDaemon(t, path, initial, true)
	d.zones.ApplyLease(dhcpLeaseEvent{Action: daemonapi.DHCPLeaseActionAdded, IP: "192.0.2.55", Hostname: "client"})
	updated := testResolverConfig(nil)
	updated.Zones[0].Spec.DHCPDerived.Sources = []string{"DHCPv4Server/two", "DHCPv4Server/one"}
	updated.Zones[0].Spec.DHCPDerived.TTL = 7
	writeRuntimeConfig(t, path, updated)
	if _, err := d.reload(t.Context()); err != nil {
		t.Fatal(err)
	}
	address := failoverDualDNSServer(t, dns.HandlerFunc(d.handleDNS))
	answer := namespaceQuery(t, address, "client.lab.example.")
	if len(answer.Answer) != 1 || answer.Answer[0].Header().Ttl != 7 || answer.Answer[0].(*dns.A).A.String() != "192.0.2.55" {
		t.Fatalf("unchanged DHCP source set lost its observation/current TTL: %v", answer)
	}
}
