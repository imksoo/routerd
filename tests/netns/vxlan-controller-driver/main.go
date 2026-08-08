// SPDX-License-Identifier: BSD-3-Clause

// vxlan-controller-driver is an integration-only adapter that runs the
// production chain.VXLANTunnelController against an iproute2 network namespace.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type store struct{ values map[string]routerstate.Value }

func (s *store) SaveObjectStatus(string, string, string, map[string]any) error { return nil }
func (s *store) ObjectStatus(string, string, string) map[string]any            { return nil }
func (s *store) Get(name string) routerstate.Value                             { return s.values[name] }
func (s *store) Age(name string) time.Duration                                 { return s.Now().Sub(s.values[name].UpdatedAt) }
func (s *store) Now() time.Time                                                { return time.Now().UTC() }

func main() {
	if len(os.Args) != 7 {
		panic("usage: driver NETNS ROLE WITNESS LOCAL PEER VNI")
	}
	ns, role, witness, local, peer := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	vni, err := strconv.Atoi(os.Args[6])
	if err != nil {
		panic(err)
	}
	now := time.Now().UTC()
	s := &store{values: map[string]routerstate.Value{
		"ha.role":    {Status: routerstate.StatusSet, Value: role, Since: now, UpdatedAt: now},
		"ha.witness": {Status: routerstate.StatusSet, Value: witness, Since: now, UpdatedAt: now},
	}}
	when := api.ResourceWhenSpec{All: []api.ResourceWhenSpec{
		{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master", MaxAge: "5s"}}},
		{State: map[string]api.StateMatchSpec{"ha.witness": {Equals: "leader", MaxAge: "5s"}}},
	}}
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "overlay"}, Spec: api.VXLANTunnelSpec{IfName: "vx-l2", VNI: vni, LocalAddress: local, Peers: []string{peer}, UnderlayInterface: "ul0", UDPPort: 4789, MTU: 1450, Bridge: "br-l2", When: when}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
	dir := filepath.Join(os.TempDir(), "routerd-netns-controller", ns)
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}
	c := chain.VXLANTunnelController{Router: router, DeclaredRouter: router, Store: s, OperatingSystem: platform.OSLinux, NetworkdDir: dir, StartedAt: now.Add(-time.Second)}
	c.Command = func(ctx context.Context, name string, args ...string) ([]byte, error) {
		// The namespace intentionally has no systemd-networkd. Runtime kernel
		// mutations still use real iproute2; only the persistence reload is a no-op.
		if name == "networkctl" {
			return nil, nil
		}
		out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
		if err != nil {
			return out, fmt.Errorf("%v: %w: %s", append([]string{name}, args...), err, out)
		}
		return out, nil
	}
	if err := c.Reconcile(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
