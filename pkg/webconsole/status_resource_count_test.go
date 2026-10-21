// SPDX-License-Identifier: BSD-3-Clause

package webconsole

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
	"github.com/imksoo/routerd/pkg/controlapi"
	"testing"
)

// A console can outlive configuration replacement; its status count must use
// the latest declaration rather than the initial pointer or filtered results.
func TestStatusResourceCountFollowsCurrentRouter(t *testing.T) {
	initial := &api.Router{Spec: api.RouterSpec{Resources: make([]api.Resource, 3)}}
	current := initial
	result := &apply.Result{Phase: "Healthy", Generation: 7, Resources: make([]apply.ResourceResult, 1)}
	controllers := []controlapi.ControllerStatus{{Name: "dns"}}
	h := New(Options{Router: initial, CurrentRouter: func() *api.Router { return current }})
	for _, count := range []int{3, 4, 2, 0} {
		current = &api.Router{Spec: api.RouterSpec{Resources: make([]api.Resource, count)}}
		status := h.statusWithControllers(result, controllers)
		if status.Status.ResourceCount != count {
			t.Errorf("resourceCount = %d, want %d", status.Status.ResourceCount, count)
		}
		if status.Status.Phase != "Healthy" || status.Status.Generation != 7 {
			t.Error("observation metadata changed")
		}
		if len(status.Status.Controllers) != 1 || status.Status.Controllers[0].Name != "dns" {
			t.Error("controller status changed")
		}
	}
	fallback := New(Options{Router: initial}).statusWithControllers(result, controllers)
	if fallback.Status.ResourceCount != 3 {
		t.Errorf("fixed declaration: resourceCount = %d, want 3", fallback.Status.ResourceCount)
	}
}
