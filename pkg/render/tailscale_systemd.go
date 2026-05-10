// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"strconv"
	"strings"

	"routerd/pkg/api"
)

func TailscaleUnitName(name string) string {
	return "routerd-tailscale-" + sanitizeSystemdName(name) + ".service"
}

func TailscaleSystemdSpec(name string, spec api.TailscaleNodeSpec) api.SystemdUnitSpec {
	if firstNonEmpty(spec.State, "present") == "absent" {
		return api.SystemdUnitSpec{State: "absent", UnitName: TailscaleUnitName(name)}
	}
	if spec.AuthKeyFile != "" && spec.AuthKeyEnv == "" {
		spec.AuthKeyEnv = "TS_AUTHKEY"
	}
	noNewPrivileges := true
	privateTmp := true
	remainAfterExit := true
	var environmentFiles []string
	if spec.AuthKeyFile != "" {
		environmentFiles = append(environmentFiles, spec.AuthKeyFile)
	}
	return api.SystemdUnitSpec{
		UnitName:                 TailscaleUnitName(name),
		Description:              "routerd Tailscale node " + name,
		Type:                     "oneshot",
		ExecStart:                TailscaleUpArgs(spec),
		EnvironmentFiles:         environmentFiles,
		Wants:                    []string{"network-online.target", "tailscaled.service"},
		After:                    []string{"network-online.target", "tailscaled.service"},
		WantedBy:                 []string{"multi-user.target"},
		Restart:                  "no",
		RuntimeDirectory:         []string{"routerd"},
		RuntimeDirectoryPreserve: "yes",
		RemainAfterExit:          &remainAfterExit,
		NoNewPrivileges:          &noNewPrivileges,
		PrivateTmp:               &privateTmp,
		ProtectHome:              "true",
		ProtectSystem:            "no",
		RestrictAddressFamilies:  []string{"AF_UNIX", "AF_INET", "AF_INET6", "AF_NETLINK"},
		CapabilityBoundingSet:    []string{"CAP_NET_ADMIN", "CAP_NET_RAW"},
		AmbientCapabilities:      []string{"CAP_NET_ADMIN", "CAP_NET_RAW"},
	}
}

func TailscaleUpArgs(spec api.TailscaleNodeSpec) []string {
	binary := firstNonEmpty(spec.BinaryPath, "/usr/bin/tailscale")
	args := []string{binary, "up"}
	appendValue := func(flag, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			args = append(args, flag+"="+value)
		}
	}
	appendBool := func(flag string, value *bool) {
		if value != nil {
			args = append(args, flag+"="+strconv.FormatBool(*value))
		}
	}
	appendValue("--hostname", spec.Hostname)
	appendValue("--login-server", spec.LoginServer)
	appendValue("--operator", spec.Operator)
	if spec.AuthKey != "" {
		appendValue("--auth-key", spec.AuthKey)
	} else if spec.AuthKeyEnv != "" {
		appendValue("--auth-key", "${"+spec.AuthKeyEnv+"}")
	}
	if spec.AdvertiseExitNode {
		args = append(args, "--advertise-exit-node")
	}
	if len(spec.AdvertiseRoutes) > 0 {
		args = append(args, "--advertise-routes="+strings.Join(spec.AdvertiseRoutes, ","))
	}
	if len(spec.AdvertiseTags) > 0 {
		args = append(args, "--advertise-tags="+strings.Join(spec.AdvertiseTags, ","))
	}
	appendBool("--accept-routes", spec.AcceptRoutes)
	appendBool("--accept-dns", spec.AcceptDNS)
	appendBool("--shields-up", spec.ShieldsUp)
	if spec.SSH {
		args = append(args, "--ssh")
	}
	return args
}

func sanitizeSystemdName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "default"
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
