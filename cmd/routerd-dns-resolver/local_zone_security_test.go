// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dnsresolver"
	"github.com/miekg/dns"
)

func TestLocalZoneNeverFabricatesAuthenticatedData(t *testing.T) {
	config := dnsresolver.RuntimeConfig{Resource: "fixture", Spec: dnsresolver.NormalizeSpec(api.DNSResolverSpec{Sources: []api.DNSResolverSourceSpec{{Kind: "zone", ZoneRef: []string{"DNSZone/local"}}}}), Zones: []dnsresolver.RuntimeZone{{Name: "local", Spec: api.DNSZoneSpec{Zone: "example.test", DNSSEC: api.DNSZoneDNSSECSpec{Enabled: true}, Records: []api.DNSZoneRecordSpec{{Hostname: "router", IPv4: "192.0.2.1"}}}}}}
	for _, question := range []struct {
		name string
		kind uint16
	}{{"router.example.test.", dns.TypeA}, {"router.example.test.", dns.TypeAAAA}, {"absent.example.test.", dns.TypeA}, {"1.2.0.192.in-addr.arpa.", dns.TypePTR}} {
		query := new(dns.Msg)
		query.SetQuestion(question.name, question.kind)
		response, ok := newZoneTable(config.Zones).Answer(query, nil)
		if !ok || response.AuthenticatedData {
			t.Fatalf("unvalidated zone answer has AD: %v, %+v", ok, response)
		}
	}
	path := filepath.Join(t.TempDir(), "resolver.json")
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(options{resource: "fixture", configFile: path}); err == nil {
		t.Fatal("worker accepted unsupported local DNSSEC")
	}
	if _, err := newDaemon(options{resource: "fixture"}, config); err == nil {
		t.Fatal("daemon constructor accepted unsupported local DNSSEC")
	}
}
