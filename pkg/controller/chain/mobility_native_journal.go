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
		if address.Operation == "" {
			return nil, fmt.Errorf("pending mobility address has no operation")
		}
	}
	return pending, nil
}

// A pending write is an uncertain operation, not an acquisition receipt.
// Presence cannot prove which writer installed the tuple. Only an authoritative
// absence observation permits retiring pending work; never delete it to recover.
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
		if route.Scope == "" || route.IfIndex < 1 {
			return fmt.Errorf("pending mobility route %s has no native-world receipt", route.ID)
		}
		if err := c.verifyOrdinaryRouteIdentity(ctx, mobilityRouteReceipt(route)); err != nil {
			return fmt.Errorf("pending mobility route %s identity is unconfirmed: %w", route.ID, err)
		}
		matching, _, err := c.observeOrdinaryRoute(ctx, mobilityRouteRemoval(route))
		if err != nil {
			return fmt.Errorf("observe pending mobility route %s: %w", route.ID, err)
		}
		if err := c.verifyOrdinaryRouteIdentity(ctx, mobilityRouteReceipt(route)); err != nil {
			return err
		}
		if matching {
			return fmt.Errorf("pending mobility route %s is present without acknowledged ownership; inspect and remove it before retrying", route.ID)
		}
		if err := c.finishMobilityRoute(route, false, true); err != nil {
			return err
		}
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
	for _, a := range pending {
		observed, err := c.verifyMobilityAddressIdentity(ctx, a)
		if err != nil {
			return err
		}
		if observed.has(a.Address) {
			return fmt.Errorf("pending mobility address %s is present without acknowledged ownership; inspect and remove it before retrying", a.ID)
		}
		if err := c.confirmMobilityAddress(a, false, true); err != nil {
			return err
		}
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
