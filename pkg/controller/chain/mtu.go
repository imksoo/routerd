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
	"github.com/imksoo/routerd/pkg/render"
)

type PathMTUController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store      Store
	DryRun     bool
	NftCommand string
	Path       string
}

func (c PathMTUController) Reconcile(ctx context.Context) error {
	if c.Router == nil {
		return nil
	}
	data, err := render.NftablesTCPMSSClamp(c.Router)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.Path, "/run/routerd/mss.nft")
	nft := firstNonEmpty(c.NftCommand, "nft")
	changed, err := c.applyTable(ctx, nft, path, data)
	if err != nil {
		return err
	}
	if c.Store != nil {
		status := map[string]any{
			"phase":     "Applied",
			"nftTable":  "routerd_mss",
			"nftPath":   path,
			"changed":   changed,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}
		if len(bytes.TrimSpace(data)) == 0 {
			status["phase"] = "Skipped"
			status["reason"] = "no tunnel path MTU policy derived"
		}
		if err := c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu", status); err != nil {
			return err
		}
	}
	if changed && c.Bus != nil {
		event := daemonapi.NewEvent(daemonapi.DaemonRef{Name: "routerd", Kind: "routerd", Instance: "controller"}, "routerd.net.path_mtu.applied", daemonapi.SeverityInfo)
		event.Attributes = map[string]string{"path": path, "table": "routerd_mss"}
		if err := c.Bus.Publish(ctx, event); err != nil {
			return err
		}
	}
	return nil
}

func (c PathMTUController) applyTable(ctx context.Context, nft, path string, data []byte) (bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if !c.DryRun {
			_ = exec.CommandContext(ctx, nft, "delete", "table", "inet", "routerd_mss").Run()
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
	if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
		return changed, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	missing := exec.CommandContext(ctx, nft, "list", "table", "inet", "routerd_mss").Run() != nil
	if !changed && !missing {
		return false, nil
	}
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return changed, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return changed || missing, nil
}
