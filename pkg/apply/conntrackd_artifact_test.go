// SPDX-License-Identifier: BSD-3-Clause
package apply

import (
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/lifecycle"
	"github.com/imksoo/routerd/pkg/platform"
	"testing"
)

func TestConntrackdStockMaskIntent(t *testing.T) {
	r := api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "NAT44SessionSync"}, Metadata: api.ObjectMeta{Name: "sync"}, Spec: api.NAT44SessionSyncSpec{Mode: "conntrackd"}}
	for _, systemd := range []bool{true, false} {
		got := resourceArtifactIntentsForPlatform(r, nil, platform.OSLinux, platform.Features{HasSystemd: systemd})
		if !systemd {
			if len(got) != 0 {
				t.Fatal(got)
			}
			continue
		}
		if lifecycle.ArtifactCleanupEligible(got[0].Artifact) {
			t.Fatal("stock mask must not be automatically removed")
		}
		if len(got) != 1 || got[0].Artifact.Kind != "systemd.mask" || got[0].Artifact.Name != "conntrackd.service" || got[0].Artifact.Owner != r.ID() || got[0].Artifact.Attributes["retained"] != "true" {
			t.Fatal(got)
		}
	}
	r.Spec = api.NAT44SessionSyncSpec{Mode: "event-stream"}
	if got := resourceArtifactIntentsForPlatform(r, nil, platform.OSLinux, platform.Features{HasSystemd: true}); len(got) != 0 {
		t.Fatal(got)
	}
}
