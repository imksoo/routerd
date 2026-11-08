// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/miekg/dns"
)

func TestSelftest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "resolver.json")
	if err := os.WriteFile(path, []byte(`{"resource":"lab","spec":{"listen":[{"addresses":["127.0.0.1"],"port":5053}],"sources":[{"name":"default","kind":"upstream","match":["."],"upstreams":["https://1.1.1.1/dns-query"]}]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err := run([]string{"selftest", "--resource", "lab", "--config-file", path}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"resource":"lab"`) || !strings.Contains(out.String(), "https://1.1.1.1/dns-query") {
		t.Fatalf("unexpected selftest output: %s", out.String())
	}
}

func assertZoneARecord(t *testing.T, zones *zoneTable, name, wantIP string) {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(name, dns.TypeA)
	resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("A record missing for %s: ok=%v resp=%v", name, ok, resp)
	}
	if a, isA := resp.Answer[0].(*dns.A); !isA || a.A.String() != wantIP {
		t.Fatalf("A record for %s = %v, want %s", name, resp.Answer[0], wantIP)
	}
}

func assertZonePTRRecord(t *testing.T, zones *zoneTable, name, wantTarget string) {
	t.Helper()
	req := new(dns.Msg)
	req.SetQuestion(name, dns.TypePTR)
	resp, ok := zones.Answer(req, []string{"DNSZone/lan-zone"})
	if !ok || len(resp.Answer) != 1 {
		t.Fatalf("PTR record missing for %s: ok=%v resp=%v", name, ok, resp)
	}
	if ptr, isPTR := resp.Answer[0].(*dns.PTR); !isPTR || ptr.Ptr != wantTarget {
		t.Fatalf("PTR record for %s = %v, want %s", name, resp.Answer[0], wantTarget)
	}
}

func skipIfListenNotPermitted(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, os.ErrPermission) || errors.Is(err, syscall.EPERM) {
		t.Skipf("network listen is not permitted in this environment: %v", err)
	}
}
