// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/imksoo/routerd/pkg/platform"
)

// nativeOperationOwner encloses the complete read/pending/effect/publication
// lifetime. It is shared by serve's internal applies, never by another CLI.
// The descriptor is raw and CLOEXEC: an unacknowledged shutdown deliberately
// retains it until OS process exit, without a file finalizer or a registry.
type nativeOperationOwner struct {
	descriptor int
	retained   atomic.Bool
}

func acquireNativeOperationOwner(ctx context.Context) (*nativeOperationOwner, error) {
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return nil, fmt.Errorf("identify native operation scope: %w", err)
	}
	digest := sha256.Sum256([]byte(scope))
	path := filepath.Join(platformDefaults.RuntimeDir, fmt.Sprintf("native-operation-%x.lock", digest))
	descriptor, err := lockNativeOperation(path)
	if err != nil {
		return nil, err
	}
	return &nativeOperationOwner{descriptor: descriptor}, nil
}

func (owner *nativeOperationOwner) retainUntilProcessExit() {
	if owner != nil {
		owner.retained.Store(true)
	}
}

func (owner *nativeOperationOwner) finish(acknowledged bool) error {
	if owner == nil || owner.descriptor < 0 {
		return nil
	}
	if !acknowledged {
		owner.retainUntilProcessExit()
	}
	descriptor := owner.descriptor
	owner.descriptor = -1
	if owner.retained.Load() {
		return nil
	} // raw FD stays open until actual OS exit
	return closeNativeOperation(descriptor)
}
