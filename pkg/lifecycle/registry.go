// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func ResourceKey(apiVersion, kind, name string) string {
	return strings.TrimSpace(apiVersion) + "/" + strings.TrimSpace(kind) + "/" + strings.TrimSpace(name)
}

func APIVersionForKind(kind string) string {
	for _, resource := range api.ConfigResourceKinds() {
		if resource.Kind == kind {
			return resource.APIVersion
		}
	}
	switch kind {
	case "Inventory", "Router":
		return api.RouterAPIVersion
	case "ServiceUnit", "NetworkAdoption", "KernelModule", "ConntrackTuning":
		return api.SystemAPIVersion
	case "ConntrackObserver":
		return api.NetAPIVersion
	default:
		return ""
	}
}
