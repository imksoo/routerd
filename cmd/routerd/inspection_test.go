// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestServeInspectionResourcesIncludesDerivedRuntimePackage(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{
		TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
		Metadata: api.ObjectMeta{Name: "wg-sam"},
		Spec:     api.WireGuardInterfaceSpec{},
	}}}}

	resources, err := serveInspectionResources(router, nil, "Package/router-runtime", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 {
		t.Fatalf("resources len = %d, want 1", len(resources))
	}
	if resources[0].APIVersion != api.SystemAPIVersion || resources[0].Kind != "Package" || resources[0].Name != "router-runtime" {
		t.Fatalf("resource = %#v, want system Package/router-runtime", resources[0])
	}
	spec, ok := resources[0].Spec.(api.PackageSpec)
	if !ok {
		t.Fatalf("spec type = %T, want api.PackageSpec", resources[0].Spec)
	}
	ubuntu := api.OSPackageSetSpec{}
	for _, set := range spec.Packages {
		if set.OS == "ubuntu" {
			ubuntu = set
		}
	}
	if !stringInSlice(ubuntu.Names, "wireguard-tools") {
		t.Fatalf("ubuntu package names = %#v, want wireguard-tools", ubuntu.Names)
	}
}

func stringInSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
