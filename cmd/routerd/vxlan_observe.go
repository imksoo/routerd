// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	controllerchain "github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

// applyEngine binds inspection to the same native receipt reader used by
// child controllers. It neither reconciles nor creates ownership state.
func applyEngine(ctx context.Context, router *api.Router, observations resourcequery.StateStore) *apply.Engine {
	engine := apply.New(ctx)
	engine.Segments = &controllerchain.VXLANSegmentController{Router: router, GateStore: observations}
	return engine
}
