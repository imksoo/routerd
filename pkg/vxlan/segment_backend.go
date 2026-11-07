// SPDX-License-Identifier: BSD-3-Clause

package vxlan

import (
	"context"
	"errors"
	"fmt"

	"github.com/imksoo/routerd/pkg/platform"
)

func NewSegmentNative(target platform.OS, scope func(context.Context) (string, error)) (SegmentNative, error) {
	if scope == nil {
		return nil, errors.New("VXLAN native kernel scope reader is required")
	}
	switch target {
	case platform.OSLinux:
		return newLinuxSegmentNative(scope)
	case platform.OSFreeBSD:
		return newFreeBSDSegmentNative(scope), nil
	default:
		return nil, fmt.Errorf("VXLAN native lifecycle is unsupported on %s", target)
	}
}
