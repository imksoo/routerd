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

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

const sysctlPersistentOwnerKey = routerstate.SysctlPersistentOwnerKey

type sysctlPersistentOwner = routerstate.SysctlPersistentOwner

type sysctlPersistentPlan struct {
	kind, name, resource string
	setting              sysctlSetting
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
		!filepath.IsAbs(owner.Path) || filepath.Clean(owner.Path) != owner.Path || owner.Key == "" || owner.ResourceName == "" || owner.Name != routerstate.SysctlPersistentOwnerName(owner.ResourceName, owner.Key) || strings.ContainsAny(owner.ResourceName+owner.Key, "/\r\n") {
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
		return routerstate.ErrSysctlPersistentOwnerChanged
	}
	return ctx.Err()
}

type sysctlPersistentWriter interface {
	SaveSysctlPersistentOwner(context.Context, string, string, *routerstate.SysctlPersistentOwner, *routerstate.SysctlPersistentOwner, string) error
}

func (s eventedStore) SaveSysctlPersistentOwner(ctx context.Context, kind, name string, previous, next *routerstate.SysctlPersistentOwner, phase string) error {
	writer, ok := s.Store.(sysctlPersistentWriter)
	if !ok {
		return errors.New("persistent sysctl conditional storage is unavailable")
	}
	return writer.SaveSysctlPersistentOwner(ctx, kind, name, previous, next, phase)
}

func (c SysctlController) savePersistentSysctlOwner(ctx context.Context, previous, next *sysctlPersistentOwner, kind, name, phase string) error {
	writer, ok := c.Store.(sysctlPersistentWriter)
	if !ok {
		return errors.New("persistent sysctl conditional storage is unavailable")
	}
	return writer.SaveSysctlPersistentOwner(ctx, kind, name, previous, next, phase)
}

func (c SysctlController) cleanupPersistentSysctls(ctx context.Context, desired map[string]bool) error {
	if c.sysctlOS() != platform.OSFreeBSD || c.DryRun {
		return nil
	}
	lister, ok := c.Store.(interface {
		ListObjectStatuses() ([]routerstate.ObjectStatus, error)
	})
	if !ok {
		return nil
	}
	statuses, err := lister.ListObjectStatuses()
	if err != nil {
		return err
	}
	for _, status := range statuses {
		if status.APIVersion != api.SystemAPIVersion || (status.Kind != "Sysctl" && status.Kind != "SysctlProfile") {
			continue
		}
		owner, err := persistentSysctlOwner(status.Status, status.Kind, status.Name)
		if err != nil {
			return err
		}
		if owner == nil || desired[owner.Kind+"/"+owner.Name] {
			continue
		}
		if err := c.withdrawPersistentSysctl(ctx, *owner); err != nil {
			return err
		}
	}
	return nil
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
	owner := sysctlPersistentOwner{Version: 1, Kind: plan.kind, Name: plan.name, ResourceName: plan.resource, Path: path, Key: plan.setting.Key}
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
		if err := c.savePersistentSysctlOwner(ctx, previous, &owner, plan.kind, plan.name, "PersistPending"); err != nil {
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
	if err := c.savePersistentSysctlOwner(ctx, &owner, &active, plan.kind, plan.name, "PersistentReady"); err != nil {
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
		if err := c.savePersistentSysctlOwner(ctx, &previous, &owner, owner.Kind, owner.Name, "PersistWithdrawing"); err != nil {
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
	return c.savePersistentSysctlOwner(ctx, &owner, nil, owner.Kind, owner.Name, "Removed")
}
