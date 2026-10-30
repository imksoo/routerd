// SPDX-License-Identifier: BSD-3-Clause

package logstore

import (
	"testing"
	"time"
)

func TestParseRetentionRejectsNegativeAndOverflow(t *testing.T) {
	for _, value := range []string{"-1d", "-1h", "200000d", "9223372036854775807d", "106752d"} {
		if duration, err := ParseRetention(value); err == nil {
			t.Fatalf("invalid retention %s became %s", value, duration)
		}
	}
	for _, test := range []struct {
		value string
		want  time.Duration
	}{{"0", 0}, {"0d", 0}, {"30d", 30 * 24 * time.Hour}, {"106751d", 106751 * 24 * time.Hour}, {"1h", time.Hour}} {
		if duration, err := ParseRetention(test.value); err != nil || duration != test.want {
			t.Fatalf("retention %s = %s, %v", test.value, duration, err)
		}
	}
}
