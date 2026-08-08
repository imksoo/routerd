// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/vxlan"
)

type VXLANTunnelController struct {
	Router          *api.Router
	Store           Store
	DryRun          bool
	OperatingSystem platform.OS
	Command         vxlan.CommandRunner
}

func (c VXLANTunnelController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	osName := c.OperatingSystem
	if osName == "" {
		osName = platform.CurrentOS()
	}
	if osName != platform.OSLinux {
		return nil
	}
	aliases := bridgeInterfaceAliases(c.Router)
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "VXLANTunnel" {
			continue
		}
		spec, err := resource.VXLANTunnelSpec()
		if err != nil {
			return err
		}
		cfg := vxlan.Config{
			Name:              resource.Metadata.Name,
			IfName:            firstNonEmpty(spec.IfName, resource.Metadata.Name),
			VNI:               spec.VNI,
			LocalAddress:      spec.LocalAddress,
			Peers:             append([]string(nil), spec.Peers...),
			UnderlayInterface: aliases[spec.UnderlayInterface],
			UDPPort:           spec.UDPPort,
			MTU:               spec.MTU,
			Bridge:            aliases[spec.Bridge],
		}
		if cfg.UnderlayInterface == "" {
			cfg.UnderlayInterface = spec.UnderlayInterface
		}
		if spec.Bridge != "" && cfg.Bridge == "" {
			cfg.Bridge = spec.Bridge
		}
		if err := c.reconcileOne(ctx, resource, cfg); err != nil {
			return err
		}
	}
	return nil
}

func (c VXLANTunnelController) reconcileOne(ctx context.Context, resource api.Resource, cfg vxlan.Config) error {
	status := map[string]any{
		"phase":             "Pending",
		"ifname":            cfg.IfName,
		"vni":               cfg.VNI,
		"localAddress":      cfg.LocalAddress,
		"underlayIfname":    cfg.UnderlayInterface,
		"bridgeIfname":      cfg.Bridge,
		"peers":             append([]string(nil), cfg.Peers...),
		"managedBy":         "routerd",
		"restartPersistent": true,
	}
	if c.DryRun {
		status["phase"] = "Planned"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	}
	observed, err := c.command(ctx, "ip", "-details", "link", "show", "dev", cfg.IfName)
	if err != nil {
		if err := (vxlan.Controller{Command: c.command}).Apply(ctx, cfg); err != nil {
			status["phase"] = "Error"
			status["reason"] = "CreateFailed"
			status["error"] = err.Error()
			_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
			return fmt.Errorf("VXLANTunnel/%s create failed: %w", resource.Metadata.Name, err)
		}
		status["created"] = true
	} else if !vxlanDetailsMatch(string(observed), cfg) {
		previous := c.Store.ObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name)
		if statusString(previous, "managedBy") != "routerd" {
			status["phase"] = "RequiresAdoption"
			status["reason"] = "ForeignInterface"
			return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
		}
		if _, err := c.command(ctx, "ip", "link", "delete", "dev", cfg.IfName); err != nil {
			return err
		}
		if err := (vxlan.Controller{Command: c.command}).Apply(ctx, cfg); err != nil {
			return err
		}
		status["recreated"] = true
	} else {
		if cfg.MTU != 0 {
			if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "mtu", fmt.Sprint(cfg.MTU)); err != nil {
				return err
			}
		}
		if cfg.Bridge != "" {
			if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "master", cfg.Bridge); err != nil {
				return err
			}
		}
		if _, err := c.command(ctx, "ip", "link", "set", "dev", cfg.IfName, "up"); err != nil {
			return err
		}
	}
	for _, peer := range cfg.Peers {
		fdb, _ := c.command(ctx, "bridge", "fdb", "show", "dev", cfg.IfName)
		entry := "00:00:00:00:00:00 dst " + peer
		if strings.Contains(string(fdb), entry) {
			continue
		}
		if _, err := c.command(ctx, "bridge", "fdb", "append", "00:00:00:00:00:00", "dev", cfg.IfName, "dst", peer); err != nil {
			return err
		}
	}
	observed, err = c.command(ctx, "ip", "-details", "link", "show", "dev", cfg.IfName)
	if err != nil || !vxlanDetailsMatch(string(observed), cfg) {
		status["phase"] = "Drifted"
		status["reason"] = "ReadbackMismatch"
		if err != nil {
			status["error"] = err.Error()
		}
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
	}
	status["phase"] = "Healthy"
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "VXLANTunnel", resource.Metadata.Name, status)
}

func (c VXLANTunnelController) command(ctx context.Context, name string, args ...string) ([]byte, error) {
	if c.Command != nil {
		return c.Command(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func vxlanDetailsMatch(observed string, cfg vxlan.Config) bool {
	port := cfg.UDPPort
	if port == 0 {
		port = 4789
	}
	for _, want := range []string{
		"vxlan id " + fmt.Sprint(cfg.VNI),
		"local " + cfg.LocalAddress,
		"dev " + cfg.UnderlayInterface,
		"dstport " + fmt.Sprint(port),
		"nolearning",
	} {
		if !strings.Contains(observed, want) {
			return false
		}
	}
	if cfg.MTU != 0 && !strings.Contains(observed, " mtu "+fmt.Sprint(cfg.MTU)+" ") {
		return false
	}
	if cfg.Bridge != "" && !strings.Contains(observed, "master "+cfg.Bridge) {
		return false
	}
	return true
}
