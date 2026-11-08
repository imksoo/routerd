// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

type PathMTUController struct {
	Router              *api.Router
	LocalCaptureIntents []dynamicconfig.LocalCaptureIntent
	OS                  platform.OS
	Bus                 interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store             Store
	DryRun            bool
	NftCommand        string
	Path              string
	L2Path            string
	ForceFragmentPath string
}

func (c PathMTUController) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	targetOS := c.OS
	if targetOS == "" {
		targetOS = platform.CurrentOS()
	}
	if targetOS == platform.OSFreeBSD {
		if c.Store == nil {
			return nil
		}
		return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu", map[string]any{
			"phase":     "Skipped",
			"reason":    "path MTU policy is enforced by pf",
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		})
	}
	mssData, err := render.NftablesTCPMSSClampWithLocalCaptureIntents(c.Router, c.LocalCaptureIntents)
	if err != nil {
		return c.savePathMTUError("MSSRenderFailed", err)
	}
	l2MSSData, err := render.NftablesL2TCPMSSClamp(c.Router)
	if err != nil {
		return c.savePathMTUError("L2MSSRenderFailed", err)
	}
	forceFragmentData, err := render.NftablesIPv4ForceFragmentWithLocalCaptureIntents(c.Router, c.LocalCaptureIntents)
	if err != nil {
		return c.savePathMTUError("ForceFragmentRenderFailed", err)
	}
	path := firstNonEmpty(c.Path, "/run/routerd/mss.nft")
	// Keep an implicit L2 artifact beside the regular MSS artifact. This keeps
	// both outputs in the caller-selected dry-run directory while preserving the
	// usual /run/routerd paths for a live controller.
	l2Path := firstNonEmpty(c.L2Path, filepath.Join(filepath.Dir(path), "l2-mss.nft"))
	forceFragmentPath := firstNonEmpty(c.ForceFragmentPath, "/run/routerd/forcefrag.nft")
	nft := firstNonEmpty(c.NftCommand, "nft")
	mssChanged, err := c.applyTable(ctx, nft, path, "inet", "routerd_mss", mssData)
	if err != nil {
		return c.savePathMTUError("MSSApplyFailed", err)
	}
	l2MSSChanged, err := c.applyL2MSSTable(ctx, nft, l2Path, l2MSSData)
	if err != nil {
		return c.savePathMTUError("L2MSSApplyFailed", err)
	}
	forceFragmentChanged, err := c.applyTable(ctx, nft, forceFragmentPath, "ip", "routerd_forcefrag", forceFragmentData)
	if err != nil {
		return c.savePathMTUError("ForceFragmentApplyFailed", err)
	}
	if c.Store != nil {
		status := map[string]any{
			"phase":                   "Applied",
			"nftTable":                "routerd_mss",
			"nftPath":                 path,
			"forceFragmentNftTable":   "routerd_forcefrag",
			"forceFragmentNftPath":    forceFragmentPath,
			"changed":                 mssChanged || l2MSSChanged || forceFragmentChanged,
			"mssChanged":              mssChanged,
			"l2MSSChanged":            l2MSSChanged,
			"l2MSSNftTable":           "routerd_l2_mss",
			"l2MSSNftPath":            l2Path,
			"l2MSSActive":             len(bytes.TrimSpace(l2MSSData)) > 0,
			"forceFragmentChanged":    forceFragmentChanged,
			"forceFragmentIPv4Active": len(bytes.TrimSpace(forceFragmentData)) > 0,
			"dryRun":                  c.DryRun,
			"updatedAt":               time.Now().UTC().Format(time.RFC3339Nano),
		}
		if len(bytes.TrimSpace(mssData)) == 0 && len(bytes.TrimSpace(l2MSSData)) == 0 && len(bytes.TrimSpace(forceFragmentData)) == 0 {
			status["phase"] = "Skipped"
			status["reason"] = "no tunnel path MTU policy derived"
		}
		if err := c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu", status); err != nil {
			return err
		}
	}
	if (mssChanged || l2MSSChanged || forceFragmentChanged) && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.net.path_mtu.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"mssPath": path, "mssTable": "routerd_mss", "l2MSSPath": l2Path, "l2MSSTable": "routerd_l2_mss", "forceFragmentPath": forceFragmentPath, "forceFragmentTable": "routerd_forcefrag"}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c PathMTUController) savePathMTUError(reason string, cause error) error {
	if c.Store != nil {
		if err := c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu", map[string]any{
			"phase": "Error", "reason": reason, "error": cause.Error(),
			"drifted":   strings.Contains(strings.ToLower(cause.Error()), "drift"),
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return fmt.Errorf("%v (saving PathMTU error status: %w)", cause, err)
		}
	}
	return cause
}

