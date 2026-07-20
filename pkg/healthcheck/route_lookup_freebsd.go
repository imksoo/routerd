// SPDX-License-Identifier: BSD-3-Clause

//go:build freebsd

package healthcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// lookupRoute uses FreeBSD's read-only `route -n get` command to record the
// nexthop and output interface selected for a healthcheck probe. It does not
// alter routing state.
func lookupRoute(ctx context.Context, target, family string) (RouteInfo, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return RouteInfo{}, errors.New("target is required")
	}
	family, err := normalizeFreeBSDRouteFamily(family)
	if err != nil {
		return RouteInfo{}, err
	}
	if net.ParseIP(target) == nil {
		ip, err := resolveFreeBSDTargetIP(ctx, target, family)
		if err != nil {
			return RouteInfo{}, err
		}
		target = ip
	}

	args := []string{"-n", "get"}
	switch family {
	case "ipv4":
		args = append(args, "-inet")
	case "ipv6":
		args = append(args, "-inet6")
	}
	args = append(args, target)
	out, err := exec.CommandContext(ctx, "route", args...).Output()
	if err != nil {
		return RouteInfo{}, fmt.Errorf("route -n get: %w", err)
	}
	return parseFreeBSDRouteGet(string(out))
}

func resolveFreeBSDTargetIP(ctx context.Context, host, family string) (string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		isV4 := addr.IP.To4() != nil
		switch family {
		case "ipv4":
			if isV4 {
				return addr.IP.String(), nil
			}
		case "ipv6":
			if !isV4 {
				return addr.IP.String(), nil
			}
		default:
			return addr.IP.String(), nil
		}
	}
	return "", fmt.Errorf("no %s address found for %s", family, host)
}
