// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDerivedCheckpointDoesNotReinitializePartialOrOldState(t *testing.T) {
	for _, key := range []string{"definition", "revision", "initialized", "asserted", "pendingTarget", "pendingDueAt", "lastAssertedAt", "lastRetractedAt"} {
		if _, err := DecodeDerivedCheckpoint(map[string]any{key: "broken"}); err == nil {
			t.Fatalf("partial %s checkpoint was treated as fresh", key)
		}
	}
	if cp, err := DecodeDerivedCheckpoint(map[string]any{"owner": "derived-event", "phase": "Pending"}); err != nil || cp != (DerivedCheckpoint{}) {
		t.Fatalf("uninitialized declaration rejected: %+v %v", cp, err)
	}
}

func TestDerivedCommitRechecksProjectionExpiryWithoutSourceWrite(t *testing.T) {
	store, err := OpenSQLite(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	spec := api.HealthCheckSpec{Target: "192.0.2.1", Protocol: "icmp", Interval: "1s", Timeout: "1s"}
	sample := api.HealthObservation{Probe: spec.Probe(), Phase: "Healthy", CheckedAt: now}
	if err := store.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", map[string]any{"phase": "Healthy", "routing": sample}); err != nil {
		t.Fatal(err)
	}
	inputs, err := store.ReadDerivedInputs(t.Context(), []DerivedInputRef{{Kind: "HealthCheck", Name: "wan", Field: "phase"}})
	if err != nil || len(inputs.Values) != 1 || inputs.Values[0] != "Healthy" {
		t.Fatalf("initial current sample: %+v %v", inputs, err)
	}
	now = now.Add(sample.Probe.Freshness() + time.Nanosecond)
	next := DerivedCheckpoint{Definition: strings.Repeat("a", 64), Initialized: true, Asserted: true, LastAssertedAt: now}
	event := daemonapi.NewEvent(daemonapi.DaemonRef{Kind: "routerd-derived"}, "routerd.expiry.asserted", daemonapi.SeverityInfo)
	event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DerivedEvent", Name: "expiry"}
	if _, err := store.CommitDerived(t.Context(), "expiry", DerivedCheckpoint{}, next, &event, inputs); !errors.Is(err, ErrDerivedConflict) {
		t.Fatalf("expired unchanged source was accepted: %v", err)
	}
	current, err := store.ReadDerivedInputs(t.Context(), inputs.References)
	if err != nil || len(current.Values) != 1 || current.Values[0] != "Unknown" || current.Versions[0] != inputs.Versions[0] {
		t.Fatalf("test did not exercise time-only projection change: %+v %v", current, err)
	}
	status, err := store.ReadObjectStatus(api.NetAPIVersion, "DerivedEvent", "expiry")
	if err != nil || len(status) != 0 {
		t.Fatalf("expired source committed latch: %+v %v", status, err)
	}
	outputs, err := store.ListEvents(EventQuery{Topic: event.Type})
	if err != nil || len(outputs) != 0 {
		t.Fatalf("expired source committed output: %+v %v", outputs, err)
	}
}
