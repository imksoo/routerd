// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/hostdeps"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const sysctlPersistentOwnerKey = "sysctlPersistentOwner"

type sysctlPersistentOwner struct {
	Version       int    `json:"version"`
	Token         string `json:"token"`
	Kind          string `json:"kind"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Key           string `json:"key"`
	Block         string `json:"block"`
	PreviousBlock string `json:"previousBlock,omitempty"`
	Pending       bool   `json:"pending"`
	Withdrawing   bool   `json:"withdrawing"`
}

type sysctlPersistentPlan struct {
	kind, name string
	setting    sysctlSetting
}

func (c SysctlController) sysctlOS() platform.OS {
	if c.OS != "" {
		return c.OS
	}
	return platform.CurrentOS()
}

func (c SysctlController) freeBSDSysctlPath() string {
	if c.PersistentPath != "" {
		return filepath.Clean(c.PersistentPath)
	}
	if c.BaseDir != "" {
		return filepath.Join(c.BaseDir, "sysctl.conf.local")
	}
	return "/etc/sysctl.conf.local"
}

// Ordinary status refreshes must not erase the concrete persistent owner.
// Only savePersistentSysctlOwner may acquire, replace or release that proof.
func (c SysctlController) saveSysctlStatus(apiVersion, kind, name string, status map[string]any) error {
	if c.sysctlOS() == platform.OSFreeBSD && apiVersion == api.SystemAPIVersion && (kind == "Sysctl" || kind == "SysctlProfile") {
		current, err := c.Store.ReadObjectStatus(apiVersion, kind, name)
		if err != nil {
			return err
		}
		if owner, exists := current[sysctlPersistentOwnerKey]; exists {
			status[sysctlPersistentOwnerKey] = owner
		}
	}
	return c.Store.SaveObjectStatus(apiVersion, kind, name, status)
}

func persistentSysctlOwner(status map[string]any, kind, name string) (*sysctlPersistentOwner, error) {
	raw, exists := status[sysctlPersistentOwnerKey]
	if !exists {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var owner sysctlPersistentOwner
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&owner); err != nil {
		return nil, fmt.Errorf("decode persistent sysctl owner: %w", err)
	}
	token, err := hex.DecodeString(owner.Token)
	if err != nil || len(token) != 16 || owner.Version != 1 || owner.Kind != kind || owner.Name != name ||
		!filepath.IsAbs(owner.Path) || filepath.Clean(owner.Path) != owner.Path || owner.Key == "" {
		return nil, errors.New("invalid persistent sysctl owner identity")
	}
	if kind != "Sysctl" && kind != "SysctlProfile" {
		return nil, errors.New("invalid persistent sysctl owner kind")
	}
	if owner.Withdrawing {
		if !owner.Pending || owner.PreviousBlock == "" {
			return nil, errors.New("invalid persistent sysctl withdrawal")
		}
	} else if owner.Block == "" || (!owner.Pending && owner.PreviousBlock != "") {
		return nil, errors.New("invalid persistent sysctl acquisition")
	}
	for _, block := range []string{owner.Block, owner.PreviousBlock} {
		if block == "" {
			continue
		}
		begin, end := persistentSysctlMarkers(owner)
		lines := strings.Split(block, "\n")
		if len(lines) != 4 || lines[0] != begin || lines[2] != end || lines[3] != "" {
			return nil, errors.New("persistent sysctl owner has an invalid exact block")
		}
		key, value, ok := strings.Cut(lines[1], " = ")
		if !ok || key == "" || value == "" || strings.ContainsAny(key, " \t\r/") {
			return nil, errors.New("persistent sysctl owner has an invalid setting")
		}
		if block == owner.Block && key != owner.Key {
			return nil, errors.New("persistent sysctl owner key differs from its exact block")
		}
	}
	return &owner, nil
}

func persistentSysctlMarkers(owner sysctlPersistentOwner) (string, string) {
	identity := owner.Kind + "/" + owner.Name + " token " + owner.Token
	return "# BEGIN routerd sysctl " + identity, "# END routerd sysctl " + identity
}

func persistentSysctlBlock(owner sysctlPersistentOwner, value string) (string, error) {
	if strings.ContainsAny(owner.Name+owner.Key+value, "\r\n") || value == "" || strings.ContainsAny(owner.Key, " \t/") {
		return "", errors.New("persistent sysctl setting must be one complete line")
	}
	begin, end := persistentSysctlMarkers(owner)
	return begin + "\n" + owner.Key + " = " + value + "\n" + end + "\n", nil
}

func (c SysctlController) readPersistentSysctlOwner(kind, name string) (*sysctlPersistentOwner, error) {
	status, err := c.Store.ReadObjectStatus(api.SystemAPIVersion, kind, name)
	if err != nil {
		return nil, err
	}
	return persistentSysctlOwner(status, kind, name)
}

func (c SysctlController) checkPersistentSysctlOwner(ctx context.Context, expected sysctlPersistentOwner) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := c.readPersistentSysctlOwner(expected.Kind, expected.Name)
	if err != nil {
		return err
	}
	if current == nil || *current != expected {
		return errors.New("persistent sysctl receipt changed while observing")
	}
	return ctx.Err()
}

func (c SysctlController) savePersistentSysctlOwner(previous *sysctlPersistentOwner, next *sysctlPersistentOwner, kind, name, phase string) error {
	status, err := c.Store.ReadObjectStatus(api.SystemAPIVersion, kind, name)
	if err != nil {
		return err
	}
	current, err := persistentSysctlOwner(status, kind, name)
	if err != nil {
		return err
	}
	if (previous == nil) != (current == nil) || (previous != nil && *previous != *current) {
		return errors.New("persistent sysctl receipt changed before persistence")
	}
	updated := make(map[string]any, len(status)+3)
	for key, value := range status {
		updated[key] = value
	}
	if next == nil {
		delete(updated, sysctlPersistentOwnerKey)
		delete(updated, "previousValue")
		updated["changed"] = false
	} else {
		updated[sysctlPersistentOwnerKey] = *next
	}
	updated["phase"], updated["updatedAt"] = phase, time.Now().UTC().Format(time.RFC3339Nano)
	return c.Store.SaveObjectStatus(api.SystemAPIVersion, kind, name, updated)
}

func (c SysctlController) preparePersistentSysctls(ctx context.Context) (map[string]bool, error) {
	if c.sysctlOS() != platform.OSFreeBSD {
		return nil, nil
	}
	var plans []sysctlPersistentPlan
	for _, resource := range hostdeps.SysctlResources(c.Router) {
		switch resource.Kind {
		case "Sysctl":
			spec, err := resource.SysctlSpec()
			if err != nil {
				return nil, err
			}
			if spec.Persistent {
				plans = append(plans, sysctlPersistentPlan{"Sysctl", resource.Metadata.Name, sysctlSetting{Key: spec.Key, Value: spec.Value}})
			}
		case "SysctlProfile":
			spec, err := resource.SysctlProfileSpec()
			if err != nil {
				return nil, err
			}
			if !spec.Persistent {
				continue
			}
			settings, err := sysctlProfileSettings(spec)
			if err != nil {
				return nil, err
			}
			for _, setting := range settings {
				plans = append(plans, sysctlPersistentPlan{"SysctlProfile", resource.Metadata.Name + "-" + safeSysctlName(setting.Key), setting})
			}
		}
	}
	changes := map[string]bool{}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		if len(plans) != 0 {
			return nil, errors.New("persistent FreeBSD sysctls require an ownership status store")
		}
		return changes, nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return nil, err
	}
	existing := map[string]sysctlPersistentOwner{}
	for _, status := range statuses {
		if status.APIVersion != api.SystemAPIVersion || (status.Kind != "Sysctl" && status.Kind != "SysctlProfile") {
			continue
		}
		owner, err := persistentSysctlOwner(status.Status, status.Kind, status.Name)
		if err != nil {
			return nil, c.persistentSysctlFailure(status.Kind, status.Name, err)
		}
		if owner != nil {
			existing[owner.Kind+"/"+owner.Name] = *owner
		}
	}
	for _, plan := range plans {
		changed, err := c.ensurePersistentSysctl(ctx, plan)
		if err != nil {
			return nil, c.persistentSysctlFailure(plan.kind, plan.name, err)
		}
		id := plan.kind + "/" + plan.name
		changes[id] = changed
		delete(existing, id)
	}
	if !c.DryRun {
		for _, owner := range existing {
			if err := c.withdrawPersistentSysctl(ctx, owner); err != nil {
				return nil, c.persistentSysctlFailure(owner.Kind, owner.Name, err)
			}
		}
	}
	return changes, nil
}

func (c SysctlController) persistentSysctlFailure(kind, name string, err error) error {
	saveErr := c.saveSysctlStatus(api.SystemAPIVersion, kind, name, map[string]any{
		"phase": "Error", "reason": "PersistentOwnershipFailed", "error": err.Error(), "updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
	})
	return errors.Join(err, saveErr)
}

func (c SysctlController) ensurePersistentSysctl(ctx context.Context, plan sysctlPersistentPlan) (bool, error) {
	previous, err := c.readPersistentSysctlOwner(plan.kind, plan.name)
	if err != nil {
		return false, err
	}
	path := c.freeBSDSysctlPath()
	if !filepath.IsAbs(path) {
		return false, errors.New("persistent sysctl path must be absolute")
	}
	if previous != nil && (previous.Path != path || previous.Withdrawing) {
		if c.DryRun {
			return true, nil
		}
		if err := c.withdrawPersistentSysctl(ctx, *previous); err != nil {
			return false, err
		}
		previous = nil
	}
	owner := sysctlPersistentOwner{Version: 1, Kind: plan.kind, Name: plan.name, Path: path, Key: plan.setting.Key}
	if previous != nil {
		owner.Token = previous.Token
	} else {
		token := make([]byte, 16)
		if _, err := rand.Read(token); err != nil {
			return false, err
		}
		owner.Token = hex.EncodeToString(token)
	}
	owner.Block, err = persistentSysctlBlock(owner, plan.setting.Value)
	if err != nil {
		return false, err
	}
	snapshot, err := observePersistentSysctl(path)
	if err != nil {
		return false, err
	}
	oldBlock := ""
	if previous != nil {
		oldBlock = previous.Block
		if previous.Withdrawing {
			oldBlock = previous.PreviousBlock
		}
	}
	if previous != nil && previous.Pending {
		if previous.Block != owner.Block {
			if c.DryRun {
				return true, nil
			}
			if err := c.withdrawPersistentSysctl(ctx, *previous); err != nil {
				return false, err
			}
			previous, oldBlock = nil, ""
			token := make([]byte, 16)
			if _, err := rand.Read(token); err != nil {
				return false, err
			}
			owner.Token = hex.EncodeToString(token)
			owner.Block, err = persistentSysctlBlock(owner, plan.setting.Value)
			if err != nil {
				return false, err
			}
			snapshot, err = observePersistentSysctl(path)
			if err != nil {
				return false, err
			}
		} else {
			oldBlock = previous.PreviousBlock
		}
	}
	next, err := replacePersistentSysctlBlock(snapshot.data, owner, oldBlock, owner.Block)
	if err != nil {
		return false, err
	}
	changed := !bytes.Equal(snapshot.data, next)
	if c.DryRun {
		return changed, nil
	}
	if !changed && previous != nil && !previous.Pending {
		if err := c.checkPersistentSysctlOwner(ctx, *previous); err != nil {
			return false, err
		}
		latest, err := observePersistentSysctl(path)
		if err != nil {
			return false, err
		}
		if !samePersistentSysctlSnapshot(snapshot, latest) {
			return false, errors.New("persistent sysctl file changed after receipt observation")
		}
		return false, nil
	}
	if previous != nil && previous.Pending {
		owner = *previous
	} else {
		owner.Pending, owner.PreviousBlock = true, oldBlock
		if err := c.savePersistentSysctlOwner(previous, &owner, plan.kind, plan.name, "PersistPending"); err != nil {
			return false, err
		}
	}
	if err := c.writePersistentSysctl(ctx, owner, snapshot, next); err != nil {
		return false, err
	}
	readback, err := observePersistentSysctl(path)
	if err != nil {
		return false, err
	}
	block, _, _, err := findPersistentSysctlBlock(readback.data, owner)
	if err != nil {
		return false, err
	}
	if block != owner.Block {
		return false, errors.New("persistent sysctl block is absent or changed after write")
	}
	if _, err := replacePersistentSysctlBlock(readback.data, owner, owner.Block, owner.Block); err != nil {
		return false, err
	}
	if err := c.checkPersistentSysctlOwner(ctx, owner); err != nil {
		return false, err
	}
	active := owner
	active.Pending, active.PreviousBlock = false, ""
	if err := c.savePersistentSysctlOwner(&owner, &active, plan.kind, plan.name, "PersistentReady"); err != nil {
		return false, err
	}
	return changed, nil
}

func (c SysctlController) withdrawPersistentSysctl(ctx context.Context, previous sysctlPersistentOwner) error {
	owner := previous
	if !owner.Withdrawing {
		owner.Pending, owner.Withdrawing = true, true
		if !previous.Pending || previous.PreviousBlock == "" {
			owner.PreviousBlock, owner.Block = previous.Block, ""
		}
		if err := c.savePersistentSysctlOwner(&previous, &owner, owner.Kind, owner.Name, "PersistWithdrawing"); err != nil {
			return err
		}
	}
	snapshot, err := observePersistentSysctl(owner.Path)
	if err != nil {
		return err
	}
	next, err := replacePersistentSysctlBlock(snapshot.data, owner, owner.PreviousBlock, "")
	if err != nil {
		return err
	}
	if err := c.writePersistentSysctl(ctx, owner, snapshot, next); err != nil {
		return err
	}
	observed, err := observePersistentSysctl(owner.Path)
	if err != nil {
		return err
	}
	block, _, _, err := findPersistentSysctlBlock(observed.data, owner)
	if err != nil {
		return err
	}
	if block != "" {
		return errors.New("persistent sysctl block remains after withdrawal")
	}
	if err := c.checkPersistentSysctlOwner(ctx, owner); err != nil {
		return err
	}
	return c.savePersistentSysctlOwner(&owner, nil, owner.Kind, owner.Name, "Removed")
}
