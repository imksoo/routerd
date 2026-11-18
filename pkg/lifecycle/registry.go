// SPDX-License-Identifier: BSD-3-Clause

package lifecycle

import (
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func ResourceKey(apiVersion, kind, name string) string {
	return strings.TrimSpace(apiVersion) + "/" + strings.TrimSpace(kind) + "/" + strings.TrimSpace(name)
}

var internalVersionsByKind = map[string]string{
	"Inventory":         api.RouterAPIVersion,
	"Router":            api.RouterAPIVersion,
	"ServiceUnit":       api.SystemAPIVersion,
	"NetworkAdoption":   api.SystemAPIVersion,
	"KernelModule":      api.SystemAPIVersion,
	"ConntrackTuning":   api.SystemAPIVersion,
	"ConntrackObserver": api.NetAPIVersion,
}

func APIVersionForKind(kind string) string {
	if v := api.ConfigAPIVersionForKind(kind); v != "" {
		return v
	}
	return internalVersionsByKind[kind]
}
