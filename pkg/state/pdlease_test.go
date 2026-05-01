package state

import (
	"testing"
	"time"
)

func TestPDLeaseHasFreshTransactionEvidence(t *testing.T) {
	now := time.Date(2026, 5, 1, 21, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		lease PDLease
		want  bool
	}{
		{
			name:  "no last reply",
			lease: PDLease{CurrentPrefix: "2001:db8::/60"},
			want:  false,
		},
		{
			name:  "no valid lifetime",
			lease: PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: "2026-05-01T20:00:00Z"},
			want:  false,
		},
		{
			name:  "valid lifetime not yet expired",
			lease: PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: "2026-05-01T20:00:00Z", VLTime: "14400"},
			want:  true,
		},
		{
			name:  "valid lifetime expired",
			lease: PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: "2026-04-30T20:00:00Z", VLTime: "14400"},
			want:  false,
		},
		{
			name:  "invalid timestamp",
			lease: PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: "garbage", VLTime: "14400"},
			want:  false,
		},
		{
			name:  "non-numeric vltime",
			lease: PDLease{CurrentPrefix: "2001:db8::/60", LastReplyAt: "2026-05-01T20:00:00Z", VLTime: "infinite"},
			want:  false,
		},
		{
			name:  "router01-style stale (LAN address only, no transaction evidence)",
			lease: PDLease{CurrentPrefix: "2409:10:3d60:1230::/60", LastPrefix: "2409:10:3d60:1230::/60"},
			want:  false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.lease.HasFreshTransactionEvidence(now); got != tc.want {
				t.Fatalf("HasFreshTransactionEvidence() = %v, want %v", got, tc.want)
			}
		})
	}
}
