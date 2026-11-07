// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

func (s eventedStore) ReadDHCPv6AddressRecord(name string) (routerstate.DHCPv6AddressRecord, error) {
	store, ok := s.Store.(dhcpv6AddressStore)
	if !ok {
		return routerstate.DHCPv6AddressRecord{}, fmt.Errorf("DHCPv6 native record reader unavailable")
	}
	return store.ReadDHCPv6AddressRecord(name)
}
func (s eventedStore) SaveDHCPv6AddressRecord(name string, expected routerstate.DHCPv6AddressRecord, next routerstate.DHCPv6AddressState) (routerstate.DHCPv6AddressRecord, error) {
	store, ok := s.Store.(dhcpv6AddressStore)
	if !ok {
		return routerstate.DHCPv6AddressRecord{}, fmt.Errorf("DHCPv6 native record writer unavailable")
	}
	saved, err := store.SaveDHCPv6AddressRecord(name, expected, next)
	if err != nil {
		return saved, err
	}
	if saved.Revision != expected.Revision && s.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "dhcpv6-address"}, "routerd.resource.status.changed", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "DHCPv6Address", Name: name}
		event.Attributes = map[string]string{"phase": next.Phase, "previousPhase": expected.State.Phase, "changedFields": "address,addresses,phase,reason"}
		err = s.Bus.Publish(context.Background(), event)
	}
	return saved, err
}
func (s eventedStore) DeleteDHCPv6AddressRecord(name string, expected routerstate.DHCPv6AddressRecord) error {
	store, ok := s.Store.(dhcpv6AddressStore)
	if !ok {
		return fmt.Errorf("DHCPv6 native record deletion unavailable")
	}
	return store.DeleteDHCPv6AddressRecord(name, expected)
}
