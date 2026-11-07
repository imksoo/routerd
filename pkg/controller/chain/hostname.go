// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/platform"
)

// HostnameController enforces the explicitly managed singleton hostname.
// Removing management stops enforcement; it does not invent a prior hostname.
type HostnameController struct {
	Router *api.Router
	Store  Store
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Command    outputCommandFunc
	ConfigPath string
	OS         platform.OS
	DryRun     bool
}

func (c HostnameController) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	var managed []api.Resource
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Hostname" {
			continue
		}
		spec, err := resource.HostnameSpec()
		if err != nil {
			return err
		}
		if spec.Managed {
			managed = append(managed, resource)
		} else if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Hostname", resource.Metadata.Name, map[string]any{
			"phase": "Skipped", "reason": "Unmanaged", "managed": false, "updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}
	if len(managed) > 1 {
		return errors.New("multiple managed Hostname resources are not supported")
	}
	if len(managed) == 0 {
		return nil
	}
	resource := managed[0]
	spec, err := resource.HostnameSpec()
	if err != nil {
		return err
	}
	command := c.Command
	if command == nil {
		command = runOutputCommandContext
	}
	status := map[string]any{"hostname": spec.Hostname, "managed": true, "dryRun": c.DryRun}
	fail := func(reason string, operationErr error) error {
		status["phase"], status["reason"], status["error"] = "Error", reason, operationErr.Error()
		status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		if saveErr := c.Store.SaveObjectStatus(api.NetAPIVersion, "Hostname", resource.Metadata.Name, status); saveErr != nil {
			return errors.Join(operationErr, saveErr)
		}
		return operationErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	out, err := command(ctx, "hostname")
	if err != nil {
		return fail("ObserveFailed", fmt.Errorf("read hostname: %w", err))
	}
	current := strings.TrimSpace(string(out))
	if strings.ContainsAny(current, " \t\r\n") {
		return fail("ObserveFailed", errors.New("hostname observation contains whitespace"))
	}
	status["currentValue"] = current
	if c.DryRun {
		status["phase"], status["reason"] = "Rendered", "DryRun"
		status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Hostname", resource.Metadata.Name, status)
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	path := c.ConfigPath
	changed := false
	switch targetOS {
	case platform.OSLinux:
		if path == "" {
			path = "/etc/hostname"
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		changed, err = writeFileIfChanged(path, []byte(spec.Hostname+"\n"), 0644, false)
		if err != nil {
			return fail("PersistFailed", fmt.Errorf("persist hostname: %w", err))
		}
	case platform.OSFreeBSD:
		if path == "" {
			path = "/etc/rc.conf"
		}
		out, err := command(ctx, "sysrc", "-f", path, "-i", "-n", "hostname")
		if err != nil {
			return fail("ObserveFailed", fmt.Errorf("read persistent hostname: %w", err))
		}
		if strings.TrimSpace(string(out)) != spec.Hostname {
			if _, err := command(ctx, "sysrc", "-f", path, "hostname="+spec.Hostname); err != nil {
				return fail("PersistFailed", fmt.Errorf("persist hostname: %w", err))
			}
			out, err = command(ctx, "sysrc", "-f", path, "-i", "-n", "hostname")
			if err != nil {
				return fail("PersistFailed", fmt.Errorf("read persistent hostname after change: %w", err))
			}
			if strings.TrimSpace(string(out)) != spec.Hostname {
				return fail("PersistFailed", errors.New("persistent hostname readback does not match"))
			}
			changed = true
		}
	default:
		return fail("UnsupportedOS", fmt.Errorf("hostname runtime is not supported on %s", targetOS))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if current != spec.Hostname {
		if _, err := command(ctx, "hostname", spec.Hostname); err != nil {
			return fail("ApplyFailed", fmt.Errorf("set hostname: %w", err))
		}
		changed = true
	}
	out, err = command(ctx, "hostname")
	if err != nil {
		return fail("ObserveFailed", fmt.Errorf("read hostname after change: %w", err))
	}
	if strings.TrimSpace(string(out)) != spec.Hostname {
		return fail("ObserveFailed", errors.New("hostname readback does not match"))
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	status["phase"], status["currentValue"], status["changed"], status["path"] = "Applied", spec.Hostname, changed, path
	status["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "Hostname", resource.Metadata.Name, status); err != nil {
		return err
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.system.hostname.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "Hostname", Name: resource.Metadata.Name}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}
