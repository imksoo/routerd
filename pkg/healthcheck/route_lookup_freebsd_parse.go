// SPDX-License-Identifier: BSD-3-Clause

package healthcheck

import (
	"errors"
	"fmt"
	"strings"
)

// normalizeFreeBSDRouteFamily accepts the public healthcheck family values
// only. Falling through on an unknown value can select an arbitrary DNS
// result and make route evidence misleading.
func normalizeFreeBSDRouteFamily(family string) (string, error) {
	switch normalized := strings.ToLower(strings.TrimSpace(family)); normalized {
	case "", "ipv4", "ipv6":
		return normalized, nil
	default:
		return "", fmt.Errorf("unsupported address family %q", family)
	}
}

// parseFreeBSDRouteGet extracts the stable fields emitted by `route -n get`.
// Gateway can be absent for directly connected routes; interface is required
// because it is the operator-useful route-selection result.
func parseFreeBSDRouteGet(output string) (RouteInfo, error) {
	var info RouteInfo
	for _, line := range strings.Split(output, "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(key)) {
		case "gateway":
			info.NextHop = strings.TrimSpace(value)
		case "interface":
			info.OutInterface = strings.TrimSpace(value)
		case "if address", "source":
			info.Source = strings.TrimSpace(value)
		}
	}
	if info.OutInterface == "" {
		return RouteInfo{}, errors.New("route get output has no interface")
	}
	return info, nil
}
