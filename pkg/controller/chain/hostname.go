// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

type HostnameController struct {
	Router *api.Router
	Store  Store
	DryRun bool
}

func (c HostnameController) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	var selected *api.Resource
	var desired string
	for i := range c.Router.Spec.Resources {
		resource := &c.Router.Spec.Resources[i]
		if resource.APIVersion != api.NetAPIVersion || resource.Kind != "Hostname" {
			continue
		}
		spec, err := resource.HostnameSpec()
		if err != nil {
			return err
		}
		if !spec.Managed {
			continue
		}
		if selected != nil {
			return fmt.Errorf("multiple managed Hostname resources are not supported")
		}
		selected, desired = resource, spec.Hostname
	}
	if selected == nil {
		return nil
	}
	current, err := os.Hostname()
	if err != nil {
		return c.fail(selected, desired, fmt.Errorf("observe hostname: %w", err))
	}
	static := current
	if platform.CurrentOS() == platform.OSLinux {
		static, err = staticHostname(ctx)
		if err != nil {
			return c.fail(selected, desired, err)
		}
	}
	changed := current != desired || static != desired
	if changed && !c.DryRun {
		commands := [][]string{{"hostnamectl", "set-hostname", "--", desired}}
		if platform.CurrentOS() == platform.OSFreeBSD {
			commands = [][]string{{"sysrc", "hostname=" + desired}, {"hostname", desired}}
		}
		for _, command := range commands {
			out, err := exec.CommandContext(ctx, command[0], command[1:]...).CombinedOutput()
			if err != nil {
				return c.fail(selected, desired, fmt.Errorf("%s: %w: %s", command[0], err, strings.TrimSpace(string(out))))
			}
		}
		current, err = os.Hostname()
		if err != nil {
			return c.fail(selected, desired, fmt.Errorf("observe hostname after apply: %w", err))
		}
		if platform.CurrentOS() == platform.OSLinux {
			static, err = staticHostname(ctx)
			if err != nil {
				return c.fail(selected, desired, err)
			}
		} else {
			static = current
		}
	}
	phase := "Applied"
	if current != desired || static != desired {
		phase = "Drifted"
	}
	if c.Store != nil {
		if err := c.Store.SaveObjectStatus(selected.APIVersion, selected.Kind, selected.Metadata.Name, map[string]any{
			"phase": phase, "desired": desired, "current": current, "static": static, "changed": changed, "dryRun": c.DryRun,
		}); err != nil {
			return err
		}
	}
	if phase == "Drifted" && !c.DryRun {
		return fmt.Errorf("hostname remains %q (static %q), want %q", current, static, desired)
	}
	return nil
}

func (c HostnameController) fail(resource *api.Resource, desired string, err error) error {
	if c.Store == nil {
		return err
	}
	return errors.Join(err, c.Store.SaveObjectStatus(resource.APIVersion, resource.Kind, resource.Metadata.Name, map[string]any{
		"phase": "Error", "desired": desired, "error": err.Error(), "dryRun": c.DryRun,
	}))
}

func staticHostname(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "hostnamectl", "status", "--static").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("observe static hostname: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
