// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package healthcheck

import (
	"context"
	"errors"
)

// lookupRoute is a no-op on platforms without a native route lookup adapter.
// ProbeEvidence still carries the spec-derived egress / source info; only the
// kernel-side nexthop is missing.
func lookupRoute(_ context.Context, _, _ string) (RouteInfo, error) {
	return RouteInfo{}, errors.New("route lookup not implemented on this platform")
}
