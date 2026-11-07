// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestSystemdUnitControllerMarksDisabledPPPoESession(t *testing.T) {
	enabled := false
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "PPPoESession"}, Metadata: api.ObjectMeta{Name: "pppoe-flets"}, Spec: api.PPPoESessionSpec{
			Interface: "wan",
			IfName:    "ppp-flets",
			Enabled:   &enabled,
			Username:  "user",
		}},
	}}}
	store := mapStore{}
	controller := SystemdUnitController{Router: router, Store: store, SystemdSystemDir: t.TempDir(), DryRun: true}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	status := store.ObjectStatus(api.NetAPIVersion, "PPPoESession", "pppoe-flets")
	if status["phase"] != PhaseDisabled {
		t.Fatalf("pppoe status = %#v", status)
	}
}
