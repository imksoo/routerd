// SPDX-License-Identifier: BSD-3-Clause
package state

import (
	"database/sql"
	"errors"
	"github.com/imksoo/routerd/pkg/api"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthObservationReadsTypedCurrentState(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "routerd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	wanted := api.HealthObservation{Phase: "Healthy", CheckedAt: now}
	if err := s.SaveObjectStatus(api.NetAPIVersion, "HealthCheck", "wan", map[string]any{"phase": "Healthy", "routing": wanted}); err != nil {
		t.Fatal(err)
	}
	got, err := s.HealthObservation("wan")
	if err != nil || !got.Usable(now, api.HealthCheckSpec{}) {
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
