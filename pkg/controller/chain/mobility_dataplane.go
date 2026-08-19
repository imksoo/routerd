// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
)

// mobilityAppliedIPv4Route is an observed local effect, not an alternate
// desired-state channel. Its only purpose is exact, safe withdrawal of a
// route which the typed MobilityDataplane plan previously installed.
type mobilityAppliedIPv4Route struct {
	ID              string `json:"id"`
	PoolRef         string `json:"poolRef"`
	PoolPrefix      string `json:"poolPrefix"`
	Purpose         string `json:"purpose"`
	Destination     string `json:"destination"`
	Device          string `json:"device"`
	PreferredSource string `json:"preferredSource,omitempty"`
	Metric          int    `json:"metric,omitempty"`
}

// mobilityAppliedIPv4Address is the corresponding exact applied-address
// ledger. It is intentionally independent from user-authored
// IPv4StaticAddress object status.
type mobilityAppliedIPv4Address struct {
	ID         string `json:"id"`
	PoolRef    string `json:"poolRef"`
	PoolPrefix string `json:"poolPrefix"`
	Purpose    string `json:"purpose"`
	Interface  string `json:"interface"`
	Address    string `json:"address"`
}

func (c IPv4RouteController) reconcileMobilityRoutes(ctx context.Context) error {
	desired, err := normalizedMobilityRouteIntents(c.MobilityDataplane.Routes)
	if err != nil {
		return err
	}
	previous, err := c.appliedMobilityRoutes()
	if err != nil {
		return fmt.Errorf("decode applied mobility IPv4 route ledger: %w", err)
	}
	if len(desired) == 0 && len(previous) == 0 {
		return nil
	}
	if (len(desired) != 0 || len(previous) != 0) && !c.DryRun {
		if _, ok := c.Store.(objectStatusMerger); !ok {
			return fmt.Errorf("mobility IPv4 route effects require an applied-effect status merger")
		}
	}
	previousByID := make(map[string]mobilityAppliedIPv4Route, len(previous))
	for _, route := range previous {
		previousByID[route.ID] = route
	}

	next := map[string]mobilityAppliedIPv4Route{}
	failed := map[string]bool{}
	var failures []string
	for _, route := range desired {
		applied, err := c.applyMobilityRoute(ctx, route)
		if err != nil {
			failed[route.ID] = true
			if previous, ok := previousByID[route.ID]; ok && mobilityRouteMatchesIntent(previous, route) {
				next[route.ID] = previous
			}
			failures = append(failures, fmt.Sprintf("%s: %v", route.ID, err))
			continue
		}
		next[route.ID] = applied
	}
	for _, applied := range previous {
		if current, ok := next[applied.ID]; ok && mobilityRoutesEqual(applied, current) {
			continue
		}
		// `ip route replace` updates preferred source in place for an otherwise
		// identical route. Deleting the old ledger tuple afterward would use the
		// same destination/device/metric selector and remove that replacement.
		if current, ok := next[applied.ID]; ok && mobilityRouteReplacedInPlace(applied, current) {
			continue
		}
		// If replacement could not be installed, retain the precisely recorded
		// old route. Removing it would turn a transient local failure into a
		// needless traffic interruption.
		if failed[applied.ID] {
			next[applied.ID] = applied
			continue
		}
		if err := c.removeMobilityRoute(ctx, applied); err != nil {
			next[applied.ID] = applied
			failures = append(failures, fmt.Sprintf("remove %s: %v", applied.ID, err))
		}
	}
	if err := c.saveAppliedMobilityRoutes(next); err != nil {
		return err
	}
	if len(failures) != 0 {
		return fmt.Errorf("reconcile mobility IPv4 routes: %s", strings.Join(failures, "; "))
	}
	return nil
}

