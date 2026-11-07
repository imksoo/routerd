// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sysctlprofile"
)

func applyRuntimeSysctls(router *api.Router) ([]string, error) {
	var applied []string
	for _, res := range router.Spec.Resources {
		switch res.Kind {
		case "Sysctl":
			spec, err := res.SysctlSpec()
			if err != nil {
				return nil, err
			}
			changed, err := applyRuntimeSysctl(spec.Key, spec.Value, api.BoolDefault(spec.Runtime, true), spec.Optional)
			if err != nil {
				return nil, err
			}
			if changed {
				applied = append(applied, spec.Key)
			}
		case "SysctlProfile":
			spec, err := res.SysctlProfileSpec()
			if err != nil {
				return nil, err
			}
			if !api.BoolDefault(spec.Runtime, true) {
				continue
			}
			if platformDefaults.OS == platform.OSFreeBSD && spec.Profile == "router-linux" {
				return nil, fmt.Errorf("%s profile router-linux is not supported on FreeBSD; use router-freebsd", res.ID())
			}
			if platformDefaults.OS == platform.OSLinux && spec.Profile == "router-freebsd" {
				return nil, fmt.Errorf("%s profile router-freebsd is not supported on Linux; use router-linux", res.ID())
			}
			entries, err := sysctlprofile.Entries(spec.Profile, spec.Overrides)
			if err != nil {
				return nil, err
			}
			for _, entry := range entries {
				changed, err := applyRuntimeSysctl(entry.Key, entry.Value, true, entry.Optional)
				if err != nil {
					return nil, err
				}
				if changed {
					applied = append(applied, entry.Key)
				}
			}
		default:
			continue
		}
	}
	return applied, nil
}

func applyRuntimeSysctl(key, value string, runtime bool, optional bool) (bool, error) {
	if !runtime {
		return false, nil
	}
	currentOut, err := exec.Command("sysctl", "-n", key).CombinedOutput()
	if err == nil && strings.TrimSpace(string(currentOut)) == value {
		return false, nil
	}
	if err := runLogged("sysctl", "-w", key+"="+value); err != nil {
		if optional {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func applyHostnames(router *api.Router) ([]string, error) {
	desired, err := managedHostnames(router)
	if err != nil {
		return nil, err
	}
	if len(desired) == 0 {
		return nil, nil
	}
	if len(desired) > 1 {
		return nil, fmt.Errorf("multiple managed Hostname resources are not supported: %s", strings.Join(desired, ","))
	}
	hostname := desired[0]
	currentOut, err := exec.Command("hostname").CombinedOutput()
	if err == nil && strings.TrimSpace(string(currentOut)) == hostname {
		return nil, nil
	}
	if err := runLogged("hostnamectl", "set-hostname", hostname); err != nil {
		if platformDefaults.OS == platform.OSFreeBSD {
			if err := runLogged("sysrc", "hostname="+hostname); err != nil {
				return nil, err
			}
			if fallbackErr := runLogged("hostname", hostname); fallbackErr != nil {
				return nil, fallbackErr
			}
			return []string{hostname}, nil
		}
		return nil, err
	}
	return []string{hostname}, nil
}

func runtimeDnsmasqServicePath(path string) string {
	return path
}

func managedHostnames(router *api.Router) ([]string, error) {
	var hostnames []string
	for _, res := range router.Spec.Resources {
		if res.Kind != "Hostname" {
			continue
		}
		spec, err := res.HostnameSpec()
		if err != nil {
			return nil, err
		}
		if !spec.Managed {
			continue
		}
		hostnames = append(hostnames, spec.Hostname)
	}
	return hostnames, nil
}

func packageSetForOSMain(spec api.PackageSpec, osName string) (api.OSPackageSetSpec, bool) {
	for _, set := range spec.Packages {
		if set.OS == osName {
			return set, true
		}
	}
	return api.OSPackageSetSpec{}, false
}
