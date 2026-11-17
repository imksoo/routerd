// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestFreeBSDRenderSkipsIdentityOnlyEventFederationRCD(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.FederationAPIVersion, Kind: "EventGroup"},
			Metadata: api.ObjectMeta{Name: "identity"},
			Spec: api.EventGroupSpec{
				NodeName: "freebsd-router",
			},
		},
	}}}
	cfg, err := FreeBSD(router)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(cfg.RCConf), "routerd_eventd_identity") {
		t.Fatalf("rc.conf should not enable identity-only eventd:\n%s", cfg.RCConf)
	}
	if _, ok := cfg.RCDScripts["routerd_eventd_identity"]; ok {
		t.Fatalf("identity-only eventd rc.d script was rendered: %#v", cfg.RCDScripts)
	}
}
