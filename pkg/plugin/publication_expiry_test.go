// SPDX-License-Identifier: BSD-3-Clause

package plugin

import (
	"strings"
	"testing"
	"time"
)

func TestDynamicPluginObservationExpiresWhileInvocationRuns(t *testing.T) {
	observedAt := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	result := validPluginResult(observedAt)
	result.Status.TTL = "1s"
	if _, err := DynamicConfigPartFromResult("Plugin/slow", 1, result, observedAt.Add(time.Second)); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired external observation published: %v", err)
	}
}
