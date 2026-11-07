// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNTPPackagesFollowManagedProvider(t *testing.T) {
	for _, tc := range []struct {
		name, client, server string
		managed, withServer  bool
		linux, freebsd       []string
	}{
		{name: "timesyncd", client: "systemd-timesyncd", managed: true, linux: []string{"systemd-timesyncd"}},
		{name: "default client", managed: true, linux: []string{"systemd-timesyncd"}},
		{name: "chrony client", client: "chrony", managed: true, linux: []string{"chrony"}, freebsd: []string{"chrony"}},
		{name: "shared chrony", client: "systemd-timesyncd", server: "chrony", managed: true, withServer: true, linux: []string{"chrony"}, freebsd: []string{"chrony"}},
		{name: "default shared provider", managed: true, withServer: true, linux: []string{"chrony"}},
		{name: "observed client", client: "systemd-timesyncd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPClient"}, Metadata: api.ObjectMeta{Name: "client"}, Spec: api.NTPClientSpec{Provider: tc.client, Managed: tc.managed}}}}}
			if tc.withServer {
				router.Spec.Resources = append(router.Spec.Resources, api.Resource{TypeMeta: api.TypeMeta{APIVersion: api.SystemAPIVersion, Kind: "NTPServer"}, Metadata: api.ObjectMeta{Name: "server"}, Spec: api.NTPServerSpec{Provider: tc.server, Managed: true}})
			}
			sets := PackageSets(router)
			if len(sets) != 3 {
				t.Fatalf("missing OS package plans: %v", sets)
			}
			for _, set := range sets {
				want := tc.linux
				if set.OS == "freebsd" {
					want = tc.freebsd
				}
				if !reflect.DeepEqual(set.Names, want) {
					t.Errorf("%s packages = %v, want %v", set.OS, set.Names, want)
				}
			}
		})
	}
}
