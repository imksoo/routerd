// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"fmt"
	"strconv"
	"strings"
)

// CommunitySendMask represents the standard/extended community advertisement
// families independently of the communities attached to a route.
func CommunitySendMask(mode string) (uint32, error) {
	switch strings.TrimSpace(mode) {
	case "", "both":
		return 3, nil
	case "standard":
		return 1, nil
	case "extended":
		return 2, nil
	default:
		return 0, fmt.Errorf("unsupported community send mode %q", mode)
	}
}

// StandardCommunities decodes the supported standard BGP community syntax.
func StandardCommunities(values []string) ([]uint32, error) {
	var out []uint32
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		switch value {
		case "internet":
			out = append(out, 0)
			continue
		case "no-export":
			out = append(out, uint32(0xffff)<<16|0xff01)
			continue
		case "no-advertise":
			out = append(out, uint32(0xffff)<<16|0xff02)
			continue
		}
		if strings.Contains(value, ":") {
			parts := strings.Split(value, ":")
			if len(parts) != 2 {
				return nil, fmt.Errorf("invalid community %q", value)
			}
			high, err := strconv.ParseUint(parts[0], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid community %q: %w", value, err)
			}
			low, err := strconv.ParseUint(parts[1], 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid community %q: %w", value, err)
			}
			out = append(out, uint32(high)<<16|uint32(low))
			continue
		}
		parsed, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid community %q: %w", value, err)
		}
		out = append(out, uint32(parsed))
	}
	return out, nil
}
