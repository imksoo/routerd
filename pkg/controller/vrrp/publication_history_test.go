package vrrp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func TestGracefulActivationRequiresMatchingPublicationHistory(t *testing.T) {
	for _, tc := range []struct {
		name, role, previousRole, address, ifname string
		advertised                                bool
		state                                     string
		keep                                      bool
	}{
		{name: "no history", role: "master"},
		{name: "published master survives reload", role: "master", previousRole: "master", address: "10.240.70.10/32", ifname: "ens18", advertised: true, state: "Ready", keep: true},
		{name: "failed publication", role: "master", previousRole: "master", address: "10.240.70.10/32", ifname: "ens18", advertised: true, state: "Failed"},
		{name: "different address", role: "master", previousRole: "master", address: "10.240.70.11/32", ifname: "ens18", advertised: true, state: "Ready"},
		{name: "different interface", role: "master", previousRole: "master", address: "10.240.70.10/32", ifname: "ens19", advertised: true, state: "Ready"},
		{name: "backup", role: "backup", previousRole: "master", address: "10.240.70.10/32", ifname: "ens18", advertised: true, state: "Ready"},
		{name: "fault", role: "fault", previousRole: "master", address: "10.240.70.10/32", ifname: "ens18", advertised: true, state: "Ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := vrrpRouter("vrrp")
			spec, _ := router.Spec.Resources[1].VirtualAddressSpec()
			spec.VRRP.GracefulActivation = &api.VirtualAddressVRRPGracefulActivationSpec{ReadyWhen: api.ResourceWhenSpec{State: map[string]api.StateMatchSpec{"DSLiteTunnel/a.phase": {Equals: "Up"}}}}
			router.Spec.Resources[1].Spec = spec
			store := statefulMapStore{mapStore: mapStore{api.NetAPIVersion + "/VirtualAddress/vip": {"role": tc.previousRole, "address": tc.address, "ifname": tc.ifname, "vipAdvertised": tc.advertised, "activationState": tc.state}}, values: map[string]routerstate.Value{}, now: time.Now()}
			present := true
			c := &Controller{Router: router, Store: store, IP: "ip", OperatingSystem: platform.OSLinux, Command: func(_ context.Context, name string, args ...string) ([]byte, error) {
				switch name + " " + strings.Join(args, " ") {
				case "ip -4 -o addr show dev ens18":
					return []byte("2: ens18 inet 10.240.70.10/32 scope global ens18\n"), nil
				case "ip addr del 10.240.70.10/32 dev ens18":
					present = false
				}
				return nil, nil
			}}
			statuses, err := reconcileGracefulActivations(context.Background(), c, map[string]string{"lan": "ens18"}, map[string]string{"vip": tc.role})
			if err != nil || present != tc.keep || statuses["vip"].VIPAdvertised != tc.keep {
				t.Fatalf("present=%v status=%+v err=%v", present, statuses["vip"], err)
			}
		})
	}
}
