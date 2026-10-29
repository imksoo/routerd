// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func pendingMobilityRoutes(status map[string]any) ([]mobilityAppliedIPv4Route, error) {
	if !statusValuePresent(status, "pendingMobilityRoutes") {
		return nil, nil
	}
	var pending []mobilityAppliedIPv4Route
	if !decodeAppliedDataplaneStatus(status["pendingMobilityRoutes"], &pending) || pending == nil {
		return nil, fmt.Errorf("invalid pending mobility routes")
	}
	for _, route := range pending {
		if err := validateAppliedMobilityRoute(route); err != nil {
			return nil, err
		}
	}
	return pending, nil
}

func pendingMobilityAddresses(status map[string]any) ([]mobilityAppliedIPv4Address, error) {
	if !statusValuePresent(status, "pendingMobilityStaticAddresses") {
		return nil, nil
	}
	var pending []mobilityAppliedIPv4Address
	if !decodeAppliedDataplaneStatus(status["pendingMobilityStaticAddresses"], &pending) || pending == nil {
		return nil, fmt.Errorf("invalid pending mobility addresses")
	}
	for _, address := range pending {
		if err := validateAppliedMobilityStaticAddress(address); err != nil {
			return nil, err
		}
	}
	return pending, nil
}

// Pending writes are recovered before a newer desired snapshot is applied.
// Deleting a pending tuple is safe only through the same exact native identity
// observation as ordinary owned-route removal. A foreign replacement survives.
func (c IPv4RouteController) beginMobilityRoutes(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	if c.Store == nil && len(c.MobilityDataplane.Routes) == 0 && len(c.MobilityDataplane.Withdrawals) == 0 {
		return nil
	}
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return err
	}
	pending, err := pendingMobilityRoutes(status)
	if err != nil {
		return err
	}
	if len(pending) == 0 && len(c.MobilityDataplane.Routes) == 0 && len(c.MobilityDataplane.Withdrawals) == 0 && !statusValuePresent(status, dynamicconfig.MobilityRouteWithdrawalAcks) {
		return nil
	}
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("mobility route ownership merger unavailable")
	}
	if err := merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{dynamicconfig.MobilityRouteWithdrawalAcks: []dynamicconfig.MobilityWithdrawalRequest{}}); err != nil {
		return err
	}
	for _, route := range pending {
		if err := c.removeMobilityRoute(ctx, route); err != nil {
			return fmt.Errorf("recover pending mobility route %s: %w", route.ID, err)
		}
	}
	if len(pending) != 0 {
		return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"pendingMobilityRoutes": []mobilityAppliedIPv4Route{}})
	}
	return nil
}

func (c IPv4RouteController) stageMobilityRoute(route mobilityAppliedIPv4Route) error {
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return err
	}
	pending, err := pendingMobilityRoutes(status)
	if err != nil {
		return err
	}
	for _, previous := range pending {
		if previous == route {
			return nil
		}
	}
	pending = append(pending, route)
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("mobility route ownership merger unavailable")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"pendingMobilityRoutes": pending, dynamicconfig.MobilityRouteWithdrawalAcks: []dynamicconfig.MobilityWithdrawalRequest{}})
}

func (c IPv4RouteController) mobilityRouteOwned(ctx context.Context, wanted mobilityAppliedIPv4Route) (bool, bool, error) {
	previous, err := c.appliedMobilityRoutes()
	if err != nil {
		return false, false, err
	}
	for _, route := range previous {
		oldKey, newKey := mobilityRouteRemoval(route), mobilityRouteRemoval(wanted)
		if route.ID != wanted.ID || route.PoolRef != wanted.PoolRef || route.PoolPrefix != wanted.PoolPrefix || oldKey.Destination != newKey.Destination || oldKey.Metric != newKey.Metric {
			continue
		}
		matching, _, err := c.observeOrdinaryRoute(ctx, mobilityRouteRemoval(route))
		if err != nil {
			return false, false, err
		}
		if matching {
			return true, route == wanted, nil
		}
	}
	return false, false, nil
}

func (c IPv4StaticAddressController) beginMobilityAddresses(ctx context.Context) error {
	if c.DryRun {
		return nil
	}
	if c.Store == nil && len(c.MobilityDataplane.StaticAddresses) == 0 && len(c.MobilityDataplane.Withdrawals) == 0 {
		return nil
	}
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return err
	}
	pending, err := pendingMobilityAddresses(status)
	if err != nil {
		return err
	}
	if len(pending) == 0 && len(c.MobilityDataplane.StaticAddresses) == 0 && len(c.MobilityDataplane.Withdrawals) == 0 && !statusValuePresent(status, dynamicconfig.MobilityAddressWithdrawalAcks) {
		return nil
	}
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("mobility address ownership merger unavailable")
	}
	if err := merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{dynamicconfig.MobilityAddressWithdrawalAcks: []dynamicconfig.MobilityWithdrawalRequest{}}); err != nil {
		return err
	}
	for _, address := range pending {
		if mobilityStaticAddressDesiredByUser(c.Router, address.Interface, address.Address) {
			return fmt.Errorf("pending mobility address %s is also desired by a user resource", address.Address)
		}
		if err := c.removeMobilityStaticAddress(ctx, address); err != nil {
			return fmt.Errorf("recover pending mobility address %s: %w", address.ID, err)
		}
	}
	if len(pending) != 0 {
		return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"pendingMobilityStaticAddresses": []mobilityAppliedIPv4Address{}})
	}
	return nil
}

func (c IPv4StaticAddressController) stageMobilityAddress(address mobilityAppliedIPv4Address) error {
	status, err := readMobilityNativeStatus(c.Store)
	if err != nil {
		return err
	}
	pending, err := pendingMobilityAddresses(status)
	if err != nil {
		return err
	}
	for _, previous := range pending {
		if previous == address {
			return nil
		}
	}
	pending = append(pending, address)
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("mobility address ownership merger unavailable")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{"pendingMobilityStaticAddresses": pending, dynamicconfig.MobilityAddressWithdrawalAcks: []dynamicconfig.MobilityWithdrawalRequest{}})
}

func (c IPv4StaticAddressController) mobilityAddressOwned(wanted mobilityAppliedIPv4Address) (bool, error) {
	previous, err := c.appliedMobilityStaticAddresses()
	if err != nil {
		return false, err
	}
	for _, address := range previous {
		if address == wanted {
			return true, nil
		}
	}
	return false, nil
}
