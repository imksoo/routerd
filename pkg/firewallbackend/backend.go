// SPDX-License-Identifier: BSD-3-Clause

package firewallbackend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/nftstate"
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
	Render(router *api.Router, path string, intents []dynamicconfig.LocalCaptureIntent) (Ruleset, error)
	Diff(ruleset Ruleset) (bool, error)
	Apply(ctx context.Context, ruleset Ruleset, dryRun bool) (bool, error)
	Reload(ctx context.Context, ruleset Ruleset) error
}

type Nftables struct {
	Command                   string
	Run                       CommandFunc
	LegacyFirewallHandoffPath string
}

func (b Nftables) Name() string { return "nftables" }

func (b Nftables) Render(router *api.Router, path string, _ []dynamicconfig.LocalCaptureIntent) (Ruleset, error) {
	holes := render.InternalFirewallHoles(router, platform.OSLinux)
	data, err := render.NftablesFirewall(router, holes)
	if err != nil {
		return Ruleset{}, err
	}
	return Ruleset{
		Backend:       b.Name(),
		Path:          stringutil.FirstNonEmpty(path, "/run/routerd/firewall.nft"),
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
	if len(ruleset.Data) == 0 {
		return b.withdraw(ctx, ruleset, dryRun)
	}
	adoptedLegacy, err := b.adoptRecordedLegacyFirewall(ctx, dryRun)
	if err != nil {
		return false, err
	}
	changed, err := b.Diff(ruleset)
	if err != nil {
		return false, err
	}
	if changed {
		if err := nftstate.Invalidate(ruleset.Path); err != nil {
			return false, err
		}
		if err := writeRuleset(ruleset.Path, ruleset.Data); err != nil {
			return false, err
		}
	}
	if dryRun {
		return changed, nil
	}
	if !changed && !adoptedLegacy && nftstate.RecentlyVerified(ruleset.Path, time.Now().UTC()) {
		return false, nil
	}
	if err := b.Reload(ctx, ruleset); err != nil {
		return false, err
	}
	if adoptedLegacy {
		if err := os.Remove(b.LegacyFirewallHandoffPath); err != nil && !os.IsNotExist(err) {
			return false, fmt.Errorf("remove consumed legacy firewall handoff: %w", err)
		}
	}
	return changed || adoptedLegacy, nftstate.MarkVerified(ruleset.Path, time.Now().UTC())
}

const legacyFirewallHandoffVersion = "routerd-v1-firewall-handoff/v1"
const legacyFirewallHandoffCommit = "e3b49dbb"

// adoptRecordedLegacyFirewall transfers the one legacy filter table created by
// the pinned 1.0 release. It is intentionally unavailable without the
// installer-written handoff record: a same-named unmarked table is otherwise
// foreign and must remain untouched.
func (b Nftables) adoptRecordedLegacyFirewall(ctx context.Context, dryRun bool) (bool, error) {
	if b.LegacyFirewallHandoffPath == "" {
		return false, nil
	}
	data, err := os.ReadFile(b.LegacyFirewallHandoffPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read legacy firewall handoff: %w", err)
	}
	var handoff struct {
		Version      string `json:"version"`
		SourceCommit string `json:"sourceCommit"`
	}
	if err := json.Unmarshal(data, &handoff); err != nil || handoff.Version != legacyFirewallHandoffVersion || handoff.SourceCommit != legacyFirewallHandoffCommit {
		return false, fmt.Errorf("invalid legacy firewall handoff record")
	}
	if dryRun {
		return true, nil
	}
	nft := stringutil.FirstNonEmpty(b.Command, "nft")
	out, err := b.run(ctx, nft, "-j", "list", "tables")
	if err != nil {
		return false, fmt.Errorf("observe legacy firewall handoff: %w: %s", err, strings.TrimSpace(string(out)))
	}
	var inventory struct {
		Tables []struct {
			Table *struct {
				Family  string `json:"family"`
				Name    string `json:"name"`
				Handle  uint64 `json:"handle"`
				Comment string `json:"comment"`
			} `json:"table"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &inventory); err != nil || inventory.Tables == nil {
		return false, fmt.Errorf("decode legacy firewall handoff inventory")
	}
	for _, entry := range inventory.Tables {
		table := entry.Table
		if table == nil || table.Family != "inet" || table.Name != "routerd_filter" {
			continue
		}
		if table.Comment == render.NftablesRouterdOwnerMarker {
			return true, nil
		}
		if table.Handle == 0 {
			return false, fmt.Errorf("legacy firewall handoff lacks native table handle")
		}
		if out, err := b.run(ctx, nft, "delete", "table", "inet", "handle", strconv.FormatUint(table.Handle, 10)); err != nil {
			return false, fmt.Errorf("handoff delete legacy inet routerd_filter handle %d: %w: %s", table.Handle, err, strings.TrimSpace(string(out)))
		}
		return true, nil
	}
	return true, nil
}

func (b Nftables) Reload(ctx context.Context, ruleset Ruleset) error {
	if err := validateRuleset(ruleset); err != nil {
		return err
	}
	if err := nftstate.Invalidate(ruleset.Path); err != nil {
		return err
	}
	nft := stringutil.FirstNonEmpty(b.Command, "nft")
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

func (b PF) Render(router *api.Router, path string, intents []dynamicconfig.LocalCaptureIntent) (Ruleset, error) {
	holes := render.InternalFirewallHoles(router, platform.OSFreeBSD)
	data, err := render.PFRuntimeFirewall(router, holes, intents)
	if err != nil {
		return Ruleset{}, err
	}
	defaults, _ := platform.Current()
	path = stringutil.FirstNonEmpty(path, filepath.Join(defaults.RuntimeDir, "firewall.pf"))
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
	pfctl := stringutil.FirstNonEmpty(b.Command, "pfctl")
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
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	return !bytes.Equal(previous, data), nil
}

func writeRuleset(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func validateRuleset(ruleset Ruleset) error {
	if strings.TrimSpace(ruleset.Path) == "" {
		return fmt.Errorf("%s ruleset path is empty", stringutil.FirstNonEmpty(ruleset.Backend, "firewall"))
	}
	if strings.Contains(ruleset.Path, "\x00") {
		return fmt.Errorf("%s ruleset path contains NUL byte", stringutil.FirstNonEmpty(ruleset.Backend, "firewall"))
	}
	return nil
}
