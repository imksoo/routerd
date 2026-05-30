// SPDX-License-Identifier: BSD-3-Clause

// Package eventfederation holds the controller that renders runtime config for
// the CloudEdge Event Federation daemon (routerd-eventd, ADR 0006 Phase 2).
//
// It mirrors the DNSResolver dual-controller pattern: this controller writes
// config.json per EventGroup, while the SystemdUnitController owns the
// routerd-eventd@<group>.service lifecycle. It never starts/stops the process
// itself. With no EventGroup resource it is a no-op (additive, zero-regression).
package eventfederation

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/bus"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/eventd"
	"github.com/imksoo/routerd/pkg/platform"
)

// Store is the minimal status persistence surface used by the controller.
type Store interface {
	SaveObjectStatus(apiVersion, kind, name string, status map[string]any) error
	ObjectStatus(apiVersion, kind, name string) map[string]any
}

// Controller renders routerd-eventd runtime config from EventGroup/EventPeer
// resources. It mirrors dnsresolver.Controller.
type Controller struct {
	Router *api.Router
	Bus    *bus.Bus
	Store  Store
	DryRun bool

	RuntimeDir string
	StateDir   string
}

// HandleEvent reconciles in response to a bus event (bridge for FuncController).
func (c Controller) HandleEvent(ctx context.Context, _ daemonapi.DaemonEvent) error {
	return c.Reconcile(ctx)
}

// Reconcile renders config.json for each EventGroup. With no EventGroup it does
// nothing and returns nil.
func (c Controller) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	_, stateDir := c.dirs()
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "EventGroup" {
			continue
		}
		spec, err := resource.EventGroupSpec()
		if err != nil {
			return err
		}
		group := resource.Metadata.Name
		config := c.buildConfig(group, spec, stateDir)
		if c.DryRun {
			if err := c.saveStatus(group, config, "Pending", "DryRun"); err != nil {
				return err
			}
			continue
		}
		configPath := filepath.Join(stateDir, "eventd", group, "config.json")
		changed, err := c.writeConfig(configPath, config)
		if err != nil {
			if statusErr := c.saveStatus(group, config, "Pending", err.Error()); statusErr != nil {
				return statusErr
			}
			return err
		}
		if err := c.saveStatus(group, config, "Applied", ""); err != nil {
			return err
		}
		if changed && c.Bus != nil {
			event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.event.federation.configured", daemonapi.SeverityInfo)
			event.Resource = &daemonapi.ResourceRef{APIVersion: api.FederationAPIVersion, Kind: "EventGroup", Name: group}
			_ = c.Bus.Publish(ctx, event)
		}
	}
	return nil
}

// buildConfig assembles the eventd runtime config for a single group, gathering
// the EventPeers whose GroupRef matches.
func (c Controller) buildConfig(group string, spec api.EventGroupSpec, stateDir string) eventd.Config {
	config := eventd.Config{
		NodeName:      strings.TrimSpace(spec.NodeName),
		Group:         group,
		Listen:        eventd.Listen{Address: strings.TrimSpace(spec.Listen.Address), Port: spec.Listen.Port},
		SecretFile:    strings.TrimSpace(spec.Auth.SecretFile),
		StatePath:     filepath.Join(stateDir, "routerd.db"),
		PruneInterval: eventd.DefaultPruneInterval,
	}
	if window := strings.TrimSpace(spec.ReplayWindow); window != "" {
		if d, err := time.ParseDuration(window); err == nil {
			config.ReplayWindow = d
		}
	}
	if config.ReplayWindow <= 0 {
		config.ReplayWindow = eventd.DefaultReplayWindow
	}
	config.Retention.MaxEvents = spec.Retention.MaxEvents
	if maxAge := strings.TrimSpace(spec.Retention.MaxAge); maxAge != "" {
		if d, err := time.ParseDuration(maxAge); err == nil {
			config.Retention.MaxAge = d
		}
	}
	config.PushRetry = eventd.PushRetry{
		MaxAttempts: eventd.DefaultMaxAttempts,
		BaseBackoff: eventd.DefaultBaseBackoff,
		MaxBackoff:  eventd.DefaultMaxBackoff,
	}
	for _, peer := range c.Router.Spec.Resources {
		if peer.Kind != "EventPeer" {
			continue
		}
		peerSpec, err := peer.EventPeerSpec()
		if err != nil {
			continue
		}
		if strings.TrimSpace(peerSpec.GroupRef) != group {
			continue
		}
		config.Peers = append(config.Peers, eventd.PeerConfig{
			NodeName:        strings.TrimSpace(peerSpec.NodeName),
			Endpoint:        strings.TrimSpace(peerSpec.Endpoint),
			Types:           peerSpec.Types,
			SubjectPrefixes: peerSpec.SubjectPrefixes,
		})
	}
	return config
}

// writeConfig writes config.json idempotently, reporting whether it changed.
func (c Controller) writeConfig(configPath string, config eventd.Config) (bool, error) {
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		return false, err
	}
	data, err := eventd.MarshalConfigJSON(config)
	if err != nil {
		return false, err
	}
	current, readErr := os.ReadFile(configPath)
	if readErr == nil && string(bytes.TrimSpace(current)) == string(data) {
		return false, nil
	}
	if err := os.WriteFile(configPath, append(data, '\n'), 0o644); err != nil {
		return false, err
	}
	return true, nil
}

func (c Controller) saveStatus(group string, config eventd.Config, phase, message string) error {
	status := map[string]any{
		"phase":     phase,
		"group":     group,
		"nodeName":  config.NodeName,
		"peers":     len(config.Peers),
		"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if strings.TrimSpace(config.Listen.Address) != "" {
		status["listenAddress"] = config.Listen.Address
		status["listenPort"] = config.Listen.Port
	}
	if message != "" {
		status["message"] = message
		status["reason"] = message
	}
	return c.Store.SaveObjectStatus(api.FederationAPIVersion, "EventGroup", group, status)
}

func (c Controller) dirs() (runtimeDir, stateDir string) {
	defaults, _ := platform.Current()
	runtimeDir = strings.TrimRight(c.RuntimeDir, "/")
	if runtimeDir == "" {
		runtimeDir = defaults.RuntimeDir
	}
	stateDir = strings.TrimRight(c.StateDir, "/")
	if stateDir == "" {
		stateDir = defaults.StateDir
	}
	return runtimeDir, stateDir
}
