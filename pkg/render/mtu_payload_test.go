// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func TestForwardedMSSUsesNativeTunnelPayloadMTUOnce(t *testing.T) {
	for _, explicit := range []int{0, 1200} {
		router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
			netResource("WireGuardInterface", "wg", api.WireGuardInterfaceSpec{MTU: 1420}),
			{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"}, Metadata: api.ObjectMeta{Name: "gre"}, Spec: api.TunnelInterfaceSpec{Mode: "gre", UnderlayInterface: "wg", MTU: explicit}},
		}}}
		data, err := NftablesTCPMSSClampWithLocalCaptureIntents(router, []dynamicconfig.LocalCaptureIntent{{ID: "capture", PoolRef: "pool", Address: "192.0.2.10/32", Disposition: dynamicconfig.CaptureProtectExisting, CaptureInterface: "ens3", TunnelInterfaces: []string{"gre"}}})
		if err != nil {
			t.Fatalf("IPv4 tunnel MTU %d rejected: %v", explicit, err)
		}
		want := "maxseg size > 1356 tcp option maxseg size set 1356" // Native GRE: 1420 - 24; IPv4/TCP: 1396 - 40.
		if explicit == 1200 {
			want = "maxseg size > 1160 tcp option maxseg size set 1160"
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("encapsulation was subtracted twice or native MTU ignored:\n%s", data)
		}
	}
}
