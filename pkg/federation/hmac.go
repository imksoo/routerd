// SPDX-License-Identifier: BSD-3-Clause

package federation

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"math"
	"strconv"
	"time"
)

// Canonical signing scheme for Event Federation peer delivery (ADR 0006,
// Phase 2). This is the single source of truth: the daemon (Chunk 2) MUST use
// Sign/Verify and never re-derive the canonical input.
//
// The signed input is:
//
//	timestamp + "\n" + body
//
// where timestamp is the unix-seconds value rendered in base-10 (no leading
// zeros, via strconv.FormatInt) and body is the raw request body bytes
// verbatim. The signature is HMAC-SHA256 over that input keyed by the shared
// secret, encoded as lowercase hex.
//
// Verify additionally enforces replay protection: a signature is rejected if
// the timestamp is outside ±window of the verifier's clock.

// Sentinel errors returned by Verify so callers can record the reason.
var (
	// ErrBadSignature means the recomputed HMAC did not match the supplied one.
	ErrBadSignature = errors.New("federation: bad signature")
	// ErrStaleTimestamp means the timestamp fell outside the allowed window.
	ErrStaleTimestamp = errors.New("federation: stale timestamp")
)

// Sign returns the lowercase hex HMAC-SHA256 of the canonical input
// (timestamp + "\n" + body) keyed by secret.
func Sign(secret []byte, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(strconv.FormatInt(timestamp, 10)))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify recomputes the canonical HMAC over (timestamp, body) and compares it
// against signatureHex in constant time, and rejects timestamps that fall
// outside ±window of now. It returns ErrStaleTimestamp for replay/skew and
// ErrBadSignature for a mismatch, or nil when the signature is valid and fresh.
func Verify(secret []byte, timestamp int64, body []byte, signatureHex string, now time.Time, window time.Duration) error {
	if window > 0 {
		skew := now.Unix() - timestamp
		if skew < 0 {
			skew = -skew
		}
		if float64(skew) > math.Floor(window.Seconds()) {
			return ErrStaleTimestamp
		}
	}
	want := Sign(secret, timestamp, body)
	if !hmac.Equal([]byte(want), []byte(signatureHex)) {
		return ErrBadSignature
	}
	return nil
}
