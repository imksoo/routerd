// SPDX-License-Identifier: BSD-3-Clause

package render

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
)

func PPPoEClientSystemdSpec(resource, ifname string, spec api.PPPoESessionSpec, telemetryEnv []string) api.SystemdUnitSpec {
	noNewPrivileges := false
	args := []string{"daemon", "--resource", resource, "--interface", ifname, "--username", spec.Username}
	if spec.PasswordFile != "" {
		args = append(args, "--password-file", spec.PasswordFile)
	} else if spec.Password != "" {
		args = append(args, "--password", spec.Password)
	}
	if spec.AuthMethod != "" {
		args = append(args, "--auth-method", spec.AuthMethod)
	}
	if spec.MTU != 0 {
		args = append(args, "--mtu", fmt.Sprintf("%d", spec.MTU))
	}
	if spec.MRU != 0 {
		args = append(args, "--mru", fmt.Sprintf("%d", spec.MRU))
	}
	if spec.ServiceName != "" {
		args = append(args, "--service-name", spec.ServiceName)
	}
	if spec.ACName != "" {
		args = append(args, "--ac-name", spec.ACName)
	}
	if spec.LCPEchoInterval != 0 {
		args = append(args, "--lcp-echo-interval", fmt.Sprintf("%d", spec.LCPEchoInterval))
	}
	if spec.LCPEchoFailure != 0 {
		args = append(args, "--lcp-echo-failure", fmt.Sprintf("%d", spec.LCPEchoFailure))
	}
	return api.SystemdUnitSpec{
		Description:              "routerd PPPoE client " + resource,
		ExecStart:                append([]string{"/usr/local/sbin/routerd-pppoe-client"}, args...),
		Environment:              telemetryEnv,
		RuntimeDirectory:         []string{"routerd/pppoe-client"},
		RuntimeDirectoryPreserve: "yes",
		StateDirectory:           []string{"routerd/pppoe-client"},
		LogsDirectory:            []string{"routerd"},
		// Keep the former root process privileges for pppd and its plugins.
		NoNewPrivileges: &noNewPrivileges,
	}
}
