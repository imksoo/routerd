// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"github.com/imksoo/routerd/pkg/api"
)

func runtimeDnsmasqServicePath(path string) string {
	return path
}

func packageSetForOSMain(spec api.PackageSpec, osName string) (api.OSPackageSetSpec, bool) {
	for _, set := range spec.Packages {
		if set.OS == osName {
			return set, true
		}
	}
	return api.OSPackageSetSpec{}, false
}
