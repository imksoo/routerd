// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/daemonapi"
	"github.com/imksoo/routerd/pkg/nftstate"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

type PathMTUController struct {
	Router *api.Router
	OS     platform.OS
	Bus    interface {
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
	mssData, err := render.NftablesTCPMSSClamp(c.Router)
	if err != nil {
		return err
	}
	l2MSSData, err := render.NftablesL2TCPMSSClamp(c.Router)
	if err != nil {
		return err
	}
	forceFragmentData, err := render.NftablesIPv4ForceFragment(c.Router)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.Path, "/run/routerd/mss.nft")
	l2Path := firstNonEmpty(c.L2Path, "/run/routerd/l2-mss.nft")
	forceFragmentPath := firstNonEmpty(c.ForceFragmentPath, "/run/routerd/forcefrag.nft")
	nft := firstNonEmpty(c.NftCommand, "nft")
	mssChanged, err := c.applyTable(ctx, nft, path, "inet", "routerd_mss", mssData)
	if err != nil {
		return err
	}
	l2MSSChanged, err := c.applyTable(ctx, nft, l2Path, "bridge", "routerd_l2_mss", l2MSSData)
	if err != nil {
		return err
	}
	forceFragmentChanged, err := c.applyTable(ctx, nft, forceFragmentPath, "ip", "routerd_forcefrag", forceFragmentData)
	if err != nil {
		return err
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

func (c PathMTUController) applyTable(ctx context.Context, nft, path, family, table string, data []byte) (bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if !c.DryRun {
			if table != "routerd_l2_mss" && nftstate.RecentlyVerified(path, time.Now().UTC()) {
				return false, nil
			}
			current, listErr := exec.CommandContext(ctx, nft, "list", "table", family, table).CombinedOutput()
			if listErr == nil && len(bytes.TrimSpace(current)) > 0 {
				if !nftTableRouterdOwned(string(current)) {
					return false, fmt.Errorf("refusing to delete unowned nft table %s %s", family, table)
				}
				if out, err := exec.CommandContext(ctx, nft, "delete", "table", family, table).CombinedOutput(); err != nil {
					return false, fmt.Errorf("delete owned nft table %s %s: %w: %s", family, table, err, strings.TrimSpace(string(out)))
				}
			}
			_ = nftstate.MarkVerified(path, time.Now().UTC())
		}
		return false, nil
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
	if table != "routerd_l2_mss" && !changed && nftstate.RecentlyVerified(path, time.Now().UTC()) {
		return false, nil
	}
	current, listErr := exec.CommandContext(ctx, nft, "list", "table", family, table).CombinedOutput()
	missing := listErr != nil
	drifted := false
	if !missing && table == "routerd_l2_mss" {
		if !nftTableRouterdOwned(string(current)) {
			return false, fmt.Errorf("refusing to replace unowned nft table %s %s", family, table)
		}
		desiredDigest := nftMarkerValue(string(data), render.NftablesL2MSSDigestMarker)
		currentDigest := nftMarkerValue(nftTableComment(string(current)), render.NftablesL2MSSDigestMarker)
		drifted = desiredDigest == "" || currentDigest != desiredDigest
	}
	if !changed && !missing && !drifted {
		_ = nftstate.MarkVerified(path, time.Now().UTC())
		return false, nil
	}
	if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
		return changed, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return changed, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	_ = nftstate.MarkVerified(path, time.Now().UTC())
	return changed || missing || drifted, nil
}

func nftMarkerValue(text, marker string) string {
	start := strings.Index(text, marker)
	if start < 0 {
		return ""
	}
	value := text[start+len(marker):]
	if end := strings.IndexAny(value, " \"\\n\\r;}"); end >= 0 {
		value = value[:end]
	}
	return value
}

func nftTableRouterdOwned(text string) bool {
	comment := nftTableComment(text)
	return comment == render.NftablesRouterdOwnerMarker || strings.HasPrefix(comment, render.NftablesRouterdOwnerMarker+" ")
}

func nftTableComment(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "chain ") {
			break
		}
		if !strings.HasPrefix(line, "comment \"") {
			continue
		}
		comment := strings.TrimPrefix(line, "comment \"")
		if end := strings.Index(comment, "\""); end >= 0 {
			return comment[:end]
		}
	}
	return ""
}
