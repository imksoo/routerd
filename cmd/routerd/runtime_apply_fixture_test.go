// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"

	"github.com/imksoo/routerd/pkg/api"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
)

// Configuration-only fixtures supply the runtime boundary without starting host
// controllers. Actual worker, HA and HTTP tests use the production Runner.
func testCommitRuntime(current func() *api.Router) func(context.Context, *api.Router, bool, func() error) (controllerchain.RuntimeReloadOutcome, error) {
	return func(ctx context.Context, next *api.Router, _ bool, commit func() error) (controllerchain.RuntimeReloadOutcome, error) {
		prior := controllerchain.RuntimeReloadOutcome{Active: controllerchain.RuntimeSnapshot{Router: current(), Known: true, Available: true}, Accepted: true}
		if err := ctx.Err(); err != nil {
			return prior, err
		}
		if err := commit(); err != nil {
			return prior, err
		}
		return controllerchain.RuntimeReloadOutcome{Active: controllerchain.RuntimeSnapshot{Router: next, Known: true, Available: true}, Accepted: true}, nil
	}
}