func (c PathMTUController) runNFT(ctx context.Context, nft string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, nft, args...).CombinedOutput()
}

func nftListMissing(out []byte, err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(s, "no such file or directory") || strings.Contains(s, "does not exist")
}

func (c PathMTUController) applyL2MSSTable(ctx context.Context, nft, path string, data []byte) (bool, error) {
	if c.DryRun {
		if len(bytes.TrimSpace(data)) == 0 {
			return false, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return false, err
		}
		return writeFileIfChanged(path, data, 0644, false)
	}
	out, err := c.runNFT(ctx, nft, "-j", "list", "tables")
	if err != nil {
		return false, fmt.Errorf("list nft tables: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var listing struct {
		Objects []struct {
			Table *struct {
				Family string `json:"family"`
				Name   string `json:"name"`
			} `json:"table"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &listing); err != nil {
		return false, fmt.Errorf("decode nft tables: %w", err)
	}
	var batch bytes.Buffer
	hadCurrent, hadOld := false, false
	for _, object := range listing.Objects {
		table := object.Table
		if table == nil || table.Family != "bridge" || !strings.HasPrefix(table.Name, "routerd_l2_") {
			continue
		}
		if strings.Trim(table.Name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
			continue
		}
		fmt.Fprintf(&batch, "delete table bridge %s\n", table.Name)
		if table.Name == "routerd_l2_mss" {
			hadCurrent = true
		} else {
			hadOld = true
		}
	}
	batch.Write(data)
	if batch.Len() == 0 {
		return removeFileIfExists(path+".owner", false)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	changed, err := writeFileIfChanged(path, batch.Bytes(), 0644, false)
	if err != nil {
		return false, err
	}
	if out, err := c.runNFT(ctx, nft, "-c", "-f", path); err != nil {
		return false, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := c.runNFT(ctx, nft, "-f", path); err != nil {
		return false, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	removedOwner, err := removeFileIfExists(path+".owner", false)
	if err != nil {
		return false, err
	}
	return changed || removedOwner || hadOld || (hadCurrent != (len(bytes.TrimSpace(data)) > 0)), nil
}

func (c PathMTUController) applyTable(ctx context.Context, nft, path, family, table string, data []byte) (bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if c.DryRun {
			return false, nil
		}
		out, err := c.runNFT(ctx, nft, "list", "table", family, table)
		if nftListMissing(out, err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("list nft table %s %s: %w: %s", family, table, err, strings.TrimSpace(string(out)))
		}
		if out, err := c.runNFT(ctx, nft, "delete", "table", family, table); err != nil {
			return false, fmt.Errorf("delete nft table %s %s: %w: %s", family, table, err, strings.TrimSpace(string(out)))
		}
		return true, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return false, err
	}
	changed, err := writeFileIfChanged(path, data, 0644, false)
	if err != nil {
		return false, err
	}
	if c.DryRun {
		return changed, nil
	}
	out, listErr := c.runNFT(ctx, nft, "list", "table", family, table)
	missing := nftListMissing(out, listErr)
	if listErr != nil && !missing {
		return changed, fmt.Errorf("list nft table %s %s: %w: %s", family, table, listErr, strings.TrimSpace(string(out)))
	}
	// The file and table can both exist while the kernel rules have changed.
	// Reapply the rendered table atomically on every reconciliation.
	if out, err := c.runNFT(ctx, nft, "-c", "-f", path); err != nil {
		return changed, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := c.runNFT(ctx, nft, "-f", path); err != nil {
		return changed, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return changed || missing, nil
}
