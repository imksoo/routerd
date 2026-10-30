// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// runLinuxPPPoEInterfaceProbe is injected by tests. The probe is used only for
// lifecycle ownership; it does not alter Linux pppd configuration or logging.
var runLinuxPPPoEInterfaceProbe = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var linuxPPPoEInterfaceExists = func(ctx context.Context, ifname string) (bool, error) {
	out, err := runLinuxPPPoEInterfaceProbe(ctx, "ip", "-j", "link", "show")
	if err != nil {
		return false, fmt.Errorf("inspect Linux PPPoE interface %q ownership: %w", ifname, err)
	}
	var links []struct {
		IfName string `json:"ifname"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		return false, fmt.Errorf("decode Linux PPPoE interfaces: %w", err)
	}
	if links == nil {
		return false, fmt.Errorf("Linux PPPoE interface observation is not an array")
	}
	for _, link := range links {
		if link.IfName == "" {
			return false, fmt.Errorf("Linux PPPoE interface observation lacks name")
		}
		if link.IfName == ifname {
			return true, nil
		}
	}
	return false, nil
}
