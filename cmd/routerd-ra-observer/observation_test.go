// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/raobserver"
)

func historyDaemon(now *time.Time) *daemon {
	d := newDaemon(options{resource: "monitor", ifname: "test0"})
	d.clock = func() time.Time { return *now }
	return d
}

func TestObserverHistoryExpiresAndEvictsWithoutLosingDetections(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	d := historyDaemon(&now)
	for i := 0; i <= maxObservedRouters; i++ {
		d.recordAdvertisement(raobserver.Advertisement{SourceMAC: fmt.Sprintf("02:00:00:00:00:%02x", i), SourceLLA: "fe80::1"})
		now = now.Add(time.Second)
	}
	if len(d.routers) != maxObservedRouters || d.historyEvicted != 1 || len(d.events) != maxObservedRouters+1 {
		t.Fatalf("history or detection lost its bound: routers=%d evicted=%d events=%d", len(d.routers), d.historyEvicted, len(d.events))
	}
	if _, exists := d.routers["020000000000"]; exists {
		t.Fatal("oldest router was not evicted")
	}
	now = now.Add(routerObservationTTL)
	status := d.status()
	if len(d.routers) != 0 || d.historyExpired != maxObservedRouters || status.Observed["rogueCount"] != "0" {
		t.Fatalf("quiet diagnostic history did not expire: %+v", status.Observed)
	}
	d.recordAdvertisement(raobserver.Advertisement{SourceMAC: "02:00:00:00:00:01", SourceLLA: "fe80::1"})
	if len(d.events) != maxObservedRouters+2 {
		t.Fatal("a renewed detection after expiry was suppressed")
	}
}

func TestObserverHistoryStatusStaysWithinActualReaderBudget(t *testing.T) {
	now := time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC)
	d := historyDaemon(&now)
	adv := raobserver.Advertisement{SourceLLA: "fe80::ffff:ffff:ffff:ffff", RouterLifetime: 65535}
	for i := 0; i < 200; i++ {
		adv.Prefixes = append(adv.Prefixes, raobserver.PrefixOption{Prefix: "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128", ValidLifetime: ^uint32(0), PreferredLifetime: ^uint32(0)})
		adv.Routes = append(adv.Routes, raobserver.RouteOption{Prefix: "ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff/128", Preference: "medium", Lifetime: ^uint32(0)})
		adv.RDNSS = append(adv.RDNSS, raobserver.RDNSSOption{Servers: []string{"ffff:ffff:ffff:ffff:ffff:ffff:ffff:ffff"}, Lifetime: ^uint32(0)})
	}
	for i := 0; i < maxObservedRouters; i++ {
		adv.SourceMAC = fmt.Sprintf("02:00:00:00:00:%02x", i)
		d.recordAdvertisement(adv)
	}
	encoded, err := json.Marshal(d.status())
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) >= 1<<20 || d.observationsTruncated != maxObservedRouters || d.observationItemsOmitted == 0 {
		t.Fatalf("status budget/diagnostics: bytes=%d truncated=%d omitted=%d", len(encoded), d.observationsTruncated, d.observationItemsOmitted)
	}
	if len(adv.Prefixes) != 200 || len(adv.RDNSS) != 200 {
		t.Fatal("diagnostic bounding mutated the input advertisement")
	}
}
