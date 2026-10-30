// SPDX-License-Identifier: BSD-3-Clause
package state

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

func TestHealthObservationReadsTypedCurrentState(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	spec := api.HealthCheckSpec{Target: "192.0.2.1", Interval: "30.000000001s", Timeout: "3.000000001s"}
	wanted := api.HealthObservation{Probe: spec.Probe(), Phase: "Healthy", CheckedAt: now}
	if err := s.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", map[string]any{"phase": "Healthy", "routing": wanted}); err != nil {
		t.Fatal(err)
	}
	got, err := s.HealthObservation("wan")
	if err != nil || !got.Usable(now, spec) {
		t.Fatalf("typed observation: %#v %v", got, err)
	}
	for _, phase := range []string{"Disabled", "Standby", "NotApplicable", "Pending", "Unknown", "Unhealthy"} {
		if err := s.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", map[string]any{"phase": phase, "routing": wanted, "observed": map[string]any{"phase": "Healthy"}}); err != nil {
			t.Fatal(err)
		}
		got, err = s.HealthObservation("wan")
		if err != nil || got != (api.HealthObservation{}) {
			t.Fatalf("neutral or mismatched %s promoted history: %#v %v", phase, got, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.HealthObservation("wan"); !errors.Is(err, sql.ErrConnDone) {
		t.Fatalf("closed reader: %v", err)
	}
}

func TestHealthReadProjectionDoesNotRewriteStoredSample(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Date(2026, 10, 30, 1, 2, 3, 0, time.UTC)
	s.now = func() time.Time { return now }
	spec := api.HealthCheckSpec{Target: "192.0.2.1", Interval: "30.000000001s", Timeout: "3.000000001s"}
	sample := api.HealthObservation{Probe: spec.Probe(), Phase: "Healthy", CheckedAt: now}
	if err := s.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", map[string]any{"phase": "Healthy", "health": "healthy", "routing": sample}); err != nil {
		t.Fatal(err)
	}
	status, err := s.ReadObjectStatus(api.NetAPIVersion, "HealthCheck", "wan")
	if err != nil || status["phase"] != "Healthy" {
		t.Fatalf("fresh status: %#v %v", status, err)
	}
	// The normal generic status persistence boundary must retain exact nanoseconds.
	if err := s.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", status); err != nil {
		t.Fatal(err)
	}
	current, err := s.HealthObservation("wan")
	if err != nil || current.Probe != spec.Probe() {
		t.Fatalf("generic round trip changed probe: %+v %v", current, err)
	}
	var before string
	if err := s.db.QueryRow(`SELECT status FROM objects WHERE api_version=? AND kind=? AND name=?`, api.NetAPIVersion, "HealthCheck", "wan").Scan(&before); err != nil {
		t.Fatal(err)
	}
	now = now.Add(3 * time.Minute)
	status, err = s.ReadObjectStatus(api.NetAPIVersion, "HealthCheck", "wan")
	if err != nil || status["phase"] != "Unknown" || status["routing"] == nil {
		t.Fatalf("expired projection: %#v %v", status, err)
	}
	listed, err := s.ListObjectStatuses()
	if err != nil || len(listed) != 1 || listed[0].Status["phase"] != "Unknown" {
		t.Fatalf("expired list projection: %#v %v", listed, err)
	}
	current, err = s.HealthObservation("wan")
	if err != nil || current != (api.HealthObservation{}) {
		t.Fatalf("expired observation: %+v %v", current, err)
	}
	var after string
	if err := s.db.QueryRow(`SELECT status FROM objects WHERE api_version=? AND kind=? AND name=?`, api.NetAPIVersion, "HealthCheck", "wan").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("read projected freshness by mutating retained sample")
	}
}
