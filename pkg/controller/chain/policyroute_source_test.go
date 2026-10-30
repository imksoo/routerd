// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func policyTargetHealthyForTest(t *testing.T, controller IPv4PolicyRouteController, name string) bool {
	t.Helper()
	healthy, err := controller.targetHealthy(name)
	if err != nil {
		t.Fatal(err)
	}
	return healthy
}

func policyDSLiteReadyForTest(t *testing.T, controller IPv4PolicyRouteController, name string) bool {
	t.Helper()
	ready, err := controller.dsliteResourceReady(name)
	if err != nil {
		t.Fatal(err)
	}
	return ready
}

func effectivePolicyRouterForTest(t *testing.T, controller IPv4PolicyRouteController, ctx context.Context, active map[string]bool) *api.Router {
	t.Helper()
	router, err := controller.effectivePolicyRouteRouter(ctx, active)
	if err != nil {
		t.Fatal(err)
	}
	return router
}
