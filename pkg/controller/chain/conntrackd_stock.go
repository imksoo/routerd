// SPDX-License-Identifier: BSD-3-Clause
package chain

import (
	"context"
	"fmt"
	"strings"
)

// Conflicts alone lets a package restart stop the managed synchronization unit.
// Never force replacement of a local unit or release a pre-existing mask.
func (c SystemdUnitController) ensureStockConntrackdMasked(ctx context.Context, command outputCommandFunc) error {
	if c.DryRun {
		return nil
	}
	maskedInactive := func() (bool, error) {
		out, err := command(ctx, "systemctl", "show", "conntrackd.service", "--property=LoadState,ActiveState")
		if err != nil {
			return false, fmt.Errorf("inspect distribution conntrackd: %w: %s", err, strings.TrimSpace(string(out)))
		}
		state := map[string]string{}
		for _, line := range strings.Split(string(out), "\n") {
			k, v, ok := strings.Cut(line, "=")
			if ok {
				state[k] = v
			}
		}
		return state["LoadState"] == "masked" && state["ActiveState"] == "inactive", nil
	}
	ok, err := maskedInactive()
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	out, err := command(ctx, "systemctl", "mask", "--now", "conntrackd.service")
	if err != nil {
		return fmt.Errorf("mask distribution conntrackd: %w: %s", err, strings.TrimSpace(string(out)))
	}
	ok, err = maskedInactive()
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("distribution conntrackd is not masked and inactive")
	}
	return nil
}
