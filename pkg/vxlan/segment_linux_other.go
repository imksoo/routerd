// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package vxlan

import (
	"context"
	"errors"
)

func newLinuxSegmentNative(func(context.Context) (string, error)) (SegmentNative, error) {
	return nil, errors.New("Linux VXLAN native backend is unavailable on this operating system")
}
