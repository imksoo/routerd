// SPDX-License-Identifier: BSD-3-Clause

package ipsec

import "context"

// saID is the kernel lookup identity reported by an owned VICI CHILD_SA.
// It is never inferred from an endpoint pair alone.
type saID struct {
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Protocol    string `json:"protocol"`
	SPI         uint32 `json:"spi"`
	Mark        uint32 `json:"mark,omitempty"`
	Mask        uint32 `json:"mask,omitempty"`
	IfID        uint32 `json:"ifID,omitempty"`
}

// No traffic counter or key material is persisted. Fingerprint binds the exact
// immutable native configuration, including a digest of the native key bytes.
type saReceipt struct {
	ID          saID   `json:"id"`
	Scope       string `json:"scope"`
	ReqID       uint32 `json:"reqID"`
	AddTime     uint64 `json:"addTime"`
	Fingerprint string `json:"fingerprint"`
}

type nativeSAStore interface {
	Observe(context.Context, saID) (saReceipt, bool, error)
	Delete(context.Context, saReceipt) error
}
