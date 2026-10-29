// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// FIBRoute is the desired forwarding topology, independent of a kernel receipt.
type FIBRoute struct {
	Prefix          string   `json:"prefix"`
	NextHops        []string `json:"nextHops"`
	PreferredSource string   `json:"preferredSource,omitempty"`
}

type FIBNextHop struct {
	Gateway        string `json:"gateway"`
	InterfaceIndex int    `json:"interfaceIndex"`
}

// NativeFIBRoute records only the precise native tuple installed by this owner.
// Protocol flags alone do not establish ownership of a route found in the RIB.
type NativeFIBRoute struct {
	Prefix          string       `json:"prefix"`
	NextHops        []FIBNextHop `json:"nextHops"`
	PreferredSource string       `json:"preferredSource,omitempty"`
	Table           int          `json:"table"`
	Protocol        int          `json:"protocol"`
	Metric          int          `json:"metric"`
}

type FIBOperation struct {
	Action  string         `json:"action"`
	Route   NativeFIBRoute `json:"route"`
	Unknown bool           `json:"unknown,omitempty"`
}

type FIBJournal struct {
	KernelScope string           `json:"kernelScope"`
	Applied     []NativeFIBRoute `json:"applied"`
	Pending     *FIBOperation    `json:"pending,omitempty"`
}

func EqualNativeFIBRoute(a, b NativeFIBRoute) bool {
	return a.Prefix == b.Prefix && a.PreferredSource == b.PreferredSource && a.Table == b.Table && a.Protocol == b.Protocol && a.Metric == b.Metric && slices.Equal(a.NextHops, b.NextHops)
}

func ValidateFIBJournal(j FIBJournal) error {
	table, err := fibScopeTable(j.KernelScope)
	if err != nil {
		return err
	}
	if j.Applied == nil {
		return fmt.Errorf("BGP FIB receipt observation is missing")
	}
	seen := []NativeFIBRoute{}
	for _, r := range j.Applied {
		if err := validateNativeFIBRoute(r, j.KernelScope, table); err != nil {
			return err
		}
		for _, previous := range seen {
			if EqualNativeFIBRoute(previous, r) || strings.HasPrefix(j.KernelScope, "linux:") && previous.Prefix == r.Prefix {
				return fmt.Errorf("duplicate BGP FIB receipt %s", r.Prefix)
			}
		}
		seen = append(seen, r)
	}
	if p := j.Pending; p != nil {
		if p.Action != "add" && p.Action != "withdraw" {
			return fmt.Errorf("unsupported BGP FIB operation %q", p.Action)
		}
		if err := validateNativeFIBRoute(p.Route, j.KernelScope, table); err != nil {
			return err
		}
		if p.Action == "add" {
			for _, r := range seen {
				if EqualNativeFIBRoute(r, p.Route) {
					return fmt.Errorf("pending BGP FIB add already has a receipt")
				}
			}
		}
		if p.Action == "withdraw" {
			found := false
			for _, r := range j.Applied {
				found = found || EqualNativeFIBRoute(r, p.Route)
			}
			if !found {
				return fmt.Errorf("pending BGP FIB withdrawal lacks its exact receipt")
			}
		}
	}
	return nil
}

func fibScopeTable(scope string) (int, error) {
	if strings.TrimSpace(scope) != scope {
		return 0, fmt.Errorf("BGP FIB kernel identity is invalid")
	}
	if strings.HasPrefix(scope, "linux:") && len(scope) > len("linux:") {
		return 254, nil
	}
	if strings.HasPrefix(scope, "freebsd:") {
		at := strings.LastIndex(scope, ":fib:")
		if at > len("freebsd:") {
			fib, err := strconv.Atoi(scope[at+len(":fib:"):])
			if err == nil && fib >= 0 {
				return fib, nil
			}
		}
	}
	return 0, fmt.Errorf("BGP FIB kernel identity is missing or invalid")
}

func validateNativeFIBRoute(r NativeFIBRoute, scope string, table int) error {
	if r.Table != table {
		return fmt.Errorf("BGP FIB receipt table differs from its kernel scope")
	}

	if strings.HasPrefix(scope, "linux:") {
		if r.Table != 254 || r.Protocol != 186 || r.Metric != 200 {
			return fmt.Errorf("invalid Linux BGP FIB receipt selectors")
		}
	} else if r.Protocol != 0x8000 || r.Metric != 0 || len(r.NextHops) != 1 || r.PreferredSource == "" {
		return fmt.Errorf("invalid FreeBSD BGP FIB receipt selectors")
	}
	p, err := netip.ParsePrefix(r.Prefix)
	if err != nil || p.Masked().String() != r.Prefix || strings.HasPrefix(scope, "linux:") && !p.Addr().Is4() {
		return fmt.Errorf("invalid BGP FIB receipt prefix %q", r.Prefix)
	}
	if len(r.NextHops) == 0 {
		return fmt.Errorf("BGP FIB receipt %s has no next hop", r.Prefix)
	}
	if r.Table < 0 || r.Protocol < 0 || r.Metric < 0 {
		return fmt.Errorf("invalid BGP FIB native selector")
	}
	seen := map[FIBNextHop]bool{}
	for _, hop := range r.NextHops {
		a, err := netip.ParseAddr(hop.Gateway)
		if err != nil || a.Zone() != "" || a.Is4() != p.Addr().Is4() || a.String() != hop.Gateway || a.IsUnspecified() || hop.InterfaceIndex <= 0 || seen[hop] {
			return fmt.Errorf("invalid BGP FIB receipt next hop for %s", r.Prefix)
		}
		seen[hop] = true
	}
	if r.PreferredSource != "" {
		a, err := netip.ParseAddr(r.PreferredSource)
		if err != nil || a.Zone() != "" || a.Is4() != p.Addr().Is4() || a.String() != r.PreferredSource {
			return fmt.Errorf("invalid BGP FIB receipt source for %s", r.Prefix)
		}
	}
	return nil
}
