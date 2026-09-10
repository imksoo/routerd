// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"strings"
)

// PoolStatusEventProjection removes pool-local scan bookkeeping from a status
// already normalized by the generic status layer. Semantic plan, ownership and
// capture fields remain intact. The returned map never aliases the input map.
func PoolStatusEventProjection(status map[string]any) map[string]any {
	out := make(map[string]any, len(status))
	for key, value := range status {
		switch key {
		case "bgpCaptureTransitionCompleted",
			"discoveryLastScanAt", "lastEventAt", "lastPacketAt", "lastScanAt",
			"packetsSeen", "scanCount", "probeCount", "probeHitCount", "proactiveCount":
			continue
		}
		out[key] = value
	}
	return out
}

// PoolObservationRefreshOnly classifies changes that extend discovery evidence
// without changing a pool's plan. It leaves changedFields intact so callers can
// retain their existing diagnostic and severity contracts.
func PoolObservationRefreshOnly(current, next map[string]any, fields []string) bool {
	if len(fields) == 0 {
		return false
	}
	for _, field := range fields {
		switch strings.TrimSpace(field) {
		case "phase":
			previousPhase, phase := fmt.Sprint(current["phase"]), fmt.Sprint(next["phase"])
			if !((previousPhase == "Watching" && phase == "Ready") ||
				(previousPhase == "Ready" && phase == "Watching") ||
				(previousPhase == "Pending" && phase == "Watching") ||
				(previousPhase == "Watching" && phase == "Pending")) {
				return false
			}
		case "discoveryCompletedAt", "discoveryFreshUntil", "discoveryLastScanAt", "discoveryObserved":
		default:
			return false
		}
	}
	return true
}
