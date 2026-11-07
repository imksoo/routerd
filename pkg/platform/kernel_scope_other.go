// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !freebsd

package platform

import (
	"context"
	"errors"
)

func KernelNetworkScope(context.Context) (string, error) {
	return "", errors.New("native static address ownership requires Linux or FreeBSD")
}
