// SPDX-License-Identifier: BSD-3-Clause

package platform

import (
	"testing"
)

func TestDnsmasqLeaseCandidatesPreferActualManagedPath(t *testing.T) {
	defaults := Defaults{RuntimeDir: "/run/routerd", StateDir: "/var/lib/routerd"}
	if got := DnsmasqLeaseCandidates(defaults); got[0] != "/var/lib/routerd/dnsmasq/dnsmasq.leases" {
		t.Fatalf("systemd candidates = %#v, want state path first", got)
	}
	freebsd := Defaults{RuntimeDir: "/var/run/routerd", StateDir: "/var/db/routerd"}
	got := DnsmasqLeaseCandidates(freebsd)
	if got[0] != "/var/db/routerd/dnsmasq/dnsmasq.leases" {
		t.Fatalf("freebsd candidates = %#v, want state path first", got)
	}
}
