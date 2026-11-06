// SPDX-License-Identifier: BSD-3-Clause

package hostdeps

import (
	"reflect"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestLeaseSyncDerivesLinuxTransportPackages(t *testing.T) {
	resources := []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv4ServerLeaseSync"}, Spec: api.DHCPv4ServerLeaseSyncSpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6ServerLeaseSync"}, Spec: api.DHCPv6ServerLeaseSyncSpec{}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DHCPv6PrefixDelegationLeaseSync"}, Spec: api.DHCPv6PrefixDelegationLeaseSyncSpec{}},
	}
	for _, resource := range resources {
		t.Run(resource.Kind, func(t *testing.T) {
			router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{resource}}}
			packages := DerivedPackageResources(router)
			if len(packages) != 1 {
				t.Fatalf("lease sync must derive its clean-OS transport dependencies: %v", packages)
			}
			spec, err := packages[0].PackageSpec()
			if err != nil {
				t.Fatal(err)
			}
			for _, os := range []string{"ubuntu", "debian"} {
				found := false
				for _, set := range spec.Packages {
					if set.OS != os {
						continue
					}
					found = true
					if set.Manager != "apt" || !reflect.DeepEqual(set.Names, []string{"rsync", "openssh-client"}) {
						t.Errorf("%s lease sync transport packages = %+v", os, set)
					}
				}
				if !found {
					t.Errorf("missing %s package declaration", os)
				}
			}
		})
	}
}
