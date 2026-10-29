// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		"dnsmasq-dhcp[1234]: 42 client provides name: win-laptop",
		"dnsmasq-dhcp[1234]: 42 vendor class: MSFT 5.0",
		"dnsmasq-dhcp[1234]: 42 requested options: 1,15,3,6,44,46,47,31,33,249,43",
		"dnsmasq-dhcp[1234]: 42 DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:ff",
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

func TestFingerprintTransactionIdentityPreservesInterleavedClientsAndSplitOptions(t *testing.T) {
	store, err := logstore.OpenDHCPFingerprintLog(filepath.Join(t.TempDir(), "fp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	input := strings.NewReader(strings.Join([]string{
		"100 vendor class: MSFT 5.0",
		"200 vendor class: android-dhcp-14",
		"100 DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:01",
		"200 DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:02",
		"100 requested options: 1:netmask, 15:domain-name, 3:router",
		"200 client provides name: phone",
		"100 requested options: 6:dns-server, 44:netbios-ns, 46:netbios-node, 47:netbios-scope",
		"vendor class: must-not-attach-to-last-client",
	}, "\n"))
	if err := ingest(context.Background(), input, store); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List(context.Background(), logstore.DHCPFingerprintFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%+v", rows)
	}
	for _, row := range rows {
		switch row.MAC {
		case "aa:bb:cc:dd:ee:01":
			if row.VendorClass != "MSFT 5.0" || len(row.RequestedOptions) != 7 {
				t.Fatalf("client1=%+v", row)
			}
		case "aa:bb:cc:dd:ee:02":
			if row.VendorClass != "android-dhcp-14" || row.Hostname != "phone" {
				t.Fatalf("client2=%+v", row)
			}
		}
	}
}

func TestFingerprintCollidingTransactionDoesNotCrossAttributeMetadata(t *testing.T) {
	store, err := logstore.OpenDHCPFingerprintLog(filepath.Join(t.TempDir(), "fp.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	i := newFingerprintIngester(store)
	for _, line := range []string{"10 DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:01", "10 DHCPDISCOVER(eth0) aa:bb:cc:dd:ee:02", "10 vendor class: MSFT"} {
		if err := i.line(context.Background(), line, time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := store.List(context.Background(), logstore.DHCPFingerprintFilter{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.VendorClass != "" {
			t.Fatalf("ambiguous metadata persisted: %+v", row)
		}
	}
}

func TestJournalProcessStopsWhenIngestRejectsOversizedLine(t *testing.T) {
	old := commandContext
	t.Cleanup(func() { commandContext = old })
	commandContext = func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "printf '%070000d\\n' 0; exec sleep 30")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err := run(ctx, []string{"--db", filepath.Join(t.TempDir(), "fp.db")})
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("ingest result: %v", err)
	}
	if ctx.Err() != nil {
		t.Fatal("journal child was not released after ingest failure")
	}
}
