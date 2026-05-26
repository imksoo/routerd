// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
)

func renderAddressFromResource(router *api.Router, source api.StatusValueSourceSpec) (string, error) {
	if router == nil || strings.TrimSpace(source.Resource) == "" {
		return "", nil
	}
	kind, name, ok := strings.Cut(strings.TrimSpace(source.Resource), "/")
	if !ok || kind == "" || name == "" {
		return "", fmt.Errorf("source resource must be Kind/name")
	}
	field := strings.TrimSpace(source.Field)
	if field == "" {
		field = "address"
	}
	for _, res := range router.Spec.Resources {
		if res.Kind != kind || res.Metadata.Name != name {
			continue
		}
		switch kind {
		case "IPv4StaticAddress":
			if field != "address" {
				return "", fmt.Errorf("unsupported IPv4StaticAddress field %q", field)
			}
			spec, err := res.IPv4StaticAddressSpec()
			if err != nil {
				return "", err
			}
			return renderAddressValue(spec.Address), nil
		case "VirtualAddress":
			if field != "address" {
				return "", fmt.Errorf("unsupported VirtualAddress field %q", field)
			}
			spec, err := res.VirtualAddressSpec()
			if err != nil {
				return "", err
			}
			return renderAddressValue(spec.Address), nil
		default:
			return "", fmt.Errorf("unsupported source resource kind %q in static render", kind)
		}
	}
	if source.Optional {
		return "", nil
	}
	return "", fmt.Errorf("source resource %s not found", source.Resource)
}

func renderAddressValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Addr().String()
	}
	return value
}
