// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
)

const conntrackdConfigDir = "/etc/conntrackd"

func conntrackdUnitName(name string) string { return "routerd-conntrackd@" + name + ".service" }

func (c SystemdUnitController) cleanupStaleConntrackdSyncUnits(ctx context.Context, configDir string, command outputCommandFunc) error {
	desired := map[string]bool{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "NAT44SessionSync" {
			continue
		}
		spec, err := resource.NAT44SessionSyncSpec()
		if err != nil {
			return err
		}
		if spec.Mode == "conntrackd" {
			desired[conntrackdUnitName(resource.Metadata.Name)] = true
		}
	}
	paths, err := systemdArtifactPaths(c.SystemdSystemDir, "routerd-conntrackd@*.service")
	if err != nil {
		return err
	}
	if c.Store == nil && len(paths) > 0 {
		return fmt.Errorf("conntrackd removal status store unavailable")
	}
	for _, unitPath := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		unit := filepath.Base(unitPath)
		if desired[unit] {
			continue
		}
		name := strings.TrimSuffix(strings.TrimPrefix(unit, "routerd-conntrackd@"), ".service")
		configPath := filepath.Join(configDir, "routerd-"+name+".conf")
		for _, path := range []string{unitPath, configPath} {
			if err := validateRouterdArtifactOwnership(path); err != nil {
				return err
			}
		}
		if c.DryRun {
			continue
		}
		// Retain the receipt through supervisor acknowledgement, file removal,
		// reload and status publication. A new process can retry any failed step.
		if err := beginArtifactRemoval(unitPath); err != nil {
			return err
		}
		if _, statErr := os.Stat(unitPath); os.IsNotExist(statErr) {
			// A previous attempt already disabled and unlinked this unit. systemd
			// rejects disable on a missing file, so confirm native absence or stop
			// any still-loaded instance before completing the pending receipt.
			out, err := command(ctx, "systemctl", "show", unit, "--property=LoadState,ActiveState")
			if err != nil {
				return err
			}
			fields := map[string]string{}
			for _, line := range strings.Split(string(out), "\n") {
				key, value, ok := strings.Cut(line, "=")
				if ok {
					fields[key] = value
				}
			}
			if fields["LoadState"] != "not-found" || fields["ActiveState"] != "inactive" {
				if _, err := command(ctx, "systemctl", "stop", unit); err != nil {
					return err
				}
			}
		} else if statErr != nil {
			return statErr
		} else if _, err := command(ctx, "systemctl", "disable", "--now", unit); err != nil {
			return fmt.Errorf("stop removed conntrackd %s: %w", name, err)
		}
		if _, err := removeFileIfExists(configPath, false); err != nil {
			return err
		}
		if _, err := removeFileIfExists(unitPath, false); err != nil {
			return err
		}
		if _, err := command(ctx, "systemctl", "daemon-reload"); err != nil {
			return err
		}
		if err := c.Store.SaveObjectStatus(api.SystemAPIVersion, "ServiceUnit", unit, map[string]any{"phase": "Removed", "unitName": unit, "path": unitPath, "source": "NAT44SessionSync/" + name, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
			return err
		}
		if err := clearArtifactActivation(unitPath); err != nil {
			return err
		}
	}
	if c.DryRun || c.Store == nil {
		return nil
	}
	return c.cleanupRemovedSystemdResourceStatuses("NAT44SessionSync", conntrackdUnitName)
}
