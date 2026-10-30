// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/miekg/dns"
	"net"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestWildcardTCPListenerCannotBypassSelectedSources(t *testing.T) {
	port := freeTCPPort(t)
	cfg := testResolverConfig([]int{port})
	cfg.Spec.Listen[0].Addresses = []string{"0.0.0.0"}
	cfg.Spec.Listen[0].Sources = []string{"public"}
	cfg.Spec.Sources = append(cfg.Spec.Sources, api.DNSResolverSourceSpec{Name: "public", Kind: "zone", Match: []string{"public.example"}, ZoneRef: []string{"DNSZone/public"}})
	public := cfg.Zones[0]
	public.Name = "public"
	public.Spec.Zone = "public.example"
	cfg.Zones = append(cfg.Zones, public)
	d := newTestDaemon(t, filepath.Join(t.TempDir(), "resolver.json"), cfg, false)
	if err := d.startDNS(t.Context()); err != nil {
		t.Fatal(err)
	}
	query := new(dns.Msg)
	query.SetQuestion("router.lab.example.", dns.TypeA)
	answer, _, err := (&dns.Client{Net: "tcp", Timeout: time.Second}).Exchange(query, net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	if len(answer.Answer) > 0 {
		t.Fatalf("wildcard listener selected only public source but served private source: %v", answer.Answer)
	}
}
