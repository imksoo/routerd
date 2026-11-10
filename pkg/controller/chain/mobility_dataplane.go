// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
)

func (c IPv4RouteController) reconcileMobilityRoutes(ctx context.Context) error {
	desired, err := normalizedMobilityRouteIntents(c.MobilityDataplane.Routes)
	if err != nil {
		return err
	}
	for _, route := range desired {
		if err := c.applyMobilityRoute(ctx, route); err != nil {
			return fmt.Errorf("%s: %w", route.ID, err)
		}
	}
	return nil
}

func (c IPv4RouteController) applyMobilityRoute(ctx context.Context, route dynamicconfig.MobilityIPv4RouteIntent) error {
	if c.DryRun {
		return nil
	}
	if !interfaceDevicePresent(ctx, route.Device) {
		return fmt.Errorf("device %q is not ready", route.Device)
	}
	preferredSource := route.PreferredSource
	if preferredSource != "" {
		local, err := ipv4PreferredSourceIsLocal(ctx, preferredSource)
		if err != nil {
			return fmt.Errorf("observe preferred source: %w", err)
		}
		if !local {
			preferredSource = ""
		}
	}
	if platform.CurrentOS() != platform.OSFreeBSD && ipv4RouteInstalled(ctx, "unicast", route.Destination, route.Device, "", preferredSource, route.Metric) {
		return nil
	}
	args := []string{"route", "replace", route.Destination, "dev", route.Device}
	if preferredSource != "" {
		args = append(args, "src", preferredSource)
	}
	if route.Metric > 0 {
		args = append(args, "metric", fmt.Sprintf("%d", route.Metric))
	}
	name := "ip"
	if platform.CurrentOS() == platform.OSFreeBSD {
		name, args = freeBSDIPv4RouteApplyCommand("unicast", route.Destination, route.Device, "", preferredSource)
	}
	out, err := runOutputCommandContext(ctx, name, args...)
	if err != nil && platform.CurrentOS() == platform.OSFreeBSD && freeBSDRouteNeedsAdd(out) {
		args = freeBSDIPv4RouteAddArgs(args)
		out, err = runOutputCommandContext(ctx, name, args...)
	}
	if err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func normalizedMobilityRouteIntents(intents []dynamicconfig.MobilityIPv4RouteIntent) ([]dynamicconfig.MobilityIPv4RouteIntent, error) {
	byID := map[string]dynamicconfig.MobilityIPv4RouteIntent{}
	for _, route := range intents {
		route.ID = strings.TrimSpace(route.ID)
		route.PoolRef = strings.TrimSpace(route.PoolRef)
		route.PoolPrefix = strings.TrimSpace(route.PoolPrefix)
		route.Destination = strings.TrimSpace(route.Destination)
		route.Device = strings.TrimSpace(route.Device)
		route.PreferredSource = strings.TrimSpace(route.PreferredSource)
		if route.ID == "" || route.PoolRef == "" || route.PoolPrefix == "" || route.Device == "" {
			return nil, fmt.Errorf("invalid mobility IPv4 route intent: id, poolRef, poolPrefix, and device are required")
		}
		if route.Purpose != dynamicconfig.MobilityIPv4RoutePurposeLocalInventory && route.Purpose != dynamicconfig.MobilityIPv4RoutePurposeCapturePrefix {
			return nil, fmt.Errorf("mobility IPv4 route %s has unsupported purpose %q", route.ID, route.Purpose)
		}
		scope, err := dynamicconfig.ParseCanonicalIPv4Prefix(route.PoolPrefix)
		if err != nil {
			return nil, fmt.Errorf("mobility IPv4 route %s has invalid pool prefix %q", route.ID, route.PoolPrefix)
		}
		prefix, err := netip.ParsePrefix(route.Destination)
		if err != nil || !prefix.Addr().Is4() || prefix.Masked().String() != route.Destination || prefix.Bits() < scope.Bits() || !scope.Contains(prefix.Addr()) {
			return nil, fmt.Errorf("mobility IPv4 route %s has invalid destination %q", route.ID, route.Destination)
		}
		if err := sam.ValidateCaptureInterface(route.Device); err != nil {
			return nil, fmt.Errorf("mobility IPv4 route %s device: %w", route.ID, err)
		}
		if route.PreferredSource != "" {
			address, err := netip.ParseAddr(route.PreferredSource)
			if err != nil || !address.Is4() || address.String() != route.PreferredSource || !scope.Contains(address) {
				return nil, fmt.Errorf("mobility IPv4 route %s has invalid preferred source %q", route.ID, route.PreferredSource)
			}
			route.PreferredSource = address.String()
		}
		if route.Metric < 0 {
			return nil, fmt.Errorf("mobility IPv4 route %s has negative metric", route.ID)
		}
		if previous, exists := byID[route.ID]; exists {
			if previous != route {
				return nil, fmt.Errorf("mobility IPv4 route intent %q is ambiguous", route.ID)
			}
			continue
		}
		byID[route.ID] = route
	}
	out := make([]dynamicconfig.MobilityIPv4RouteIntent, 0, len(byID))
	for _, route := range byID {
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (c IPv4StaticAddressController) reconcileMobilityStaticAddresses(ctx context.Context) error {
	desired, err := normalizedMobilityStaticAddressIntents(c.MobilityDataplane.StaticAddresses)
	if err != nil {
		return err
	}
	var failures []error
	for _, address := range desired {
		if err := c.applyMobilityStaticAddress(ctx, address); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", address.ID, err))
		}
	}
	return errors.Join(failures...)
}

func (c IPv4StaticAddressController) applyMobilityStaticAddress(ctx context.Context, address dynamicconfig.MobilityIPv4AddressIntent) error {
	if c.DryRun {
		return nil
	}
	if !interfaceDevicePresent(ctx, address.Interface) {
		return fmt.Errorf("device %q is not ready", address.Interface)
	}
	present, err := ipv4AddressPresent(ctx, address.Interface, address.Address)
	if err != nil {
		return err
	}
	if present {
		return nil
	}
	name, args := ipv4StaticAddressApplyCommand(platform.CurrentOS(), address.Interface, address.Address)
	if err := runCommandContext(ctx, name, args...); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func normalizedMobilityStaticAddressIntents(intents []dynamicconfig.MobilityIPv4AddressIntent) ([]dynamicconfig.MobilityIPv4AddressIntent, error) {
	byID := map[string]dynamicconfig.MobilityIPv4AddressIntent{}
	for _, address := range intents {
		address.ID = strings.TrimSpace(address.ID)
		address.PoolRef = strings.TrimSpace(address.PoolRef)
		address.PoolPrefix = strings.TrimSpace(address.PoolPrefix)
		address.Interface = strings.TrimSpace(address.Interface)
		address.Address = strings.TrimSpace(address.Address)
		if address.ID == "" || address.PoolRef == "" || address.PoolPrefix == "" || address.Interface == "" {
			return nil, fmt.Errorf("invalid mobility IPv4 address intent: id, poolRef, poolPrefix, and interface are required")
		}
		if address.Purpose != dynamicconfig.MobilityIPv4AddressPurposeCaptureSource {
			return nil, fmt.Errorf("mobility IPv4 address %s has unsupported purpose %q", address.ID, address.Purpose)
		}
		scope, err := dynamicconfig.ParseCanonicalIPv4Prefix(address.PoolPrefix)
		if err != nil {
			return nil, fmt.Errorf("mobility IPv4 address %s has invalid pool prefix %q", address.ID, address.PoolPrefix)
		}
		prefix, err := netip.ParsePrefix(address.Address)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() != 32 || prefix.Masked().String() != address.Address || !scope.Contains(prefix.Addr()) {
			return nil, fmt.Errorf("mobility IPv4 address %s has invalid host address %q", address.ID, address.Address)
		}
		if err := sam.ValidateCaptureInterface(address.Interface); err != nil {
			return nil, fmt.Errorf("mobility IPv4 address %s interface: %w", address.ID, err)
		}
		if previous, exists := byID[address.ID]; exists {
			if previous != address {
				return nil, fmt.Errorf("mobility IPv4 address intent %q is ambiguous", address.ID)
			}
			continue
		}
		byID[address.ID] = address
	}
	out := make([]dynamicconfig.MobilityIPv4AddressIntent, 0, len(byID))
	for _, address := range byID {
		out = append(out, address)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
