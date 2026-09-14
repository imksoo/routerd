package vrrp

import (
	"context"
	"fmt"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"strings"
	"testing"
	"time"
)

func TestPublishedVIPSurvivesObservationError(t *testing.T) {
	router := vrrpRouter("vrrp")
	spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
	spec.VRRP.GracefulActivation = &api.VirtualAddressVRRPGracefulActivationSpec{ReadyWhen: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"DSLiteTunnel/a.phase": {Equals: "Up"}}}}
	router.Spec.Resources[1].Spec = spec
	store := statefulMapStore{mapStore: mapStore{api.NetAPIVersion + "/VirtualAddress/vip": {"role": "master", "address": "10.240.70.10/32", "ifname": "ens18", "vipAdvertised": true, "activationState": "Ready"}}, values: map[string]routerstate.Value{}, now: time.Now()}
	failObservation, present := true, true
	c := &Controller{Router: router, Store: store, IP: "ip", OperatingSystem: platform.OSLinux, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name + " " + strings.Join(args, " ") {
		case "ip -4 -o addr show dev ens18":
			if failObservation {
				return nil, fmt.Errorf("temporary observation failure")
			}
			return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
		case "ip addr del 10.240.70.10/32 dev ens18":
			present = false
		}
		return nil, nil
	}}
	aliases, roles := map[string]string{"lan": "ens18"}, map[string]string{"vip": "master"}
	for i := 0; i < 2; i++ {
		statuses, err := reconcileGracefulActivations(context.Background(), c, aliases, roles)
		if err == nil {
			t.Fatal("expected observation error")
		}
		if err := c.saveStatuses("Applied", "", false, nil, roles, nil, statuses, nil); err != nil {
			t.Fatal(err)
		}
	}
	failObservation = false
	statuses, err := reconcileGracefulActivations(context.Background(), c, aliases, roles)
	if err != nil {
		t.Fatal(err)
	}
	if !present {
		t.Fatalf("published VIP removed after transient observation error: %+v", statuses["vip"])
	}
}
