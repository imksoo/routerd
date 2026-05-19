// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"routerd/pkg/api"
	"routerd/pkg/platform"
	"routerd/pkg/render"
)

type CommandFunc func(context.Context, string, ...string) ([]byte, error)

type Ruleset struct {
	Backend       string
	Path          string
	Data          []byte
	InternalHoles int
}

type Backend interface {
	Name() string
	Render(router *api.Router, path string) (Ruleset, error)
	Diff(ruleset Ruleset) (bool, error)
	Apply(ctx context.Context, ruleset Ruleset, dryRun bool) (bool, error)
	Reload(ctx context.Context, ruleset Ruleset) error
}

type Nftables struct {
	Command string
	Run     CommandFunc
}

func (b Nftables) Name() string { return "nftables" }

func (b Nftables) Render(router *api.Router, path string) (Ruleset, error) {
	holes := render.InternalFirewallHoles(router)
	data, err := render.NftablesFirewall(router, holes)
	if err != nil {
		return Ruleset{}, err
	}
	return Ruleset{
		Backend:       b.Name(),
		Path:          firstNonEmpty(path, "/run/routerd/firewall.nft"),
		Data:          data,
		InternalHoles: len(holes),
	}, nil
}

func (b Nftables) Diff(ruleset Ruleset) (bool, error) {
	return fileChanged(ruleset.Path, ruleset.Data)
}

func (b Nftables) Apply(ctx context.Context, ruleset Ruleset, dryRun bool) (bool, error) {
	changed, err := b.Diff(ruleset)
	if err != nil {
		return false, err
	}
	if changed {
		if err := writeRuleset(ruleset.Path, ruleset.Data); err != nil {
			return false, err
		}
	}
	if dryRun {
		return changed, nil
	}
	if err := b.Reload(ctx, ruleset); err != nil {
		return false, err
	}
	return changed, nil
}

func (b Nftables) Reload(ctx context.Context, ruleset Ruleset) error {
	nft := firstNonEmpty(b.Command, "nft")
	if out, err := b.run(ctx, nft, "-c", "-f", ruleset.Path); err != nil {
		return fmt.Errorf("%s -c -f %s: %w: %s", nft, ruleset.Path, err, strings.TrimSpace(string(out)))
	}
	if out, err := b.run(ctx, nft, "-f", ruleset.Path); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", nft, ruleset.Path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (b Nftables) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if b.Run != nil {
		return b.Run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type PF struct {
	Command string
	Run     CommandFunc
}

func (b PF) Name() string { return "pf" }

func (b PF) Render(router *api.Router, path string) (Ruleset, error) {
	holes := render.InternalFirewallHoles(router)
	data, err := render.PF(router, holes)
	if err != nil {
		return Ruleset{}, err
	}
	defaults, _ := platform.Current()
	path = firstNonEmpty(path, filepath.Join(defaults.RuntimeDir, "firewall.pf"))
	if strings.HasSuffix(path, ".nft") {
		path = strings.TrimSuffix(path, ".nft") + ".pf"
	}
	return Ruleset{
		Backend:       b.Name(),
		Path:          path,
		Data:          data,
		InternalHoles: len(holes),
	}, nil
}

func (b PF) Diff(ruleset Ruleset) (bool, error) {
	return fileChanged(ruleset.Path, ruleset.Data)
}

func (b PF) Apply(ctx context.Context, ruleset Ruleset, dryRun bool) (bool, error) {
	changed, err := b.Diff(ruleset)
	if err != nil {
		return false, err
	}
	if changed {
		if err := writeRuleset(ruleset.Path, ruleset.Data); err != nil {
			return false, err
		}
	}
	if dryRun {
		return changed, nil
	}
	if err := b.Reload(ctx, ruleset); err != nil {
		return false, err
	}
	return changed, nil
}

func (b PF) Reload(ctx context.Context, ruleset Ruleset) error {
	pfctl := firstNonEmpty(b.Command, "pfctl")
	if pfctl == "nft" {
		pfctl = "pfctl"
	}
	if out, err := b.run(ctx, pfctl, "-n", "-f", ruleset.Path); err != nil {
		return fmt.Errorf("%s -n -f %s: %w: %s", pfctl, ruleset.Path, err, strings.TrimSpace(string(out)))
	}
	if out, err := b.run(ctx, pfctl, "-f", ruleset.Path); err != nil {
		return fmt.Errorf("%s -f %s: %w: %s", pfctl, ruleset.Path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (b PF) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if b.Run != nil {
		return b.Run(ctx, name, args...)
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func ForPlatform(osName platform.OS, command string) Backend {
	if osName == platform.OSFreeBSD {
		return PF{Command: command}
	}
	return Nftables{Command: command}
}

func fileChanged(path string, data []byte) (bool, error) {
	previous, err := os.ReadFile(path)
	if err != nil {
		return true, nil
	}
	return !bytes.Equal(previous, data), nil
}

func writeRuleset(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
