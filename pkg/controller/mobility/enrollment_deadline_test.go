// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestSAMEnrollmentDeadlineKeepsFractionalSecondAcrossSQLiteReopen(t *testing.T) {
	now := time.Date(2026, 10, 31, 0, 0, 0, 600000000, time.UTC)
	path := filepath.Join(t.TempDir(), "state.db")
	store, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	result := controlapi.NewSAMEnrollmentClaimSubmitResult("SAMEnrollmentClaim/pve-leaf-a", "SAMEnrollmentClaim/pve-leaf-a", 1, now, now.Add(8*time.Second))
	client := &fakeSAMEnrollmentJoinClient{now: now, submitResultSet: true, submitResult: &result}
	router := testSAMEnrollmentClientRouter("fractional-deadline")
	for i, resource := range router.Spec.Resources {
		if resource.Kind == "SAMEnrollmentClient" {
			spec, err := resource.SAMEnrollmentClientSpec()
			if err != nil {
				t.Fatal(err)
			}
			spec.StateTTLRefreshBefore = "2s"
			router.Spec.Resources[i].Spec = spec
		}
	}
	controller := SAMEnrollmentClientController{Router: router, Store: store, Now: func() time.Time { return now },
		ClientFactory: func(api.SAMEnrollmentClientSpec) []SAMEnrollmentJoinClient { return []SAMEnrollmentJoinClient{client} },
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := routerstate.OpenSQLite(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	controller.Store = reopened
	if got := controller.NextReconcileAfter(); got != 6*time.Second {
		t.Fatalf("persisted renewal ran early and can lose its next safety deadline: got=%s want=6s", got)
	}
}