// cleanupRemovedMobilityRoutes performs the withdrawal half before SAM tears
// down proxy-neighbor state. FreeBSD routeguard requires this ordering.
func (c IPv4RouteController) cleanupRemovedMobilityRoutes(ctx context.Context) error {
	desired, err := normalizedMobilityRouteIntents(c.MobilityDataplane.Routes)
	if err != nil {
		return err
	}
	desiredByID := make(map[string]bool, len(desired))
	for _, route := range desired {
		desiredByID[route.ID] = true
	}
	previous, err := c.appliedMobilityRoutes()
	if err != nil {
		return fmt.Errorf("decode applied mobility IPv4 route ledger: %w", err)
	}
	if len(previous) == 0 {
		return nil
	}
	if !c.DryRun {
		if _, ok := c.Store.(objectStatusMerger); !ok {
			return fmt.Errorf("mobility IPv4 route effects require an applied-effect status merger")
		}
	}
	next := map[string]mobilityAppliedIPv4Route{}
	var failures []string
	for _, applied := range previous {
		if desiredByID[applied.ID] {
			next[applied.ID] = applied
			continue
		}
		if err := c.removeMobilityRoute(ctx, applied); err != nil {
			next[applied.ID] = applied
			failures = append(failures, fmt.Sprintf("remove %s: %v", applied.ID, err))
		}
	}
	if err := c.saveAppliedMobilityRoutes(next); err != nil {
		return err
	}
	if len(failures) != 0 {
		return fmt.Errorf("withdraw mobility IPv4 routes: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (c IPv4RouteController) applyMobilityRoute(ctx context.Context, route dynamicconfig.MobilityIPv4RouteIntent) (mobilityAppliedIPv4Route, error) {
	applied := mobilityAppliedIPv4Route{
		ID:          route.ID,
		PoolRef:     route.PoolRef,
		PoolPrefix:  route.PoolPrefix,
		Purpose:     string(route.Purpose),
		Destination: route.Destination,
		Device:      route.Device,
		Metric:      route.Metric,
	}
	if c.DryRun {
		return applied, nil
	}
	devicePresent := c.DevicePresent
	if devicePresent == nil {
		devicePresent = interfaceDevicePresent
	}
	if !devicePresent(ctx, route.Device) {
		return mobilityAppliedIPv4Route{}, fmt.Errorf("device %q is not ready", route.Device)
	}
	preferredSource := route.PreferredSource
	if preferredSource != "" && !ipv4PreferredSourceIsLocal(ctx, c.run, preferredSource) {
		preferredSource = ""
	}
	applied.PreferredSource = preferredSource
	if platform.CurrentOS() != platform.OSFreeBSD && ipv4RouteInstalled(ctx, c.run, "unicast", route.Destination, route.Device, "", preferredSource, route.Metric) {
		return applied, nil
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
	out, err := c.run(ctx, name, args...)
	if err != nil && platform.CurrentOS() == platform.OSFreeBSD && freeBSDRouteNeedsAdd(out) {
		args = freeBSDIPv4RouteAddArgs(args)
		out, err = c.run(ctx, name, args...)
	}
	if err != nil {
		return mobilityAppliedIPv4Route{}, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return applied, nil
}

func (c IPv4RouteController) removeMobilityRoute(ctx context.Context, route mobilityAppliedIPv4Route) error {
	if c.DryRun {
		return nil
	}
	removal := ipv4RouteRemoval{
		Type:            "unicast",
		Destination:     route.Destination,
		Device:          route.Device,
		PreferredSource: route.PreferredSource,
		Metric:          fmt.Sprintf("%d", route.Metric),
	}
	args := ipv4RouteRemovalDeleteArgs(removal)
	if len(args) == 0 {
		return nil
	}
	name := "ip"
	if platform.CurrentOS() == platform.OSFreeBSD {
		name, args = freeBSDIPv4RouteRemovalDeleteCommand(removal)
	} else if removedIPv4RouteRemovalIsCurrentlyBGP(ctx, c.run, removal) {
		return nil
	}
	out, err := c.run(ctx, name, args...)
	if err != nil && !missingIPv4RouteDelete(err, out) {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c IPv4RouteController) appliedMobilityRoutes() ([]mobilityAppliedIPv4Route, error) {
	if c.Store == nil {
		return nil, nil
	}
	status := c.Store.ObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	if !statusValuePresent(status, "appliedMobilityRoutes") {
		return nil, nil
	}
	var decoded []mobilityAppliedIPv4Route
	if !decodeAppliedDataplaneStatus(status["appliedMobilityRoutes"], &decoded) || decoded == nil {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	byID := map[string]mobilityAppliedIPv4Route{}
	for _, route := range decoded {
		if err := validateAppliedMobilityRoute(route); err != nil {
			return nil, err
		}
		if previous, exists := byID[route.ID]; exists {
			if previous != route {
				return nil, fmt.Errorf("conflicting mobility IPv4 route ledger entries for id %q", route.ID)
			}
			continue
		}
		byID[route.ID] = route
	}
	return sortedMobilityAppliedRoutes(byID), nil
}

func (c IPv4RouteController) saveAppliedMobilityRoutes(routes map[string]mobilityAppliedIPv4Route) error {
	if c.DryRun || c.Store == nil {
		return nil
	}
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("mobility IPv4 route effects require an applied-effect status merger")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{
		"appliedMobilityRoutes": sortedMobilityAppliedRoutes(routes),
	})
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

func mobilityRouteMatchesIntent(applied mobilityAppliedIPv4Route, intent dynamicconfig.MobilityIPv4RouteIntent) bool {
	return applied.ID == intent.ID && applied.PoolRef == intent.PoolRef && applied.PoolPrefix == intent.PoolPrefix && applied.Purpose == string(intent.Purpose) &&
		applied.Destination == intent.Destination && applied.Device == intent.Device && applied.Metric == intent.Metric
}

func mobilityRoutesEqual(left, right mobilityAppliedIPv4Route) bool {
	return left == right
}

func mobilityRouteReplacedInPlace(previous, current mobilityAppliedIPv4Route) bool {
	return previous.ID == current.ID && previous.Destination == current.Destination &&
		previous.Device == current.Device && previous.Metric == current.Metric
}

func sortedMobilityAppliedRoutes(routes map[string]mobilityAppliedIPv4Route) []mobilityAppliedIPv4Route {
	out := make([]mobilityAppliedIPv4Route, 0, len(routes))
	for _, route := range routes {
		out = append(out, route)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validateAppliedMobilityRoute(route mobilityAppliedIPv4Route) error {
	intent := dynamicconfig.MobilityIPv4RouteIntent{
		ID:              route.ID,
		PoolRef:         route.PoolRef,
		PoolPrefix:      route.PoolPrefix,
		Purpose:         dynamicconfig.MobilityIPv4RoutePurpose(route.Purpose),
		Destination:     route.Destination,
		Device:          route.Device,
		PreferredSource: route.PreferredSource,
		Metric:          route.Metric,
	}
	_, err := normalizedMobilityRouteIntents([]dynamicconfig.MobilityIPv4RouteIntent{intent})
	return err
}

func (c IPv4StaticAddressController) reconcileMobilityStaticAddresses(ctx context.Context) error {
	desired, err := normalizedMobilityStaticAddressIntents(c.MobilityDataplane.StaticAddresses)
	if err != nil {
		return err
	}
	previous, err := c.appliedMobilityStaticAddresses()
	if err != nil {
		return fmt.Errorf("decode applied mobility IPv4 address ledger: %w", err)
	}
	if len(desired) == 0 && len(previous) == 0 {
		return nil
	}
	if (len(desired) != 0 || len(previous) != 0) && !c.DryRun {
		if _, ok := c.Store.(objectStatusMerger); !ok {
			return fmt.Errorf("mobility IPv4 address effects require an applied-effect status merger")
		}
	}
	previousByID := make(map[string]mobilityAppliedIPv4Address, len(previous))
	for _, address := range previous {
		previousByID[address.ID] = address
	}
	next := map[string]mobilityAppliedIPv4Address{}
	failed := map[string]bool{}
	var failures []string
	for _, address := range desired {
		applied, err := c.applyMobilityStaticAddress(ctx, address)
		if err != nil {
			failed[address.ID] = true
			if previous, ok := previousByID[address.ID]; ok && mobilityStaticAddressMatchesIntent(previous, address) {
				next[address.ID] = previous
			}
			failures = append(failures, fmt.Sprintf("%s: %v", address.ID, err))
			continue
		}
		next[address.ID] = applied
	}
	for _, applied := range previous {
		if current, ok := next[applied.ID]; ok && mobilityStaticAddressesEqual(applied, current) {
			continue
		}
		if failed[applied.ID] || mobilityStaticAddressDesiredByUser(c.Router, applied.Interface, applied.Address) {
			next[applied.ID] = applied
			continue
		}
		if err := c.removeMobilityStaticAddress(ctx, applied); err != nil {
			next[applied.ID] = applied
			failures = append(failures, fmt.Sprintf("remove %s: %v", applied.ID, err))
		}
	}
	if err := c.saveAppliedMobilityStaticAddresses(next); err != nil {
		return err
	}
	if len(failures) != 0 {
		return fmt.Errorf("reconcile mobility IPv4 addresses: %s", strings.Join(failures, "; "))
	}
	return nil
}

func (c IPv4StaticAddressController) applyMobilityStaticAddress(ctx context.Context, address dynamicconfig.MobilityIPv4AddressIntent) (mobilityAppliedIPv4Address, error) {
	applied := mobilityAppliedIPv4Address{ID: address.ID, PoolRef: address.PoolRef, PoolPrefix: address.PoolPrefix, Purpose: string(address.Purpose), Interface: address.Interface, Address: address.Address}
	if c.DryRun {
		return applied, nil
	}
	devicePresent := c.DevicePresent
	if devicePresent == nil {
		devicePresent = interfaceDevicePresent
	}
	if !devicePresent(ctx, address.Interface) {
		return mobilityAppliedIPv4Address{}, fmt.Errorf("device %q is not ready", address.Interface)
	}
	addressPresent := c.AddressPresent
	if addressPresent == nil {
		addressPresent = ipv4AddressPresent
	}
	if addressPresent(ctx, address.Interface, address.Address) {
		return applied, nil
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	name, args := ipv4StaticAddressApplyCommand(platform.CurrentOS(), address.Interface, address.Address)
	if err := command(ctx, name, args...); err != nil {
		return mobilityAppliedIPv4Address{}, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return applied, nil
}

func (c IPv4StaticAddressController) removeMobilityStaticAddress(ctx context.Context, address mobilityAppliedIPv4Address) error {
	if c.DryRun {
		return nil
	}
	addressPresent := c.AddressPresent
	if addressPresent == nil {
		addressPresent = ipv4AddressPresent
	}
	if !addressPresent(ctx, address.Interface, address.Address) {
		return nil
	}
	command := c.Command
	if command == nil {
		command = runCommandContext
	}
	name, args := ipv4StaticAddressDeleteCommand(platform.CurrentOS(), address.Interface, address.Address)
	if err := command(ctx, name, args...); err != nil {
		return fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return nil
}

func (c IPv4StaticAddressController) appliedMobilityStaticAddresses() ([]mobilityAppliedIPv4Address, error) {
	if c.Store == nil {
		return nil, nil
	}
	status := c.Store.ObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName)
	if !statusValuePresent(status, "appliedMobilityStaticAddresses") {
		return nil, nil
	}
	var decoded []mobilityAppliedIPv4Address
	if !decodeAppliedDataplaneStatus(status["appliedMobilityStaticAddresses"], &decoded) || decoded == nil {
		return nil, fmt.Errorf("must be a non-null JSON array")
	}
	byID := map[string]mobilityAppliedIPv4Address{}
	for _, address := range decoded {
		if err := validateAppliedMobilityStaticAddress(address); err != nil {
			return nil, err
		}
		if previous, exists := byID[address.ID]; exists {
			if previous != address {
				return nil, fmt.Errorf("conflicting mobility IPv4 address ledger entries for id %q", address.ID)
			}
			continue
		}
		byID[address.ID] = address
	}
	return sortedMobilityAppliedStaticAddresses(byID), nil
}

func (c IPv4StaticAddressController) saveAppliedMobilityStaticAddresses(addresses map[string]mobilityAppliedIPv4Address) error {
	if c.DryRun || c.Store == nil {
		return nil
	}
	merger, ok := c.Store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("mobility IPv4 address effects require an applied-effect status merger")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", samDataplaneStatusName, map[string]any{
		"appliedMobilityStaticAddresses": sortedMobilityAppliedStaticAddresses(addresses),
	})
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

func mobilityStaticAddressMatchesIntent(applied mobilityAppliedIPv4Address, intent dynamicconfig.MobilityIPv4AddressIntent) bool {
	return applied.ID == intent.ID && applied.PoolRef == intent.PoolRef && applied.PoolPrefix == intent.PoolPrefix && applied.Purpose == string(intent.Purpose) &&
		applied.Interface == intent.Interface && applied.Address == intent.Address
}

func mobilityStaticAddressesEqual(left, right mobilityAppliedIPv4Address) bool {
	return left == right
}

func mobilityStaticAddressDesiredByUser(router *api.Router, ifname, address string) bool {
	return ipv4StaticAddressDesiredByAnother(router, "", ifname, address)
}

func sortedMobilityAppliedStaticAddresses(addresses map[string]mobilityAppliedIPv4Address) []mobilityAppliedIPv4Address {
	out := make([]mobilityAppliedIPv4Address, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func validateAppliedMobilityStaticAddress(address mobilityAppliedIPv4Address) error {
	intent := dynamicconfig.MobilityIPv4AddressIntent{
		ID:         address.ID,
		PoolRef:    address.PoolRef,
		PoolPrefix: address.PoolPrefix,
		Purpose:    dynamicconfig.MobilityIPv4AddressPurpose(address.Purpose),
		Interface:  address.Interface,
		Address:    address.Address,
	}
	_, err := normalizedMobilityStaticAddressIntents([]dynamicconfig.MobilityIPv4AddressIntent{intent})
	return err
}
