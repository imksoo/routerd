// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

// BridgeController persists Linux bridge declarations through
// systemd-networkd, reloads the backend when artifacts change, and records
// status from the actual kernel links. It deliberately renders only Bridge
// artifacts; unrelated network resources remain owned by their controllers.
type BridgeController struct {
	Router          *api.Router
	Store           Store
	DryRun          bool
	NetworkdDir     string
	OperatingSystem platform.OS
	Command         func(context.Context, string, ...string) error
	Lookup          func(string) (*net.Interface, error)
	ReadFile        func(string) ([]byte, error)
	Readlink        func(string) (string, error)
}

func (c BridgeController) Reconcile(ctx context.Context) error {
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
	files, err := render.NetworkdBridgeFiles(c.Router)
	if err != nil {
		return err
	}
	changed := false
	for _, file := range files {
		path := c.networkdPath(file.Path)
		fileChanged, err := writeFileIfChanged(path, file.Data, 0o644, c.DryRun)
		if err != nil {
			return fmt.Errorf("write Bridge networkd artifact %s: %w", path, err)
		}
		changed = changed || fileChanged
	}
	if changed && !c.DryRun {
		if err := c.command(ctx, "networkctl", "reload"); err != nil {
			return fmt.Errorf("reload systemd-networkd Bridge artifacts: %w", err)
		}
		for _, ifname := range c.bridgeReconfigureOrder() {
			if err := c.command(ctx, "networkctl", "reconfigure", ifname); err != nil {
				return fmt.Errorf("reconfigure Bridge link %s: %w", ifname, err)
			}
		}
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Bridge" {
			continue
		}
		if err := c.saveStatus(resource, changed); err != nil {
			return err
		}
	}
	return nil
}

func (c BridgeController) saveStatus(resource api.Resource, changed bool) error {
	spec, err := resource.BridgeSpec()
	if err != nil {
		return err
	}
	ifname := strings.TrimSpace(spec.IfName)
	if ifname == "" {
		ifname = resource.Metadata.Name
	}
	status := map[string]any{
		"phase":      "Drifted",
		"ifname":     ifname,
		"persistent": true,
		"backend":    "systemd-networkd",
		"changed":    changed,
	}
	if c.DryRun {
		status["phase"] = "Planned"
		status["reason"] = "DryRun"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
	}
	ifi, err := c.lookup(ifname)
	if err != nil {
		status["reason"] = "InterfaceNotFound"
		status["error"] = err.Error()
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
	}
	status["index"] = ifi.Index
	status["mtu"] = ifi.MTU
	status["flags"] = ifi.Flags.String()
	if !c.isKernelBridge(ifname) {
		status["reason"] = "NotBridge"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
	}
	if spec.MTU != 0 && ifi.MTU != spec.MTU {
		status["reason"] = "MTUMismatch"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
	}
	if want := boolInt(api.BoolDefault(spec.STP, true)); c.bridgeSetting(ifname, "stp_state") != want {
		status["reason"] = "STPMismatch"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
	}
	if want := boolInt(api.BoolDefault(spec.MulticastSnooping, false)); c.bridgeSetting(ifname, "multicast_snooping") != want {
		status["reason"] = "MulticastSnoopingMismatch"
		return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
	}
	aliases := bridgeInterfaceAliases(c.Router)
	for _, member := range spec.Members {
		memberIfName := aliases[member]
		if memberIfName == "" {
			memberIfName = member
		}
		if c.memberMaster(memberIfName) != ifname {
			status["reason"] = "MemberNotAttached"
			status["member"] = memberIfName
			return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
		}
	}
	status["phase"] = "Healthy"
	status["members"] = append([]string(nil), spec.Members...)
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "Bridge", resource.Metadata.Name, status)
}

func (c BridgeController) networkdPath(path string) string {
	base := strings.TrimSpace(c.NetworkdDir)
	if base == "" {
		defaults, _ := platform.Current()
		base = defaults.NetworkdDropinDir
	}
	if base == "" {
		base = "/etc/systemd/network"
	}
	rel := strings.TrimPrefix(filepath.Clean(path), filepath.Clean("/etc/systemd/network"))
	return filepath.Join(base, strings.TrimPrefix(rel, string(filepath.Separator)))
}

func (c BridgeController) bridgeReconfigureOrder() []string {
	aliases := bridgeInterfaceAliases(c.Router)
	var out []string
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "Bridge" {
			continue
		}
		spec, err := resource.BridgeSpec()
		if err != nil {
			continue
		}
		for _, member := range spec.Members {
			if ifname := aliases[member]; ifname != "" {
				out = append(out, ifname)
			}
		}
		ifname := spec.IfName
		if ifname == "" {
			ifname = resource.Metadata.Name
		}
		out = append(out, ifname)
	}
	return compactStringList(out)
}

func bridgeInterfaceAliases(router *api.Router) map[string]string {
	out := map[string]string{}
	if router == nil {
		return out
	}
	for _, resource := range router.Spec.Resources {
		switch resource.Kind {
		case "Interface":
			spec, err := resource.InterfaceSpec()
			if err == nil {
				out[resource.Metadata.Name] = spec.IfName
			}
		case "VXLANTunnel":
			spec, err := resource.VXLANTunnelSpec()
			if err == nil {
				out[resource.Metadata.Name] = firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "VXLANSegment":
			spec, err := resource.VXLANSegmentSpec()
			if err == nil {
				out[resource.Metadata.Name] = firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "Bridge":
			spec, err := resource.BridgeSpec()
			if err == nil {
				out[resource.Metadata.Name] = firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		case "WireGuardInterface":
			spec, err := resource.WireGuardInterfaceSpec()
			if err == nil {
				out[resource.Metadata.Name] = firstNonEmpty(spec.IfName, resource.Metadata.Name)
			}
		}
	}
	return out
}

func (c BridgeController) command(ctx context.Context, name string, args ...string) error {
	if c.Command != nil {
		return c.Command(ctx, name, args...)
	}
	return runCommandContext(ctx, name, args...)
}

func (c BridgeController) lookup(ifname string) (*net.Interface, error) {
	if c.Lookup != nil {
		return c.Lookup(ifname)
	}
	return net.InterfaceByName(ifname)
}

func (c BridgeController) readFile(path string) ([]byte, error) {
	if c.ReadFile != nil {
		return c.ReadFile(path)
	}
	return os.ReadFile(path)
}

func (c BridgeController) readlink(path string) (string, error) {
	if c.Readlink != nil {
		return c.Readlink(path)
	}
	return os.Readlink(path)
}

func (c BridgeController) isKernelBridge(ifname string) bool {
	_, err := c.readFile(filepath.Join("/sys/class/net", ifname, "bridge", "stp_state"))
	return err == nil
}

func (c BridgeController) bridgeSetting(ifname, name string) int {
	data, err := c.readFile(filepath.Join("/sys/class/net", ifname, "bridge", name))
	if err != nil {
		return -1
	}
	value, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return value
}

func (c BridgeController) memberMaster(ifname string) string {
	target, err := c.readlink(filepath.Join("/sys/class/net", ifname, "master"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
