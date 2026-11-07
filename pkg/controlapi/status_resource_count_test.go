// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/apply"
)

func TestStatusResourceCountUsesCurrentDeclaration(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: make([]api.Resource, 2)}}
	for _, tt := range []struct {
		name   string
		result *apply.Result
		phase  string
	}{
		{name: "before-observation", phase: "Unknown"},
		{name: "failed-apply", result: &apply.Result{Phase: "Error", Generation: 12}, phase: "Error"},
		{name: "plan-with-derived-resource", result: &apply.Result{Phase: "Healthy", Resources: make([]apply.ResourceResult, 3)}, phase: "Healthy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			status := NewStatus(tt.result, router)
			if status.Status.ResourceCount != 2 {
				t.Errorf("resourceCount = %d, want 2", status.Status.ResourceCount)
			}
			if status.Status.Phase != tt.phase {
				t.Errorf("phase = %q, want %q", status.Status.Phase, tt.phase)
			}
			if tt.result != nil && status.Status.Generation != tt.result.Generation {
				t.Error("generation changed")
			}
		})
	}
	previous := &apply.Result{Phase: "Healthy", Resources: make([]apply.ResourceResult, 2)}
	for _, count := range []int{1, 4, 0} {
		next := &api.Router{Spec: api.RouterSpec{Resources: make([]api.Resource, count)}}
		if got := NewStatus(previous, next).Status.ResourceCount; got != count {
			t.Errorf("changed configuration: resourceCount = %d, want %d", got, count)
		}
	}
	if got := NewStatus(previous, nil).Status.ResourceCount; got != 0 {
		t.Errorf("absent configuration: resourceCount = %d, want 0", got)
	}
}
