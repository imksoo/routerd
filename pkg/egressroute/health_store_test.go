// SPDX-License-Identifier: BSD-3-Clause
package egressroute

import (
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
	"strconv"
	"time"
)

// Existing fixtures describe observations using maps. Narrow them here; product
// routing reads typed observations from the real store instead.
func fixtureHealthObservation(status map[string]any) (api.HealthObservation, error) {
	if o, ok := status["routing"].(api.HealthObservation); ok {
		return o, nil
	}
	if observed, ok := status["observed"].(map[string]any); ok && len(observed) > 0 {
		status = observed
	}
	at, _ := time.Parse(time.RFC3339Nano, fmt.Sprint(status["lastCheckedAt"]))
	count, _ := strconv.Atoi(fmt.Sprint(status["consecutiveFailed"]))
	return api.HealthObservation{Phase: fmt.Sprint(status["phase"]), CheckedAt: at, ConsecutiveFailed: count}, nil
}
func (s mapStore) HealthObservation(name string) (api.HealthObservation, error) {
	return fixtureHealthObservation(s.ObjectStatus(api.NetAPIVersion, "HealthCheck", name))
}
