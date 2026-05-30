// SPDX-License-Identifier: BSD-3-Clause

package federation

import (
	"errors"
	"testing"
	"time"
)

func TestSignVerifyRoundTrip(t *testing.T) {
	secret := []byte("shared-key")
	ts := int64(1748600000)
	body := []byte(`{"id":"evt-1","type":"routerd.client.ipv4.observed"}`)
	now := time.Unix(ts, 0)
	window := 5 * time.Minute

	sig := Sign(secret, ts, body)
	if err := Verify(secret, ts, body, sig, now, window); err != nil {
		t.Fatalf("valid round-trip rejected: %v", err)
	}
}

func TestVerifyTamperedBody(t *testing.T) {
	secret := []byte("shared-key")
	ts := int64(1748600000)
	body := []byte("original")
	now := time.Unix(ts, 0)
	window := 5 * time.Minute

	sig := Sign(secret, ts, body)
	if err := Verify(secret, ts, []byte("tampered"), sig, now, window); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("tampered body: got %v, want ErrBadSignature", err)
	}
}

func TestVerifyWrongSecret(t *testing.T) {
	ts := int64(1748600000)
	body := []byte("payload")
	now := time.Unix(ts, 0)
	window := 5 * time.Minute

	sig := Sign([]byte("right-secret"), ts, body)
	if err := Verify([]byte("wrong-secret"), ts, body, sig, now, window); !errors.Is(err, ErrBadSignature) {
		t.Fatalf("wrong secret: got %v, want ErrBadSignature", err)
	}
}

func TestVerifyStaleTimestamp(t *testing.T) {
	secret := []byte("shared-key")
	ts := int64(1748600000)
	body := []byte("payload")
	window := 5 * time.Minute
	// now is 10 minutes after the signed timestamp -> outside the 5m window.
	now := time.Unix(ts, 0).Add(10 * time.Minute)

	sig := Sign(secret, ts, body)
	if err := Verify(secret, ts, body, sig, now, window); !errors.Is(err, ErrStaleTimestamp) {
		t.Fatalf("stale timestamp: got %v, want ErrStaleTimestamp", err)
	}
}

func TestVerifyFreshWithinWindow(t *testing.T) {
	secret := []byte("shared-key")
	ts := int64(1748600000)
	body := []byte("payload")
	window := 5 * time.Minute
	// now is 4 minutes off (either side) -> inside the 5m window.
	sig := Sign(secret, ts, body)
	for _, delta := range []time.Duration{4 * time.Minute, -4 * time.Minute} {
		now := time.Unix(ts, 0).Add(delta)
		if err := Verify(secret, ts, body, sig, now, window); err != nil {
			t.Fatalf("fresh within window (delta %v) rejected: %v", delta, err)
		}
	}
}
