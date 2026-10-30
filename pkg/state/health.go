// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/api"
)

// HealthObservation reads the same current object that owns status publication.
// Old string-map observations are not a second supported routing input.
func (s *SQLiteStore) HealthObservation(name string) (api.HealthObservation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.closed {
		return api.HealthObservation{}, sql.ErrConnDone
	}
	var raw string
	err := s.db.QueryRow("SELECT coalesce(status,'{}') FROM objects WHERE api_version=? AND kind='HealthCheck' AND name=?", api.NetAPIVersion, name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return api.HealthObservation{}, nil
	}
	if err != nil {
		return api.HealthObservation{}, err
	}
	return currentHealthObservation(raw, s.now().UTC())
}

type healthStatusDocument struct {
	Phase   string                `json:"phase"`
	Routing api.HealthObservation `json:"routing"`
}

func currentHealthObservation(raw string, now time.Time) (api.HealthObservation, error) {
	var status healthStatusDocument
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return api.HealthObservation{}, err
	}
	if status.Phase == status.Routing.Phase && status.Routing.Current(now) {
		return status.Routing, nil
	}
	return api.HealthObservation{}, nil
}

// This is a read projection only. The raw sample remains available as history;
// an expired, identityless, or inconsistent sample is never a current phase.
func projectHealthStatus(status map[string]any, raw string, now time.Time) error {
	switch status["phase"] {
	case "Healthy", "Passing", "Failing", "Unhealthy":
	default:
		return nil
	}
	current, err := currentHealthObservation(raw, now)
	if err != nil {
		return err
	}
	if current == (api.HealthObservation{}) {
		status["phase"] = "Unknown"
		status["health"] = daemonapi.HealthUnknown
		status["reason"] = "ProbeObservationUnknown"
		status["conditions"] = []daemonapi.Condition{{Type: "Healthy", Status: daemonapi.ConditionUnknown, Reason: "ProbeObservationUnknown"}}
	}
	return nil
}
