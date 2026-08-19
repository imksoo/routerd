// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"fmt"
	"sort"
	"strings"
)

// ownershipResolverConflictReason is used while deriving the pool lifecycle
// phase. The owner table is the single operator-facing ownership result, so
// this must not create another pool-level status projection.
func ownershipResolverConflictReason(decisions []ownershipDecision) string {
	for _, decision := range decisions {
		if strings.TrimSpace(decision.ConflictReason) != "" {
			return "remote home owner overlaps local ownership evidence"
		}
	}
	return ""
}

func ownershipResolverClaimState(d ownershipDecision) string {
	if d.ConflictReason != "" {
		return "Conflict"
	}
	if d.Class == ownershipClassUnknown {
		return "Unknown"
	}
	if d.Class == ownershipClassStaleCapture || d.CaptureState == captureStateStale {
		return "Stale"
	}
	return "OK"
}

func ownershipResolverControlPlaneOwnerTable(decisions []ownershipDecision) []map[string]any {
	rows := make([]map[string]any, 0, len(decisions))
	for _, d := range decisions {
		row := map[string]any{
			"address": d.Address,
			"state":   ownershipResolverClaimState(d),
			"class":   d.Class,
			"source":  d.Source,
		}
		putNonEmpty(row, "ownerNode", d.HomeOwnerNode)
		putNonEmpty(row, "ownerProviderRef", d.HomeProviderRef)
		putNonEmpty(row, "ownerNICRef", d.HomeNICRef)
		putNonEmpty(row, "ownerResourceRef", d.HomeResourceRef)
		putNonEmpty(row, "localEvidenceNode", d.LocalNodeRef)
		putNonEmpty(row, "localEvidenceNICRef", d.LocalNICRef)
		putNonEmpty(row, "localEvidenceResourceRef", d.LocalResourceRef)
		putNonEmpty(row, "localEvidenceSource", d.LocalSource)
		putNonEmpty(row, "captureHolderNode", d.CaptureHolderNode)
		putNonEmpty(row, "captureDisposition", string(d.CaptureDisposition))
		putNonEmpty(row, "captureReason", d.CaptureReason)
		if d.CaptureState != "" && d.CaptureState != captureStateNone {
			row["captureState"] = d.CaptureState
		}
		putNonEmpty(row, "advertiseOwnerNode", d.AdvertiseOwnerNode)
		putNonEmpty(row, "suppressionReason", d.SuppressionReason)
		putNonEmpty(row, "conflictReason", d.ConflictReason)
		putNonEmpty(row, "conflictWinnerNode", d.ConflictWinnerNode)
		putNonEmpty(row, "conflictResolution", d.ConflictResolution)
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left := fmt.Sprint(rows[i]["address"])
		right := fmt.Sprint(rows[j]["address"])
		if left == right {
			return fmt.Sprint(rows[i]["localEvidenceNode"]) < fmt.Sprint(rows[j]["localEvidenceNode"])
		}
		return left < right
	})
	return rows
}

func putNonEmpty(row map[string]any, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		row[key] = value
	}
}
