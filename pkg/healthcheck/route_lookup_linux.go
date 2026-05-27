// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package healthcheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

// lookupRoute uses `ip -j route get TARGET` to discover the nexthop, output
// interface and source address the kernel would pick for the probe. We treat
// "ip" missing or returning non-zero as a soft failure: the probe still ran
// so route info is best-effort. The function honours the address family hint
// when one is given so dual-stacked targets do not return an IPv6 entry when
// the probe was IPv4-only.
func lookupRoute(ctx context.Context, target, family string) (RouteInfo, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return RouteInfo{}, errors.New("target is required")
	}
	// If target is a hostname we cannot rely on `ip route get` resolving it,
	// so try a DNS lookup first. Failures here are non-fatal: the probe was
	// the one with the unreachable target, not the route lookup.
	if net.ParseIP(target) == nil {
		ip, err := resolveTargetIP(ctx, target, family)
		if err != nil {
			return RouteInfo{}, err
		}
		target = ip
	}
	args := []string{"-j"}
	switch strings.ToLower(family) {
	case "ipv4":
		args = append(args, "-4")
	case "ipv6":
		args = append(args, "-6")
	}
	args = append(args, "route", "get", target)
	cmd := exec.CommandContext(ctx, "ip", args...)
	out, err := cmd.Output()
	if err != nil {
		return RouteInfo{}, fmt.Errorf("ip route get: %w", err)
	}
	var rows []struct {
		Gateway string `json:"gateway"`
		Dev     string `json:"dev"`
		PrefSrc string `json:"prefsrc"`
		Src     string `json:"src"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		return RouteInfo{}, fmt.Errorf("parse ip route get: %w", err)
	}
	if len(rows) == 0 {
		return RouteInfo{}, errors.New("no route")
	}
	row := rows[0]
	src := row.PrefSrc
	if src == "" {
		src = row.Src
	}
	return RouteInfo{NextHop: row.Gateway, OutInterface: row.Dev, Source: src}, nil
}

func resolveTargetIP(ctx context.Context, host, family string) (string, error) {
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return "", err
	}
	for _, addr := range addrs {
		isV4 := addr.IP.To4() != nil
		switch strings.ToLower(family) {
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
