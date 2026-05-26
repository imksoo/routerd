// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/logstore"
)

func TestIngestDnsmasqLogDHCPAssociatesPendingFieldsWithMAC(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dhcp-fingerprints.db")
	store, err := logstore.OpenDHCPFingerprintLog(path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()
	input := strings.NewReader(strings.Join([]string{
		"dnsmasq-dhcp[1234]: client provides name: win-laptop",
		"dnsmasq-dhcp[1234]: vendor class: MSFT 5.0",
		"dnsmasq-dhcp[1234]: requested options: 1,15,3,6,44,46,47,31,33,249,43",
		"dnsmasq-dhcp[1234]: DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:ff",
	}, "\n"))
	if err := ingest(context.Background(), input, store); err != nil {
		t.Fatalf("ingest: %v", err)
	}
	rows, err := store.List(context.Background(), logstore.DHCPFingerprintFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows", len(rows))
	}
	if rows[0].Hostname != "win-laptop" || rows[0].VendorClass != "MSFT 5.0" || rows[0].OSFamily != "Windows" {
		t.Fatalf("unexpected row: %+v", rows[0])
	}
}
