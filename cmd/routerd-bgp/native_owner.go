// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"

	"github.com/imksoo/routerd/pkg/platform"
)

// A raw CLOEXEC descriptor has no finalizer. If native shutdown is not
// acknowledged, its lock survives garbage collection until actual process exit.
type bgpProcessOwner struct {
	descriptor int
	path       string
}

func acquireBGPProcessOwner(ctx context.Context, runtimeDir string) (*bgpProcessOwner, error) {
	scope, err := platform.KernelNetworkScope(ctx)
	if err != nil {
		return nil, fmt.Errorf("identify BGP kernel world: %w", err)
	}
	digest := sha256.Sum256([]byte(scope))
	path := filepath.Join(runtimeDir, fmt.Sprintf("bgp-process-%x.lock", digest))
	descriptor, err := lockBGPProcess(path)
	if err != nil {
		return nil, err
	}
	return &bgpProcessOwner{descriptor: descriptor, path: path}, nil
}

func (owner *bgpProcessOwner) validatePaths(paths ...string) error {
	return checkBGPProcessPaths(owner.descriptor, owner.path, paths...)
}

func (owner *bgpProcessOwner) finish(acknowledged bool) error {
	if owner == nil || owner.descriptor < 0 {
		return nil
	}
	descriptor := owner.descriptor
	owner.descriptor = -1
	if !acknowledged {
		return nil
	}
	return closeBGPProcess(descriptor)
}
