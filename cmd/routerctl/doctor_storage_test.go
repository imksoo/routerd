// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"strings"
	"testing"
	"time"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestDoctorEventJournalThresholdsAndRecovery(t *testing.T) {
	pass := doctorEventJournalStatsCheck(routerstate.EventJournalStats{Rows: 10, MaxRows: 100, PayloadBytes: 10, MaxPayloadBytes: 100})
	if pass.Status != doctorPass {
		t.Fatalf("pass check = %#v", pass)
	}
	warn := doctorEventJournalStatsCheck(routerstate.EventJournalStats{Rows: 95, MaxRows: 100, PayloadBytes: 10, MaxPayloadBytes: 100})
	if warn.Status != doctorWarn {
		t.Fatalf("warn check = %#v", warn)
	}
	alert := routerstate.NewStorageAlert("/var/lib/routerd/routerd.db", time.Now())
	fail := doctorEventJournalStatsCheck(routerstate.EventJournalStats{Rows: 100, MaxRows: 100, StorageAlert: &alert})
	if fail.Status != doctorFail || !strings.Contains(fail.Remedy, "reboot the router OS") || !strings.Contains(fail.Remedy, "persistent disk") {
		t.Fatalf("fail check = %#v", fail)
	}
}
