// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/controller/framework"
	"github.com/imksoo/routerd/pkg/ha"
	"github.com/imksoo/routerd/pkg/resourcequery"
)

func TestStartCannotAuthorizeRoleGatedVIPFromPersistedMaster(t *testing.T) {
	requireLinuxRuntimeFixture(t)
	if _, err := os.Stat("/etc/keepalived/keepalived.conf"); !os.IsNotExist(err) {
		t.Skip("host has a keepalived configuration; native role observation uses a separate fixture")
	}
	router := &api.Router{Metadata: api.ObjectMeta{Name: "restarting-router"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "rd15-lan", Managed: false}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "gateway"}, Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "198.18.30.1/32", Mode: "vrrp", VRRP: api.VirtualAddressVRRPSpec{VirtualRouterID: 130}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VirtualAddress"}, Metadata: api.ObjectMeta{Name: "shared-wan"}, Spec: api.VirtualAddressSpec{Family: "ipv4", Interface: "lan", Address: "198.18.30.9/32", Mode: "static", When: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"VirtualAddress/gateway.role": {Equals: "master"}}}}},
	}}}
	store := mapStore{api.NetAPIVersion + "/VirtualAddress/gateway": {"phase": "Applied", "role": "master"}}
	runner := &Runner{Router: router, Bus: bus.New(), Store: store}
	sentinel := errors.New("stop fixture before native controllers")
	entered := false
	runner.generationBuilder = func(_ context.Context, _ *slog.Logger, status eventedStore, _ bool, _ ha.Decision) ([]framework.Controller, DaemonStatusController, controllerLifetimes, error) {
		entered = true
		filtered, err := resourcequery.FilterRouterByResolvedWhen(router, status)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range filtered.Spec.Resources {
			if r.Metadata.Name == "shared-wan" {
				t.Error("persisted master authorized a shared address before current role observation")
			}
		}
		return nil, DaemonStatusController{}, controllerLifetimes{}, sentinel
	}
	if err := runner.Start(t.Context()); !errors.Is(err, sentinel) {
		t.Fatalf("startup result: %v", err)
	}
	if !entered {
		t.Fatal("did not reach controller bootstrap boundary")
	}
}
