// SPDX-License-Identifier: BSD-3-Clause

package vrrp

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

// RefreshInitialRoles runs before startup consumers evaluate when predicates.
// Persisted observations are history; they cannot authorize native effects on a
// rebooted standby. A normal daemon restart still observes an owned live VIP as
// master, preserving its dependent services without withdrawing the dataplane.
func (c *Controller) RefreshInitialRoles(ctx context.Context) error {
	if c.Router == nil || c.Store == nil || c.DryRun || c.currentOS() != platform.OSLinux {
		return nil
	}
	var names []string
	for _, resource := range c.Router.Spec.Resources {
		spec, ok, err := vrrpResourceSpec(resource)
		if err != nil {
			return err
		}
		if ok && spec.Mode == "vrrp" {
			names = append(names, resource.Metadata.Name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	path := stringutil.FirstNonEmpty(c.ConfigPath, "/etc/keepalived/keepalived.conf")
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	configured := err == nil
	if configured && !bytes.HasPrefix(data, []byte(keepalivedConfigHeader)) {
		return fmt.Errorf("keepalived config %s is not owned by routerd", path)
	}
	roles := map[string]string{}
	if configured {
		roles = observeKeepalivedRoles(ctx, c, interfaceAliases(c.Router))
	}
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return err
		}
		previous, err := c.Store.ReadObjectStatus(api.NetAPIVersion, "VirtualAddress", name)
		if err != nil {
			return err
		}
		next := make(map[string]any, len(previous)+2)
		for key, value := range previous {
			next[key] = value
		}
		role := stringutil.FirstNonEmpty(roles[name], "unknown")
		if !configured {
			role = "inactive"
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		next["role"], next["observedAt"] = role, now
		if previous["role"] != role {
			next["lastRoleTransitionAt"] = now
		}
		if role == "backup" || role == "inactive" {
			next["vipAdvertised"] = false
			next["vipPublicationConfirmed"] = false
		}
		if role == "unknown" {
			delete(next, "vipAdvertised")
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "VirtualAddress", name, next); err != nil {
			return err
		}
	}
	return nil
}
