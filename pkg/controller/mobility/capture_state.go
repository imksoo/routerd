// SPDX-License-Identifier: BSD-3-Clause
// Capture state and provider-action fences for BGP mobility delivery.
package mobility

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
)

// bgpCaptureAssignment is the ephemeral transition view derived from the
// active provider ActionPlan. It is deliberately not a status record: the
// ActionPlan parameters and action journal are the durable fence boundary.
type bgpCaptureAssignment struct {
	Address        string
	Phase          string
	Generation     string
	DesiredHolder  string
	PreviousHolder string
	Reason         string
	IssuedAt       time.Time
	RenewedAt      time.Time
	LeaseUntil     time.Time
}

// decodeStatusValue is the only object-status codec used for the durable
// capture records. The storage boundary returns map-shaped JSON; planning gets
// the concrete value and never carries the map past this point.
func decodeStatusValue[T any](raw any) T {
	var out T
	if raw == nil {
		return out
	}
	if typed, ok := raw.(T); ok {
		return typed
	}
	encoded, err := json.Marshal(raw)
	if err != nil || json.Unmarshal(encoded, &out) != nil {
		var zero T
		return zero
	}
	return out
}

func parseBGPTrapLastSeenAt(value string) time.Time {
	if strings.TrimSpace(value) == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(value))
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

func observedSelfStaleCaptureSinceFromStatus(status map[string]any) map[string]time.Time {
	out := map[string]time.Time{}
	for address, since := range decodeStatusValue[map[string]time.Time](status["observedSelfStaleCaptures"]) {
		address = normalizeAddressString(address)
		if address != "" && !since.IsZero() {
			out[address] = since.UTC()
		}
	}
	return out
}

func observedSelfStaleCaptureTimes(decisions []ownershipDecision, selfNode string, previous map[string]time.Time, now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	for _, decision := range decisions {
		if decision.Class != ownershipClassStaleCapture && !providerInventoryConflictReleasesLocalCapture(decision, selfNode) {
			continue
		}
		if strings.TrimSpace(decision.CaptureHolderNode) != "" && strings.TrimSpace(decision.CaptureHolderNode) != strings.TrimSpace(selfNode) {
			continue
		}
		address := normalizeAddressString(decision.Address)
		if address == "" {
			continue
		}
		since := now.UTC()
		if previousSince, ok := previous[address]; ok && !previousSince.IsZero() {
			since = previousSince.UTC()
		}
		out[address] = since.UTC()
	}
	return out
}

func previousCaptureCandidateWithinMissingHold(candidate previousCaptureCandidate, now time.Time) bool {
	if candidate.LastSeenAt.IsZero() {
		return true
	}
	return now.UTC().Sub(candidate.LastSeenAt.UTC()) < bgpTrapRIBMissingHold
}

func normalizeBGPTrapPrefix(value string, poolPrefix netip.Prefix) (string, bool) {
	prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
	if err != nil {
		return "", false
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 || !poolPrefix.Contains(prefix.Addr()) {
		return "", false
	}
	return prefix.String(), true
}

func bgpTrapPathSig(address string, nextHops []string) string {
	return "prefix=" + normalizeAddressString(address) + ";nextHops=" + strings.Join(cleanStrings(nextHops), ",")
}

func bgpPathSigHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:16]
}

func mergeStringSet(base []string, extra []string) []string {
	return cleanStrings(append(append([]string(nil), base...), extra...))
}

func cleanStrings(values []string) []string {
	out := stringutil.UniqueTrimmedSorted(values)
	if out == nil {
		return []string{}
	}
	return out
}
