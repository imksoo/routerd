// SPDX-License-Identifier: BSD-3-Clause

package config

import (
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

func TestDerivedEventAdmissionRejectsNegativeHysteresis(t *testing.T) {
	for _, hysteresis := range []string{"", "0s", "1s", "-1s"} {
		router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "derived"}, Spec: api.RouterSpec{Resources: []api.Resource{testResource(api.NetAPIVersion, "DerivedEvent", "test", api.DerivedEventSpec{Topic: "routerd.derived", Inputs: []api.ReadyWhenSpec{{Field: "ready", Equals: "ready"}}, Hysteresis: hysteresis})}}}
		err := ValidateForOS(router, platform.OSLinux)
		if hysteresis == "-1s" {
			if err == nil || !strings.Contains(err.Error(), "hysteresis must be non-negative") {
				t.Fatalf("negative hysteresis admitted: %v", err)
			}
		} else if err != nil {
			t.Fatalf("valid hysteresis %q rejected: %v", hysteresis, err)
		}
	}
}
