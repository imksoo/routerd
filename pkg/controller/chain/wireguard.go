// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"routerd/pkg/api"
	"routerd/pkg/bus"
	"routerd/pkg/daemonapi"
	"routerd/pkg/wireguard"
)

type WireGuardController struct {
	Router  *api.Router
	Bus     *bus.Bus
	Store   Store
	DryRun  bool
	Command wireguard.CommandRunner
	Logger  *slog.Logger
}

func (c WireGuardController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "WireGuardInterface" {
			continue
		}
		if err := c.reconcileInterface(ctx, resource); err != nil {
			return err
		}
	}
	return nil
}

func (c WireGuardController) reconcileInterface(ctx context.Context, resource api.Resource) error {
	cfg, err := wireguard.BuildInterface(resource, c.Router.Spec.Resources)
	if err != nil {
		return err
	}
	status := map[string]any{
		"phase":      "Pending",
		"interface":  cfg.Name,
		"ifname":     cfg.Name,
		"listenPort": cfg.ListenPort,
		"mtu":        cfg.MTU,
		"fwmark":     cfg.FwMark,
		"peerCount":  len(cfg.Peers),
		"dryRun":     c.DryRun,
	}
	if cfg.PrivateKey == "" && cfg.PrivateKeyFile == "" {
		status["reason"] = "PrivateKeyMissing"
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfacePending")
		return nil
	}
	controller := wireguard.Controller{Command: c.Command, DryRun: c.DryRun}
	if _, err := controller.Apply(ctx, cfg); err != nil {
		status["phase"] = "Error"
		status["reason"] = "ApplyFailed"
		status["error"] = err.Error()
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
			return err
		}
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "InterfaceError")
		return nil
	}
	status["phase"] = "Up"
	if c.DryRun {
		status["phase"] = "Planned"
	}
	observed, err := c.interfaceStatus(ctx, cfg.Name)
	if err == nil {
		if observed.PublicKey != "" {
			status["publicKey"] = observed.PublicKey
		}
		if observed.ListenPort != 0 {
			status["listenPort"] = observed.ListenPort
		}
		if observed.FwMark != "" {
			status["fwmark"] = observed.FwMark
		}
		status["peerCount"] = len(observed.Peers)
		c.savePeerObservedStatuses(resource.Metadata.Name, cfg.Peers, observed.Peers)
	} else if !c.DryRun {
		status["statusError"] = err.Error()
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "StatusUnavailable")
	} else {
		c.savePeerPendingStatuses(resource.Metadata.Name, cfg.Peers, "DryRun")
	}
	changed := statusChanged(c.Store.ObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name), status)
	if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardInterface", resource.Metadata.Name, status); err != nil {
		return err
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.wireguard.interface.applied", daemonapi.SeverityInfo)
		event.Resource = &daemonapi.ResourceRef{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface", Name: resource.Metadata.Name}
		event.Attributes = map[string]string{
			"interface": cfg.Name,
			"peers":     fmt.Sprintf("%d", len(cfg.Peers)),
			"dryRun":    fmt.Sprintf("%t", c.DryRun),
		}
		return c.Bus.Publish(ctx, event)
	}
	return nil
}

func (c WireGuardController) interfaceStatus(ctx context.Context, ifname string) (wireguard.InterfaceStatus, error) {
	run := c.Command
	if run == nil {
		run = wireguard.DefaultCommandRunner
	}
	out, err := run(ctx, "wg", "show", ifname, "dump")
	if err != nil {
		return wireguard.InterfaceStatus{Name: ifname}, err
	}
	return wireguard.ParseInterfaceDump(ifname, out)
}

func (c WireGuardController) savePeerPendingStatuses(iface string, peers []wireguard.PeerConfig, reason string) {
	for _, peer := range peers {
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", peer.Name, map[string]any{
			"phase":      "Pending",
			"reason":     reason,
			"interface":  iface,
			"publicKey":  peer.PublicKey,
			"allowedIPs": append([]string(nil), peer.AllowedIPs...),
			"endpoint":   peer.Endpoint,
			"dryRun":     c.DryRun,
		})
	}
}

func (c WireGuardController) savePeerObservedStatuses(iface string, desired []wireguard.PeerConfig, observed []wireguard.PeerStatus) {
	byKey := map[string]wireguard.PeerStatus{}
	for _, peer := range observed {
		byKey[peer.PublicKey] = peer
	}
	for _, peer := range desired {
		status := map[string]any{
			"phase":               "Configured",
			"interface":           iface,
			"publicKey":           peer.PublicKey,
			"allowedIPs":          append([]string(nil), peer.AllowedIPs...),
			"endpoint":            peer.Endpoint,
			"persistentKeepalive": peer.PersistentKeepalive,
			"dryRun":              c.DryRun,
		}
		if got, ok := byKey[peer.PublicKey]; ok {
			if got.LatestEndpoint != "" {
				status["latestEndpoint"] = got.LatestEndpoint
			}
			if !got.LatestHandshake.IsZero() {
				status["phase"] = "Connected"
				status["latestHandshake"] = got.LatestHandshake.Format(time.RFC3339)
				status["handshakeAgeSeconds"] = int(time.Since(got.LatestHandshake).Seconds())
			} else if strings.TrimSpace(peer.Endpoint) != "" {
				status["phase"] = "Waiting"
				status["reason"] = "NoHandshakeYet"
			}
			status["transferRxBytes"] = got.TransferRxBytes
			status["transferTxBytes"] = got.TransferTxBytes
		} else {
			status["phase"] = "Pending"
			status["reason"] = "PeerNotObserved"
		}
		_ = c.Store.SaveObjectStatus(api.NetAPIVersion, "WireGuardPeer", peer.Name, status)
	}
}
