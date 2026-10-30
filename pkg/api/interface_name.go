// SPDX-License-Identifier: BSD-3-Clause

package api

import (
	"crypto/sha256"
	"encoding/binary"
	"runtime"
	"strconv"
	"strings"
)

// ResolveInterfaceIfName returns the kernel interface name for a managed
// interface resource, or name unchanged when it is already a kernel name or
// does not name a managed interface. Keeping this API-level mapping shared
// prevents controllers from resolving the same logical alias differently.
func ResolveInterfaceIfName(router *Router, name string) string {
	if router == nil {
		return name
	}
	for _, resource := range router.Spec.Resources {
		if resource.Metadata.Name != name {
			continue
		}
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "VXLANSegment":
			spec, err := resource.VXLANSegmentSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "VRF":
			spec, err := resource.VRFSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "VXLANTunnel":
			spec, err := resource.VXLANTunnelSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}

		case "WireGuardInterface":
			spec, err := resource.WireGuardInterfaceSpec()
			if err == nil && spec.IfName != "" {
				return spec.IfName
			}
		case "PPPoESession":
			spec, err := resource.PPPoESessionSpec()
			if err == nil {
				if spec.IfName != "" {
					return spec.IfName
				}
				return "ppp-" + resource.Metadata.Name
			}
		case "DSLiteTunnel":
			spec, err := resource.DSLiteTunnelSpec()
			if err == nil {
				configured := spec.TunnelName
				if configured == "" {
					configured = spec.Interface
				}
				if configured == "" {
					configured = resource.Metadata.Name
				}
				return DSLiteRuntimeIfName(configured, runtime.GOOS)
			}
		}
	}
	return name
}

// DSLiteRuntimeIfName is shared by the native owner and interface-reference
// resolution. FreeBSD requires a gif unit name at the native tunnel boundary.
func DSLiteRuntimeIfName(name, osName string) string {
	if osName != "freebsd" {
		return name
	}
	name = strings.TrimSpace(name)
	if digits, ok := strings.CutPrefix(name, "gif"); ok && digits != "" {
		allDigits := true
		for _, digit := range digits {
			allDigits = allDigits && digit >= '0' && digit <= '9'
		}
		if allDigits {
			return name
		}
	}
	sum := sha256.Sum256([]byte(name))
	index := 100 + int(binary.BigEndian.Uint16(sum[:2])%900)
	return "gif" + strconv.Itoa(index)
}
