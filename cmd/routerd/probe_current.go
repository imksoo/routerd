// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"net"
	"net/netip"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/healthcheck"
	"github.com/imksoo/routerd/pkg/resourcequery"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

// A probe owns a prepared attempt, not the authority to reuse it after a wait.
// The daemon supplies its current router reader; direct callers own a fixed
// router snapshot but still reread current conditional observations.
type probeCurrentRouter func() (*api.Router, error)

func daemonEgressAttemptCurrent(ctx context.Context, current probeCurrentRouter, store *routerstate.SQLiteStore, policyName, candidateName string, targetIndex int, ref string, prepared api.HealthCheckSpec) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	router, err := current()
	if err != nil || router == nil {
		return false, err
	}
	authorized := false
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "EgressRoutePolicy" || resource.Metadata.Name != policyName {
			continue
		}
		policy, err := resource.EgressRoutePolicySpec()
		if err != nil {
			return false, err
		}
		for _, candidate := range policy.Candidates {
			if candidate.Name != candidateName || candidate.Enabled != nil && !*candidate.Enabled {
				continue
			}
			if resourcequery.ResourceWhenPresent(candidate.When) {
				if store == nil {
					return false, nil
				}
				active, err := resourcequery.ResourceWhenMatches(candidate.When, store)
				if err != nil || !active {
					return false, err
				}
			}
			selected := candidate.HealthCheck
			if targetIndex >= 0 {
				if targetIndex >= len(candidate.Targets) {
					continue
				}
				selected = candidate.Targets[targetIndex].HealthCheck
			}
			authorized = selected == ref
			break
		}
		break
	}
	if !authorized {
		return false, nil
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "HealthCheck" || resource.Metadata.Name != ref {
			continue
		}
		spec, err := resource.HealthCheckSpec()
		if err != nil {
			return false, err
		}
		if spec.Enabled != nil && !*spec.Enabled {
			return false, nil
		}
		spec, err = healthcheck.ResolveSpecWithStoreForResource(router, store, ref, spec)
		if err != nil {
			return false, err
		}
		return spec.Probe() == prepared.Probe() && spec.Type == prepared.Type && spec.TargetSource == prepared.TargetSource, nil
	}
	return false, nil
}

func resolvedProbeRouter(router *api.Router, store *routerstate.SQLiteStore) (*api.Router, error) {
	var source resourcequery.StateStore
	if store != nil {
		source = store
	} else if router != nil {
		for _, resource := range router.Spec.Resources {
			if resourcequery.ResourceWhenPresent(resourcequery.ResourceWhen(resource)) {
				return nil, nil
			}
		}
	}
	return resourcequery.FilterRouterByResolvedWhen(router, source)
}

func daemonDNSAttemptCurrent(ctx context.Context, current probeCurrentRouter, store *routerstate.SQLiteStore, resolverName string, listenIndex int, endpoint string, question daemonDNSQuestion) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if local, err := daemonDNSProbeEndpointLocal(endpoint); err != nil || !local {
		return false, nil
	}
	router, err := current()
	if err != nil || router == nil {
		return false, err
	}
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "DNSResolver" || resource.Metadata.Name != resolverName {
			continue
		}
		spec, err := resource.DNSResolverSpec()
		if err != nil {
			return false, err
		}
		if listenIndex >= len(spec.Listen) {
			return false, nil
		}
		listen := spec.Listen[listenIndex]
		selected, ok, err := daemonDNSProbeQuestion(router, resolverName, spec, listen)
		if err != nil || !ok || selected != question {
			return false, err
		}
		addresses, _, err := daemonDNSProbeAddresses(store, listen)
		if err != nil {
			return false, err
		}
		for _, address := range addresses {
			if value, ok := daemonDNSProbeEndpoint(address, listen.Port); ok && value == endpoint {
				return true, nil
			}
		}
		return false, nil
	}
	return false, nil
}

// A configured bind address is intent, not proof that traffic stays on this
// host. Non-loopback destinations need current native assignment observation.
func daemonDNSProbeEndpointLocal(endpoint string) (bool, error) {
	host, _, err := net.SplitHostPort(endpoint)
	if err != nil {
		return false, err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false, err
	}
	ip = ip.Unmap()
	if ip.IsLoopback() {
		return true, nil
	}
	var addresses []net.Addr
	if zone := ip.Zone(); zone != "" {
		iface, err := net.InterfaceByName(zone)
		if err != nil {
			return false, err
		}
		addresses, err = iface.Addrs()
		if err != nil {
			return false, err
		}
	} else {
		addresses, err = net.InterfaceAddrs()
		if err != nil {
			return false, err
		}
	}
	for _, address := range addresses {
		if prefix, err := netip.ParsePrefix(address.String()); err == nil && prefix.Addr().Unmap() == ip.WithZone("") {
			return true, nil
		}
	}
	return false, nil
}

func daemonDNSProbeAddresses(store *routerstate.SQLiteStore, listen api.DNSResolverListenSpec) ([]string, bool, error) {
	addresses := append([]string(nil), listen.Addresses[:min(len(listen.Addresses), 16)]...)
	limited := len(listen.Addresses) > len(addresses)
	for index, source := range listen.AddressFrom {
		// Empty/unresolved sources consume preparation capacity too.
		if index >= 16 || len(addresses) >= 16 {
			return addresses, true, nil
		}
		values, err := resourcequery.ReadValues(store, source)
		if err != nil {
			return nil, false, err
		}
		count := min(len(values), 16-len(addresses))
		limited = limited || len(values) > count
		addresses = append(addresses, values[:count]...)
	}
	return addresses, limited, nil
}

func daemonDNSProbeEndpoint(address string, port int) (string, bool) {
	ip, err := netip.ParseAddr(strings.TrimSpace(address))
	if err != nil {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
		if err != nil {
			return "", false
		}
		ip = prefix.Addr()
	}
	if ip.IsUnspecified() {
		if ip.Is4() {
			ip = netip.MustParseAddr("127.0.0.1")
		} else {
			ip = netip.IPv6Loopback()
		}
	}
	if port == 0 {
		port = 5053
	}
	return net.JoinHostPort(ip.String(), strconv.Itoa(port)), true
}
