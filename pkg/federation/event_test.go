// SPDX-License-Identifier: BSD-3-Clause

package federation

import (
	"testing"
	"time"
)

func TestEventNormalize(t *testing.T) {
	tests := []struct {
		name      string
		event     Event
		wantErr   bool
		wantDedup string
	}{
		{
			name:      "fills dedupe key from id",
			event:     Event{ID: " evt-1 ", Group: "g", Type: "t"},
			wantDedup: "evt-1",
		},
		{
			name:      "keeps explicit dedupe key",
			event:     Event{ID: "evt-1", Group: "g", Type: "t", DedupeKey: "dk"},
			wantDedup: "dk",
		},
		{
			name:    "missing id",
			event:   Event{Group: "g", Type: "t"},
			wantErr: true,
		},
		{
			name:    "missing group",
			event:   Event{ID: "evt-1", Type: "t"},
			wantErr: true,
		},
		{
			name:    "missing type",
			event:   Event{ID: "evt-1", Group: "g"},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := tc.event
			err := ev.Normalize()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("normalize: %v", err)
			}
			if ev.DedupeKey != tc.wantDedup {
				t.Fatalf("dedupeKey = %q, want %q", ev.DedupeKey, tc.wantDedup)
			}
		})
	}
}

func TestEventIsExpired(t *testing.T) {
	base := time.Date(2026, 5, 30, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		expires time.Time
		now     time.Time
		want    bool
	}{
		{name: "zero never expires", expires: time.Time{}, now: base, want: false},
		{name: "future not expired", expires: base.Add(time.Hour), now: base, want: false},
		{name: "past expired", expires: base.Add(-time.Hour), now: base, want: true},
		{name: "exactly now not expired", expires: base, now: base, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev := Event{ID: "x", Group: "g", Type: "t", ExpiresAt: tc.expires}
			if got := ev.IsExpired(tc.now); got != tc.want {
				t.Fatalf("IsExpired = %v, want %v", got, tc.want)
			}
		})
	}
}
