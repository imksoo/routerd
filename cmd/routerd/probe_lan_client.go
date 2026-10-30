// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/healthcheck"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const lanClientProbeTimeout = 2 * time.Second

// daemonLANClientProbe measures one router-originated ICMP exchange. A reply
// proves this exchange only: it does not prove client-originated forwarding,
// firewall/NAT behavior, or an end-to-end LAN application session.
func daemonLANClientProbe(ctx context.Context, _ *api.Router, _ *routerstate.SQLiteStore, target string) ([]controlapi.ProbeCheck, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	address, err := netip.ParseAddr(strings.TrimSpace(target))
	if err != nil {
		return []controlapi.ProbeCheck{{Name: "LAN client target", Status: "fail", Kind: "scope", Target: target, Detail: "a literal unicast client IP is required"}}, nil
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() || address == netip.MustParseAddr("255.255.255.255") {
		return []controlapi.ProbeCheck{{Name: "LAN client target", Status: "fail", Kind: "scope", Target: target, Detail: "an unspecified, multicast or broadcast address cannot identify one client"}}, nil
	}
	zone := address.Zone()
	if address.IsLinkLocalUnicast() && address.Is6() && zone == "" {
		return []controlapi.ProbeCheck{{Name: "LAN client ICMP", Status: "warn", Kind: "scope", Protocol: healthcheck.ProtocolICMP, Target: address.String(), Detail: "unobserved: an IPv6 link-local target requires its native interface zone (for example fe80::1%eth0)"}}, nil
	}
	family := "ipv6"
	if address.Is4() {
		family = "ipv4"
	}
	spec := api.HealthCheckSpec{Protocol: healthcheck.ProtocolICMP, AddressFamily: family, Target: address.WithZone("").String(), SourceInterface: zone}
	started := time.Now().UTC()
	probeCtx, cancel := context.WithTimeout(ctx, lanClientProbeTimeout)
	defer cancel()
	result := healthcheck.Probe(probeCtx, spec)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	status := "fail"
	detail := fmt.Sprintf("router-originated ICMP echo to %s: %s", address, result.Message)
	if result.OK {
		status = "pass"
		detail += "; this does not establish client-originated forwarding or an application session"
	} else if result.FailureKind == healthcheck.FailureKindPermission {
		status = "warn"
		detail = "unobserved: raw ICMP capability is unavailable: " + result.Message
	} else if result.FailureKind == healthcheck.FailureKindOther {
		status = "warn"
		detail = "unobserved: ICMP exchange could not be established: " + result.Message
	} else if result.Timeout {
		detail += "; no matching echo reply before the probe deadline (the client may filter ICMP)"
	}
	return []controlapi.ProbeCheck{{Name: "LAN client ICMP", Status: status, Detail: detail, Kind: "communication", Protocol: healthcheck.ProtocolICMP, Target: address.String(), ObservedAt: started, DurationMilliseconds: time.Since(started).Milliseconds(), FailureKind: result.FailureKind, BindingInterface: zone}}, nil
}
