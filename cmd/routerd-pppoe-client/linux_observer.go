// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
)

// runLinuxPPPoEInterfaceProbe is injected by tests. The probe is used only for
// lifecycle ownership; it does not alter Linux pppd configuration or logging.
var runLinuxPPPoEInterfaceProbe = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

var linuxPPPoEInterfaceExists = func(ctx context.Context, ifname string) (bool, error) {
	_, err := runLinuxPPPoEInterfaceProbe(ctx, "ip", "link", "show", "dev", ifname)
	if err == nil {
		return true, nil
	}
	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("inspect Linux PPPoE interface %q ownership: %w", ifname, err)
}
