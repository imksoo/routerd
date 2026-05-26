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

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
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
	if err := validateRuleset(ruleset); err != nil {
		return false, err
	}
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
	if !changed && b.rulesetTablesPresent(ctx, ruleset) {
		return false, nil
	}
	if err := b.Reload(ctx, ruleset); err != nil {
		return false, err
	}
	return changed, nil
}

func (b Nftables) rulesetTablesPresent(ctx context.Context, ruleset Ruleset) bool {
	tables := nftRulesetTables(ruleset.Data)
	if len(tables) == 0 {
		return false
	}
	nft := firstNonEmpty(b.Command, "nft")
	for _, table := range tables {
		if _, err := b.run(ctx, nft, "list", "table", table.family, table.name); err != nil {
			return false
		}
	}
	return true
}

func (b Nftables) Reload(ctx context.Context, ruleset Ruleset) error {
	if err := validateRuleset(ruleset); err != nil {
		return err
	}
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
	if err := validateRuleset(ruleset); err != nil {
		return false, err
	}
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
	if err := validateRuleset(ruleset); err != nil {
		return err
	}
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

type nftTableRef struct {
	family string
	name   string
}

func nftRulesetTables(data []byte) []nftTableRef {
	seen := map[string]bool{}
	var tables []nftTableRef
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "table" {
			continue
		}
		table := nftTableRef{family: fields[1], name: fields[2]}
		key := table.family + "/" + table.name
		if seen[key] {
			continue
		}
		seen[key] = true
		tables = append(tables, table)
	}
	return tables
}

func validateRuleset(ruleset Ruleset) error {
	if strings.TrimSpace(ruleset.Path) == "" {
		return fmt.Errorf("%s ruleset path is empty", firstNonEmpty(ruleset.Backend, "firewall"))
	}
	if strings.Contains(ruleset.Path, "\x00") {
		return fmt.Errorf("%s ruleset path contains NUL byte", firstNonEmpty(ruleset.Backend, "firewall"))
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
