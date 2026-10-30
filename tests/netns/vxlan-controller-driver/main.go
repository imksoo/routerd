// SPDX-License-Identifier: BSD-3-Clause

// vxlan-controller-driver is an integration-only adapter that runs the
// production shared VXLAN native owner against an isolated network namespace.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/chain"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"github.com/imksoo/routerd/pkg/vxlan"
)

func main() {
	if len(os.Args) != 7 {
		panic("usage: driver NETNS ROLE WITNESS LOCAL PEER VNI")
	}
	ns, role, witness, local, peer := os.Args[1], os.Args[2], os.Args[3], os.Args[4], os.Args[5]
	vni, err := strconv.Atoi(os.Args[6])
	if err != nil {
		panic(err)
	}
	current, err := os.Readlink("/proc/self/ns/net")
	if err != nil {
		panic(err)
	}
	host, err := os.Readlink("/proc/1/ns/net")
	if err != nil || current == host {
		panic("VXLAN test driver requires a separate network namespace")
	}
	now := time.Now().UTC()
	when := api.ResourceWhenSpec{All: []api.ResourceWhenSpec{
		{State: map[string]api.StateMatchSpec{"ha.role": {Equals: "master", MaxAge: "5s"}}},
		{State: map[string]api.StateMatchSpec{"ha.witness": {Equals: "leader", MaxAge: "5s"}}},
	}}
	resource := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "overlay"}, Spec: api.VXLANTunnelSpec{IfName: "vx-l2", VNI: vni, LocalAddress: local, Peers: []string{peer}, UnderlayInterface: "ul0", UDPPort: 4789, MTU: 1450, Bridge: "br-l2", When: when}}
	// Declarations resolve the actual existing fixture interfaces. This driver
	// does not run another Bridge owner or generate boot configuration.
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "ul0"}, Spec: api.InterfaceSpec{IfName: "ul0"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br-l2"}, Spec: api.BridgeSpec{IfName: "br-l2"}},
		resource,
	}}}
	dir := filepath.Join(os.TempDir(), "routerd-netns-controller", ns)
	if err := os.MkdirAll(dir, 0755); err != nil {
		panic(err)
	}
	s, err := routerstate.OpenSQLite(filepath.Join(dir, "state.db"))
	if err != nil {
		panic(err)
	}
	defer s.Close()
	if _, err := s.Set("ha.role", role, "netns fixture"); err != nil {
		panic(err)
	}
	if _, err := s.Set("ha.witness", witness, "netns fixture"); err != nil {
		panic(err)
	}
	// The process owns an immutable configuration; live role/witness state
	// remains in SQLite and is rechecked by the actual native owner after waits.
	// StateDir holds the same kernel-scope receipts across driver invocations.
	c := chain.VXLANSegmentController{Router: router, Store: s, StateDir: dir, StartedAt: now.Add(-time.Second), Admit: func(ctx context.Context, id string, _ vxlan.SegmentConfig) (bool, error) {
		return id == resource.ID(), ctx.Err()
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := c.Reconcile(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
