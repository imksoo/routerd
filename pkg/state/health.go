// SPDX-License-Identifier: BSD-3-Clause

package state

import (
	"database/sql"
	"encoding/json"
	"errors"
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
	var status struct {
		Phase   string                `json:"phase"`
		Routing api.HealthObservation `json:"routing"`
	}
	if err := json.Unmarshal([]byte(raw), &status); err != nil {
		return api.HealthObservation{}, err
	}
	switch status.Phase {
	case "Disabled", "Standby", "NotApplicable":
		return api.HealthObservation{}, nil
	}
	return status.Routing, nil
}
