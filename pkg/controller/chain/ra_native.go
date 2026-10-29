// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/platform"
)

// Native address discovery is used only for DHCPv6Server's implicit RA mode.
// An explicit RA prefix source never falls back to native interface addresses.
func nativeRAPrefixes(ctx context.Context, ifname string) ([]raPrefixValue, error) {
	name, args := "ip", []string{"-j", "-6", "addr", "show", "dev", ifname}
	if platform.CurrentOS() == platform.OSFreeBSD {
		name, args = "ifconfig", []string{"-L", ifname}
	}
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("observe RA interface %s: %w", ifname, err)
	}
	return parseNativeRAPrefixes(out, ifname, platform.CurrentOS())
}

type raNativeLifetime uint32

func (l *raNativeLifetime) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte(`"forever"`)) {
		*l = raNativeLifetime(math.MaxUint32)
		return nil
	}
	var seconds uint32
	if err := json.Unmarshal(data, &seconds); err != nil {
		return fmt.Errorf("invalid native RA lifetime: %w", err)
	}
	*l = raNativeLifetime(seconds)
	return nil
}

func parseNativeRAPrefixes(out []byte, ifname string, osName platform.OS) ([]raPrefixValue, error) {
	var result []raPrefixValue
	appendPrefix := func(address string, bits int, preferred, valid uint32) error {
		local, err := netip.ParseAddr(strings.Split(address, "%")[0])
		if err != nil {
			return err
		}
		if !local.Is6() || local.Is4In6() || local.IsLinkLocalUnicast() || local.IsLoopback() || local.IsUnspecified() || local.IsMulticast() || bits > 64 || valid == 0 {
			return nil
		}
		result = append(result, raPrefixValue{Prefix: netip.PrefixFrom(local, 64).Masked(), Preferred: min(preferred, valid), Valid: valid})
		return nil
	}
	if osName == platform.OSLinux {
		var links []struct {
			IfName    string `json:"ifname"`
			Addresses []struct {
				Local      string            `json:"local"`
				Prefix     *int              `json:"prefixlen"`
				Flags      []string          `json:"flags"`
				Tentative  bool              `json:"tentative"`
				DADFailed  bool              `json:"dadfailed"`
				Temporary  bool              `json:"temporary"`
				Deprecated bool              `json:"deprecated"`
				Preferred  *raNativeLifetime `json:"preferred_life_time"`
				Valid      *raNativeLifetime `json:"valid_life_time"`
			} `json:"addr_info"`
		}
		if err := json.Unmarshal(out, &links); err != nil {
			return nil, err
		}
		if len(links) != 1 || links[0].IfName != ifname || links[0].Addresses == nil {
			return nil, fmt.Errorf("incomplete RA interface observation")
		}
		for _, address := range links[0].Addresses {
			if address.Tentative || address.DADFailed || address.Temporary || fieldsContain(address.Flags, "tentative") || fieldsContain(address.Flags, "dadfailed") || fieldsContain(address.Flags, "temporary") {
				continue
			}
			if address.Prefix == nil || *address.Prefix < 0 || *address.Prefix > 128 {
				return nil, fmt.Errorf("native RA address has no valid prefix length")
			}
			if address.Preferred == nil || address.Valid == nil {
				return nil, fmt.Errorf("native RA address has no lifetime observation")
			}
			preferred := uint32(*address.Preferred)
			if address.Deprecated || fieldsContain(address.Flags, "deprecated") {
				preferred = 0
			}
			if err := appendPrefix(address.Local, *address.Prefix, preferred, uint32(*address.Valid)); err != nil {
				return nil, err
			}
		}
		return mergeNativeRAPrefixes(result), nil
	}
	if osName != platform.OSFreeBSD {
		return nil, fmt.Errorf("native RA observation unsupported on %s", osName)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), ifname+":") {
		return nil, fmt.Errorf("incomplete RA interface observation")
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "inet6" {
			continue
		}
		if fieldsContain(fields, "tentative") || fieldsContain(fields, "duplicated") || fieldsContain(fields, "temporary") {
			continue
		}
		bits := -1
		preferred, valid := uint32(math.MaxUint32), uint32(math.MaxUint32)
		for i := 2; i+1 < len(fields); i++ {
			switch fields[i] {
			case "prefixlen":
				n, err := strconv.Atoi(fields[i+1])
				if err != nil {
					return nil, err
				}
				bits = n
			case "pltime", "vltime":
				value := uint64(math.MaxUint32)
				var err error
				if fields[i+1] != "infty" && fields[i+1] != "infinite" {
					value, err = strconv.ParseUint(fields[i+1], 10, 32)
				}
				if err != nil {
					return nil, err
				}
				if fields[i] == "pltime" {
					preferred = uint32(value)
				} else {
					valid = uint32(value)
				}
			}
		}
		if bits < 0 {
			return nil, fmt.Errorf("native IPv6 address has no prefix length")
		}
		if fieldsContain(fields, "deprecated") {
			preferred = 0
		}
		if err := appendPrefix(fields[1], bits, preferred, valid); err != nil {
			return nil, err
		}
	}
	return mergeNativeRAPrefixes(result), nil
}

func mergeNativeRAPrefixes(values []raPrefixValue) []raPrefixValue {
	byPrefix := map[netip.Prefix]raPrefixValue{}
	for _, value := range values {
		previous := byPrefix[value.Prefix]
		value.Preferred = max(value.Preferred, previous.Preferred)
		value.Valid = max(value.Valid, previous.Valid)
		byPrefix[value.Prefix] = value
	}
	out := make([]raPrefixValue, 0, len(byPrefix))
	for _, value := range byPrefix {
		out = append(out, value)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Prefix.Addr().Less(out[j].Prefix.Addr()) })
	return out
}
