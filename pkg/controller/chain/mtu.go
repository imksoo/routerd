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

	"routerd/pkg/api"
	"routerd/pkg/daemonapi"
	"routerd/pkg/render"
)

type PathMTUPolicyController struct {
	Router *api.Router
	Bus    interface {
		Publish(context.Context, daemonapi.DaemonEvent) error
	}
	Store      Store
	DryRun     bool
	NftCommand string
	Path       string
}

func (c PathMTUPolicyController) Reconcile(ctx context.Context) error {
	if c.Router == nil || c.Store == nil {
		return nil
	}
	router := c.renderRouter()
	data, err := render.NftablesIPv4SourceNAT(router)
	if err != nil {
		return err
	}
	path := firstNonEmpty(c.Path, "/run/routerd/mss.nft")
	nft := firstNonEmpty(c.NftCommand, "nft")
	changed, err := c.applyTable(ctx, nft, path, data)
	if err != nil {
		return err
	}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "PathMTUPolicy" {
			continue
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "PathMTUPolicy", resource.Metadata.Name, map[string]any{
			"phase":     "Applied",
			"nftTable":  "routerd_mss",
			"nftPath":   path,
			"changed":   changed,
			"dryRun":    c.DryRun,
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
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

func (c PathMTUPolicyController) renderRouter() *api.Router {
	out := &api.Router{TypeMeta: c.Router.TypeMeta, Metadata: c.Router.Metadata}
	for _, resource := range c.Router.Spec.Resources {
		switch resource.Kind {
		case "Interface", "PPPoEInterface", "DSLiteTunnel", "PathMTUPolicy":
			out.Spec.Resources = append(out.Spec.Resources, resource)
		}
	}
	return out
}

func (c PathMTUPolicyController) applyTable(ctx context.Context, nft, path string, data []byte) (bool, error) {
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
	if changed {
		if out, err := exec.CommandContext(ctx, nft, "-c", "-f", path).CombinedOutput(); err != nil {
			return changed, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
		}
	}
	if !changed && exec.CommandContext(ctx, nft, "list", "table", "inet", "routerd_mss").Run() == nil {
		return false, nil
	}
	_ = exec.CommandContext(ctx, nft, "delete", "table", "inet", "routerd_mss").Run()
	if out, err := exec.CommandContext(ctx, nft, "-f", path).CombinedOutput(); err != nil {
		return changed, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}
