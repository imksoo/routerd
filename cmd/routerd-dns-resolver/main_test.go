// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"os"
	"syscall"
	"testing"

	"github.com/miekg/dns"
)

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
