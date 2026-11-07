// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"errors"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/wireguard"
)

// Resolve current parent intent without creating keys, publishing source status,
// or running reconciliation. The actual owner stores the acknowledged digest.
func (c WireGuardController) childWireGuardConfig(resource api.Resource) (wireguard.InterfaceConfig, string, error) {
	resolved, err := c.resolvePeerResources()
	if err != nil {
		return wireguard.InterfaceConfig{}, "", err
	}
	if resolved.Router == nil || len(resolved.PendingSources[resource.Metadata.Name]) != 0 {
		return wireguard.InterfaceConfig{}, "", errors.New("WireGuard parent peer source is not ready")
	}
	for _, current := range resolved.Router.Spec.Resources {
		if current.ID() != resource.ID() {
			continue
		}
		cfg, err := wireguard.BuildInterface(current, resolved.Router.Spec.Resources)
		if err != nil {
			return cfg, "", err
		}
		cfg, err = wireguard.ResolveKeyFiles(cfg)
		if err != nil {
			return cfg, "", err
		}
		if cfg.PrivateKey == "" {
			return cfg, "", errors.New("WireGuard parent private key is not ready")
		}
		digest := wireGuardConfigHash(cfg, false)
		if digest == "" {
			return cfg, "", errors.New("WireGuard parent configuration cannot be identified")
		}
		return cfg, digest, nil
	}
	return wireguard.InterfaceConfig{}, "", errors.New("WireGuard parent declaration is no longer current")
}

func (c WireGuardController) observeChildParent(ctx context.Context, resource api.Resource, name, token string) (nativeParentLink, error) {
	absent := nativeParentLink{Declared: true}
	if c.Store == nil {
		return absent, errors.New("WireGuard parent journal is unavailable")
	}
	active := resource.Kind != ""
	var desired wireguard.InterfaceConfig
	var digest string
	var err error
	if active {
		desired, digest, err = c.childWireGuardConfig(resource)
		if err != nil {
			return absent, err
		}
		name = desired.Name
	}
	records, err := c.readNativeWireGuard()
	if err != nil {
		return absent, err
	}
	var record wireGuardNativeRecord
	for _, candidate := range records {
		if candidate.Name == name && (active && candidate.Resource == resource.Metadata.Name || !active && candidate.Token == token) {
			record = candidate
			break
		}
	}
	observed, err := c.observeNativeWireGuard(ctx, name)
	if err != nil {
		return absent, err
	}
	absent.Scope = observed.Scope
	if record.Resource == "" {
		if observed.Exists {
			return absent, errors.New("WireGuard parent is present without its ownership receipt")
		}
		return absent, ctx.Err()
	}
	if record.IfIndex < 1 || active && (record.Pending || record.ConfigHash == "" || record.ConfigHash != digest) {
		return absent, errors.New("WireGuard parent is pending or differs from current intent")
	}
	if err := c.verifyNativeWireGuard(record, observed); err != nil {
		return absent, err
	}
	if !observed.Exists {
		return absent, ctx.Err()
	}
	if active {
		status, err := c.interfaceStatus(ctx, name)
		if err != nil {
			return absent, err
		}
		matches, err := c.interfaceMatchesDesired(ctx, desired, status)
		if err != nil {
			return absent, err
		}
		if !matches || !observed.Up {
			return absent, errors.New("WireGuard parent native configuration differs from applied intent")
		}
	}
	// Config, key-file and native observations can wait. The final proof uses
	// current source, native cookie/index/scope and the same actual receipt.
	observed, err = c.observeNativeWireGuard(ctx, name)
	if err != nil {
		return absent, err
	}
	if err := c.verifyNativeWireGuard(record, observed); err != nil {
		return absent, err
	}
	if !observed.Exists {
		return absent, ctx.Err()
	}
	if active && !observed.Up {
		return absent, errors.New("WireGuard parent stopped during observation")
	}
	if active {
		current, currentDigest, err := c.childWireGuardConfig(resource)
		if err != nil {
			return absent, err
		}
		if currentDigest != digest || current.Name != desired.Name || current.MTU != desired.MTU {
			return absent, errors.New("WireGuard parent source changed while observing")
		}
	}
	records, err = c.readNativeWireGuard()
	if err != nil {
		return absent, err
	}
	found := false
	for _, current := range records {
		if sameWireGuardNativeRecord(current, record) {
			found = true
			break
		}
	}
	if !found {
		return absent, errors.New("WireGuard parent receipt changed while observing")
	}
	if err := c.checkWireGuardScope(ctx, record); err != nil {
		return absent, err
	}
	return nativeParentLink{Declared: true, Exists: true, IfName: name, Index: observed.IfIndex, Scope: record.Scope, Token: "wireguard:" + record.Token}, ctx.Err()
}
