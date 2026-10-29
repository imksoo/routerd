// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type RouterAdvertisementLifetimes struct {
	Preferred uint32
	Valid     uint32
	Router    uint16
}

// ResolveRouterAdvertisementLifetimes prepares configuration limits once.
// Dynamic prefixes retain their upstream deadlines; omitted limits do not cap
// them. Static prefixes default to the former twelve-hour advertisement lease.
func ResolveRouterAdvertisementLifetimes(spec IPv6RouterAdvertisementSpec) (RouterAdvertisementLifetimes, error) {
	validDefault := uint32(12 * 60 * 60)
	if spec.PrefixFrom.Resource != "" {
		validDefault = math.MaxUint32
	}
	valid, err := parsePrefixLifetime(spec.ValidLifetime, validDefault)
	if err != nil {
		return RouterAdvertisementLifetimes{}, fmt.Errorf("validLifetime: %w", err)
	}
	preferred, err := parsePrefixLifetime(spec.PreferredLifetime, valid)
	if err != nil {
		return RouterAdvertisementLifetimes{}, fmt.Errorf("preferredLifetime: %w", err)
	}
	if preferred > valid {
		return RouterAdvertisementLifetimes{}, fmt.Errorf("preferredLifetime must not exceed validLifetime")
	}
	router := uint64(1800)
	if strings.TrimSpace(spec.RouterLifetime) != "" {
		router, err = strconv.ParseUint(spec.RouterLifetime, 10, 16)
		if err != nil || router > 9000 || router > 0 && router < 4 {
			return RouterAdvertisementLifetimes{}, fmt.Errorf("routerLifetime must be 0 or 4-9000 seconds")
		}
	}
	return RouterAdvertisementLifetimes{Preferred: preferred, Valid: valid, Router: uint16(router)}, nil
}

func parsePrefixLifetime(value string, fallback uint32) (uint32, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback, nil
	}
	if value == "infinite" {
		return math.MaxUint32, nil
	}
	seconds, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("must be seconds within 0-4294967295 or infinite")
	}
	return uint32(seconds), nil
}
