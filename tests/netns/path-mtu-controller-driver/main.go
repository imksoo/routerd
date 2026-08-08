// SPDX-License-Identifier: BSD-3-Clause

// path-mtu-controller-driver exercises the production PathMTU controller in a
// network namespace. It intentionally contains no nft rendering of its own.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controller/chain"
	"github.com/imksoo/routerd/pkg/platform"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintln(os.Stderr, "usage: driver enable|disable nft-path owner-path")
		os.Exit(2)
	}
	enabled := os.Args[1] == "enable"
	mtu := 1280
	if raw := os.Getenv("ROUTERD_NETNS_MTU"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		mtu = parsed
	}
	r := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "underlay", MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 1128, LocalAddress: "198.18.0.1", Peers: []string{"198.18.0.2"}, UnderlayInterface: "underlay", Bridge: "br", MTU: mtu, TCPMSSClamp: enabled}},
	}}}
	c := chain.PathMTUController{Router: r, OS: platform.OSLinux, L2Path: os.Args[2], L2OwnerPath: os.Args[3], Path: os.Args[2] + ".inet", ForceFragmentPath: os.Args[2] + ".frag"}
	if nft := os.Getenv("ROUTERD_NETNS_NFT"); nft != "" {
		c.NftCommand = nft
	}
	if token := os.Getenv("ROUTERD_NETNS_TEST_TOKEN"); token != "" {
		c.RandomToken = func() (string, error) { return token, nil }
	}
	if point := os.Getenv("ROUTERD_NETNS_FAILPOINT"); point != "" {
		c.L2Failpoint = func(got string) error {
			if got == point {
				return fmt.Errorf("injected L2 failpoint %s", got)
			}
			return nil
		}
	}
	if err := c.Reconcile(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
