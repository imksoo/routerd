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
	"slices"
	"sort"
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
	Command string
	Force   bool
}

func (b Nftables) Name() string { return "nftables" }

func (b Nftables) Render(router *api.Router, path string, _ []dynamicconfig.LocalCaptureIntent) (Ruleset, error) {
	holes := render.InternalFirewallHoles(router)
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
	changed, err := b.Diff(ruleset)
	if err != nil {
		return false, err
	}
	if changed && dryRun {
		if err := writeRuleset(ruleset.Path, ruleset.Data); err != nil {
			return false, err
		}
	}
	if dryRun {
		return changed, nil
	}
	removingFilter := bytes.Equal(ruleset.Data, []byte("destroy table inet routerd_filter\n"))
	if removingFilter && !changed {
		nft := stringutil.FirstNonEmpty(b.Command, "nft")
		out, err := b.run(ctx, nft, "-j", "list", "tables")
		if err != nil {
			return false, fmt.Errorf("%s -j list tables: %w: %s", nft, err, strings.TrimSpace(string(out)))
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
		present := false
		for _, object := range listing.Objects {
			if object.Table != nil && object.Table.Family == "inet" && object.Table.Name == "routerd_filter" {
				present = true
				break
			}
		}
		if !present {
			return false, nil
		}
	}
	if !removingFilter && !b.Force && !changed && nftstate.RecentlyVerified(ruleset.Path, time.Now().UTC()) {
		return false, nil
	}
	if !b.Force && !changed && b.rulesetTablesPresent(ctx, ruleset) {
		_ = nftstate.MarkVerified(ruleset.Path, time.Now().UTC())
		return false, nil
	}
	applyRuleset := ruleset
	previous, err := os.ReadFile(ruleset.Path)
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	data, err := b.preserveDynamicSets(ctx, ruleset.Data, previous)
	if err != nil {
		return false, err
	}
	if changed || !bytes.Equal(data, ruleset.Data) {
		if err := os.MkdirAll(filepath.Dir(ruleset.Path), 0755); err != nil {
			return false, err
		}
		file, err := os.CreateTemp(filepath.Dir(ruleset.Path), ".firewall-*.nft")
		if err != nil {
			return false, err
		}
		applyRuleset.Path = file.Name()
		defer os.Remove(applyRuleset.Path)
		if err := file.Chmod(0644); err != nil {
			file.Close()
			return false, err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			return false, err
		}
		if err := file.Close(); err != nil {
			return false, err
		}
	}
	if err := b.Reload(ctx, applyRuleset); err != nil {
		return false, err
	}
	if changed {
		if !bytes.Equal(data, ruleset.Data) {
			if err := os.WriteFile(applyRuleset.Path, ruleset.Data, 0644); err != nil {
				return false, err
			}
		}
		if err := os.Rename(applyRuleset.Path, ruleset.Path); err != nil {
			return false, err
		}
	}
	_ = nftstate.MarkVerified(ruleset.Path, time.Now().UTC())
	return true, nil
}

func (b Nftables) preserveDynamicSets(ctx context.Context, data, previous []byte) ([]byte, error) {
	if !bytes.Contains(data, []byte("flush table inet routerd_filter\n")) {
		return data, nil
	}
	lines := strings.Split(string(data), "\n")
	wanted := make(map[string]bool)
	wantedChains := make(map[string]bool)
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "set" && fields[2] == "{" {
			wanted[fields[1]] = true
		}
		if len(fields) >= 3 && fields[0] == "chain" && fields[2] == "{" {
			wantedChains[fields[1]] = true
		}
		for i, field := range fields {
			if field == "meter" && i+1 < len(fields) {
				wanted[fields[i+1]] = true
			}
		}
	}
	nft := stringutil.FirstNonEmpty(b.Command, "nft")
	out, err := b.run(ctx, nft, "-j", "list", "ruleset")
	if err != nil {
		return nil, fmt.Errorf("%s -j list ruleset: %w: %s", nft, err, strings.TrimSpace(string(out)))
	}
	var snapshot struct {
		Objects []struct {
			Chain *struct {
				Family string `json:"family"`
				Table  string `json:"table"`
				Name   string `json:"name"`
				Handle uint64 `json:"handle"`
			} `json:"chain"`
			Set *struct {
				Family string          `json:"family"`
				Table  string          `json:"table"`
				Name   string          `json:"name"`
				Handle uint64          `json:"handle"`
				Type   json.RawMessage `json:"type"`
				Flags  []string        `json:"flags"`
			} `json:"set"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &snapshot); err != nil {
		return nil, fmt.Errorf("decode nft ruleset: %w", err)
	}
	existing := make(map[string]string)
	var obsolete []uint64
	var obsoleteChains []uint64
	for _, object := range snapshot.Objects {
		chain := object.Chain
		if chain != nil && chain.Family == "inet" && chain.Table == "routerd_filter" && !wantedChains[chain.Name] {
			if chain.Handle == 0 {
				return nil, fmt.Errorf("nft chain %s has no handle", chain.Name)
			}
			obsoleteChains = append(obsoleteChains, chain.Handle)
		}
		set := object.Set
		if set == nil || set.Family != "inet" || set.Table != "routerd_filter" {
			continue
		}
		if !wanted[set.Name] {
			if set.Handle == 0 {
				return nil, fmt.Errorf("nft set %s has no handle", set.Name)
			}
			obsolete = append(obsolete, set.Handle)
			continue
		}
		if !strings.HasPrefix(set.Name, "flow_pinhole_") && !strings.HasPrefix(set.Name, "ip_address_set_") {
			continue
		}
		var types []string
		if err := json.Unmarshal(set.Type, &types); err != nil {
			var single string
			if err := json.Unmarshal(set.Type, &single); err != nil {
				return nil, fmt.Errorf("decode nft set %s type: %w", set.Name, err)
			}
			types = []string{single}
		}
		flow := strings.HasPrefix(set.Name, "flow_pinhole_")
		if (!flow && len(set.Flags) == 0) || (flow && len(set.Flags) == 1 && set.Flags[0] == "timeout") {
			existing[set.Name] = strings.Join(types, " . ")
		}
	}
	preserve := make(map[string]bool)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "set flow_pinhole_") && !strings.HasPrefix(trimmed, "set ip_address_set_") {
			continue
		}
		name, rest, ok := strings.Cut(strings.TrimPrefix(trimmed, "set "), " { type ")
		if !ok {
			continue
		}
		typ, _, ok := strings.Cut(rest, ";")
		if ok && existing[name] == typ &&
			(!strings.HasPrefix(name, "flow_pinhole_") || samePinholeRules(previous, data, name)) {
			preserve[name] = true
		}
	}
	if len(preserve) == 0 && len(obsolete) == 0 && len(obsoleteChains) == 0 {
		return data, nil
	}
	sort.Slice(obsolete, func(i, j int) bool { return obsolete[i] < obsolete[j] })
	sort.Slice(obsoleteChains, func(i, j int) bool { return obsoleteChains[i] < obsoleteChains[j] })
	var kept []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "flush table inet routerd_filter" {
			kept = append(kept, line)
			for _, handle := range obsoleteChains {
				kept = append(kept, "destroy chain inet routerd_filter handle "+strconv.FormatUint(handle, 10))
			}
			for _, handle := range obsolete {
				kept = append(kept, "destroy set inet routerd_filter handle "+strconv.FormatUint(handle, 10))
			}
			continue
		}
		if strings.HasPrefix(trimmed, "destroy set inet routerd_filter ") && preserve[strings.TrimPrefix(trimmed, "destroy set inet routerd_filter ")] {
			continue
		}
		if strings.HasPrefix(trimmed, "set ") {
			name, _, _ := strings.Cut(strings.TrimPrefix(trimmed, "set "), " ")
			if preserve[name] {
				continue
			}
		}
		kept = append(kept, line)
	}
	return []byte(strings.Join(kept, "\n")), nil
}

func samePinholeRules(previous, current []byte, name string) bool {
	declaration := "set " + name + " {"
	if !bytes.Contains(previous, []byte(declaration)) {
		return true
	}
	rules := func(data []byte) []string {
		var result []string
		for _, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "@"+name+" ") {
				result = append(result, strings.TrimSpace(line))
			}
		}
		return result
	}
	return slices.Equal(rules(previous), rules(current))
}

func (b Nftables) rulesetTablesPresent(ctx context.Context, ruleset Ruleset) bool {
	tables := nftRulesetTables(ruleset.Data)
	if len(tables) == 0 {
		return false
	}
	nft := stringutil.FirstNonEmpty(b.Command, "nft")
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
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

type PF struct {
	Command string
}

func (b PF) Name() string { return "pf" }

func (b PF) Render(router *api.Router, path string, intents []dynamicconfig.LocalCaptureIntent) (Ruleset, error) {
	holes := render.InternalFirewallHoles(router)
	data, err := render.PFWithLocalCaptureIntents(router, holes, intents)
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
		return fmt.Errorf("%s ruleset path is empty", stringutil.FirstNonEmpty(ruleset.Backend, "firewall"))
	}
	if strings.Contains(ruleset.Path, "\x00") {
		return fmt.Errorf("%s ruleset path contains NUL byte", stringutil.FirstNonEmpty(ruleset.Backend, "firewall"))
	}
	return nil
}
