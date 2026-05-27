// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package healthcheck

import (
	"context"
	"errors"
)

// lookupRoute is a no-op on non-Linux platforms. ProbeEvidence still carries
// the spec-derived egress / source info; only the kernel-side nexthop is
// missing. FreeBSD support can be added later by exec'ing `route get` and
// parsing its output.
func lookupRoute(_ context.Context, _, _ string) (RouteInfo, error) {
	return RouteInfo{}, errors.New("route lookup not implemented on this platform")
}
