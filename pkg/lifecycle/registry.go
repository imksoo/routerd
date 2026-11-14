// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func ResourceKey(apiVersion, kind, name string) string {
	return strings.TrimSpace(apiVersion) + "/" + strings.TrimSpace(kind) + "/" + strings.TrimSpace(name)
}

// The public Kind definitions are indexed once; status cleanup must not rebuild
// and scan the entire schema list for each object.
var apiVersionsByKind = func() map[string]string {
	versions := map[string]string{
		"Inventory":         api.RouterAPIVersion,
		"Router":            api.RouterAPIVersion,
		"ServiceUnit":       api.SystemAPIVersion,
		"NetworkAdoption":   api.SystemAPIVersion,
		"KernelModule":      api.SystemAPIVersion,
		"ConntrackTuning":   api.SystemAPIVersion,
		"ConntrackObserver": api.NetAPIVersion,
	}
	for _, resource := range api.ConfigResourceKinds() {
		versions[resource.Kind] = resource.APIVersion
	}
	return versions
}()

func APIVersionForKind(kind string) string {
	return apiVersionsByKind[kind]
}
