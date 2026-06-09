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
	"fmt"
	"os"
	"path/filepath"
	"sort"
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

type peersFromStatus struct {
	Resource  string `json:"resource"`
	Optional  bool   `json:"optional,omitempty"`
	Phase     string `json:"phase"`
	PeerCount int    `json:"peerCount,omitempty"`
	Reason    string `json:"reason,omitempty"`
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
		config, peersFrom, pendingSources, err := c.buildConfig(group, spec, stateDir)
		if err != nil {
			if statusErr := c.saveStatus(group, config, "Pending", err.Error(), peersFrom, pendingSources); statusErr != nil {
				return statusErr
			}
			return err
		}
		if len(pendingSources) > 0 {
			if err := c.saveStatus(group, config, "Pending", "peersFrom source is not resolved", peersFrom, pendingSources); err != nil {
				return err
			}
			continue
		}
		if c.DryRun {
			if err := c.saveStatus(group, config, "Pending", "DryRun", peersFrom, pendingSources); err != nil {
				return err
			}
			continue
		}
		configPath := filepath.Join(stateDir, "eventd", group, "config.json")
		changed, err := c.writeConfig(configPath, config)
		if err != nil {
			if statusErr := c.saveStatus(group, config, "Pending", err.Error(), peersFrom, pendingSources); statusErr != nil {
				return statusErr
			}
			return err
		}
		if err := c.saveStatus(group, config, "Applied", "", peersFrom, pendingSources); err != nil {
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
func (c Controller) buildConfig(group string, spec api.EventGroupSpec, stateDir string) (eventd.Config, []peersFromStatus, []string, error) {
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
	peers := []eventd.PeerConfig{}
	indexByNode := map[string]int{}
	addPeer := func(peer eventd.PeerConfig) {
		nodeName := strings.TrimSpace(peer.NodeName)
		if nodeName == "" {
			return
		}
		peer.NodeName = nodeName
		peer.Endpoint = strings.TrimSpace(peer.Endpoint)
		if existing, ok := indexByNode[nodeName]; ok {
			peers[existing] = peer
			return
		}
		indexByNode[nodeName] = len(peers)
		peers = append(peers, peer)
	}
	peersFrom, pendingSources, err := c.resolvePeersFrom(spec, addPeer)
	if err != nil {
		config.Peers = peers
		return config, peersFrom, pendingSources, err
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
		addPeer(eventd.PeerConfig{
			NodeName:        strings.TrimSpace(peerSpec.NodeName),
			Endpoint:        strings.TrimSpace(peerSpec.Endpoint),
			Types:           peerSpec.Types,
			SubjectPrefixes: peerSpec.SubjectPrefixes,
		})
	}
	config.Peers = peers
	return config, peersFrom, pendingSources, nil
}

func (c Controller) resolvePeersFrom(spec api.EventGroupSpec, addPeer func(eventd.PeerConfig)) ([]peersFromStatus, []string, error) {
	statuses := make([]peersFromStatus, 0, len(spec.PeersFrom))
	pending := []string{}
	self := strings.TrimSpace(spec.NodeName)
	for _, source := range spec.PeersFrom {
		ref := strings.TrimSpace(source.Resource)
		status := peersFromStatus{
			Resource: ref,
			Optional: source.Optional,
			Phase:    "Resolved",
		}
		nodeSet, found, err := c.samNodeSet(ref)
		if err != nil {
			status.Phase = "Invalid"
			status.Reason = err.Error()
			statuses = append(statuses, status)
			return statuses, pending, err
		}
		if !found {
			status.Phase = "Missing"
			status.Reason = "SAMNodeSet not found"
			statuses = append(statuses, status)
			if !source.Optional {
				pending = append(pending, ref)
			}
			continue
		}
		for _, node := range nodeSet.Nodes {
			nodeRef := strings.TrimSpace(node.NodeRef)
			endpoint := strings.TrimSpace(node.EventEndpoint)
			if nodeRef == "" || nodeRef == self || endpoint == "" {
				continue
			}
			addPeer(eventd.PeerConfig{
				NodeName: nodeRef,
				Endpoint: endpoint,
			})
			status.PeerCount++
		}
		statuses = append(statuses, status)
	}
	sort.Strings(pending)
	return statuses, pending, nil
}

func (c Controller) samNodeSet(ref string) (api.SAMNodeSetSpec, bool, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMNodeSet" || strings.TrimSpace(name) == "" {
		return api.SAMNodeSetSpec{}, false, fmt.Errorf("peersFrom resource must reference SAMNodeSet/<name>")
	}
	if c.Router == nil {
		return api.SAMNodeSetSpec{}, false, nil
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMNodeSet" || resource.Metadata.Name != strings.TrimSpace(name) {
			continue
		}
		spec, err := resource.SAMNodeSetSpec()
		if err != nil {
			return api.SAMNodeSetSpec{}, true, fmt.Errorf("%s spec: %w", ref, err)
		}
		return spec, true, nil
	}
	return api.SAMNodeSetSpec{}, false, nil
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

func (c Controller) saveStatus(group string, config eventd.Config, phase, message string, peersFrom []peersFromStatus, pendingSources []string) error {
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
	if len(peersFrom) > 0 {
		status["peersFrom"] = peersFromStatusMaps(peersFrom)
	}
	if len(pendingSources) > 0 {
		status["pendingSources"] = append([]string(nil), pendingSources...)
	}
	return c.Store.SaveObjectStatus(api.FederationAPIVersion, "EventGroup", group, status)
}

func peersFromStatusMaps(statuses []peersFromStatus) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		item := map[string]any{
			"resource": status.Resource,
			"phase":    status.Phase,
		}
		if status.Optional {
			item["optional"] = true
		}
		if status.PeerCount > 0 {
			item["peerCount"] = status.PeerCount
		}
		if status.Reason != "" {
			item["reason"] = status.Reason
		}
		out = append(out, item)
	}
	return out
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
