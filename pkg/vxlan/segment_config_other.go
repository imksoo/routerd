// SPDX-License-Identifier: BSD-3-Clause

//go:build !freebsd

package vxlan

import (
	"context"
	"errors"
)

func readFreeBSDSegmentConfig(context.Context, string, int) (bsdSegmentConfig, error) {
	return bsdSegmentConfig{}, errors.New("FreeBSD VXLAN driver observation is unavailable on this operating system")
}
