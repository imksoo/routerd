// SPDX-License-Identifier: BSD-3-Clause

package resourcequery

import (
	"strconv"
	"time"

	"github.com/imksoo/routerd/internal/statusvalue"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
)

// InterfaceMTU returns the configured MTU or a valid DHCP-provided override.
func InterfaceMTU(router *api.Router, store Store, resource api.Resource, spec api.InterfaceSpec, now time.Time) (int, bool, error) {
	mtu := spec.MTU
	for _, client := range router.Spec.Resources {
		if client.Kind != "DHCPv4Client" {
			continue
		}
		clientSpec, err := client.DHCPv4ClientSpec()
		if err != nil {
			return 0, false, err
		}
		if !clientSpec.UseMTU || (clientSpec.Interface != resource.Metadata.Name && clientSpec.Interface != spec.IfName) {
			continue
		}
		if store == nil {
			return mtu, false, nil
		}
		lease := store.ObjectStatus(api.NetAPIVersion, "DHCPv4Client", client.Metadata.Name)
		expires, err := time.Parse(time.RFC3339Nano, statusvalue.Field(lease, "expiresAt"))
		if err != nil || !now.Before(expires) {
			continue
		}
		switch statusvalue.Field(lease, "phase") {
		case daemonapi.ResourcePhaseBound, daemonapi.ResourcePhaseRefreshing, daemonapi.ResourcePhaseRebinding:
		default:
			continue
		}
		if offered, err := strconv.Atoi(statusvalue.Field(lease, "mtu")); err == nil && offered >= 68 {
			mtu = offered
		}
	}
	return mtu, true, nil
}
