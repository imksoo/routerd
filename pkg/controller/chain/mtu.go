// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
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
	L2OwnerPath       string
	ForceFragmentPath string
	RunNFT            func(context.Context, ...string) ([]byte, error)
	RandomToken       func() (string, error)
	L2Failpoint       func(string) error
}

type l2MSSGeneration struct {
	Table  string `json:"table"`
	Chain  string `json:"chain"`
	Token  string `json:"token"`
	Digest string `json:"digest"`
	Handle uint64 `json:"handle,omitempty"`
	State  string `json:"state"`
}

type l2MSSOwnerManifest struct {
	Version     int               `json:"version"`
	BootID      string            `json:"bootId"`
	NetNS       uint64            `json:"netns"`
	ActiveToken string            `json:"activeToken,omitempty"`
	Generations []l2MSSGeneration `json:"generations,omitempty"`
}

func currentL2OwnerIdentity() (string, uint64, error) {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", 0, err
	}
	info, err := os.Stat("/proc/self/ns/net")
	if err != nil {
		return "", 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, fmt.Errorf("netns inode unavailable")
	}
	return strings.TrimSpace(string(b)), st.Ino, nil
}

type l2MSSApplyResult struct {
	Changed     bool
	Drifted     bool
	Table       string
	Quarantined int
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
		return c.savePathMTUError("MSSRenderFailed", err)
	}
	l2MSSData, err := render.NftablesL2TCPMSSClamp(c.Router)
	if err != nil {
		return c.savePathMTUError("L2MSSRenderFailed", err)
	}
	forceFragmentData, err := render.NftablesIPv4ForceFragment(c.Router)
	if err != nil {
		return c.savePathMTUError("ForceFragmentRenderFailed", err)
	}
	path := firstNonEmpty(c.Path, "/run/routerd/mss.nft")
	l2Path := firstNonEmpty(c.L2Path, "/run/routerd/l2-mss.nft")
	forceFragmentPath := firstNonEmpty(c.ForceFragmentPath, "/run/routerd/forcefrag.nft")
	nft := firstNonEmpty(c.NftCommand, "nft")
	mssChanged, err := c.applyTable(ctx, nft, path, "inet", "routerd_mss", mssData)
	if err != nil {
		return c.savePathMTUError("MSSApplyFailed", err)
	}
	l2Result, err := c.applyL2MSSTable(ctx, nft, l2Path, firstNonEmpty(c.L2OwnerPath, l2Path+".owner"), len(bytes.TrimSpace(l2MSSData)) > 0)
	if err != nil {
		return c.savePathMTUError("L2MSSApplyFailed", err)
	}
	l2MSSChanged := l2Result.Changed
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
			"l2MSSNftTable":           l2Result.Table,
			"l2MSSNftPath":            l2Path,
			"l2MSSActive":             len(bytes.TrimSpace(l2MSSData)) > 0,
			"l2MSSDrifted":            l2Result.Drifted,
			"l2MSSQuarantined":        l2Result.Quarantined,
			"l2MSSGenerationTable":    l2Result.Table,
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
		event.Attributes = map[string]string{"mssPath": path, "mssTable": "routerd_mss", "l2MSSPath": l2Path, "l2MSSTable": l2Result.Table, "forceFragmentPath": forceFragmentPath, "forceFragmentTable": "routerd_forcefrag"}
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
			"drifted":   strings.Contains(strings.ToLower(cause.Error()), "drift") || strings.Contains(strings.ToLower(cause.Error()), "foreign replacement"),
			"updatedAt": time.Now().UTC().Format(time.RFC3339Nano),
		}); err != nil {
			return fmt.Errorf("%v (saving PathMTU error status: %w)", cause, err)
		}
	}
	return cause
}

func (c PathMTUController) runNFT(ctx context.Context, nft string, args ...string) ([]byte, error) {
	if c.RunNFT != nil {
		return c.RunNFT(ctx, args...)
	}
	return exec.CommandContext(ctx, nft, args...).CombinedOutput()
}

func (c PathMTUController) randomToken() (string, error) {
	if c.RandomToken != nil {
		return c.RandomToken()
	}
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func readL2MSSOwner(path string) (*l2MSSOwnerManifest, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m l2MSSOwnerManifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m.Version != 3 || m.BootID == "" || m.NetNS == 0 {
		return nil, fmt.Errorf("invalid L2 MSS owner manifest")
	}
	for _, g := range m.Generations {
		if g.Table == "" || g.Chain == "" || g.Token == "" || g.Digest == "" || (g.State != "staged" && g.State != "activating" && g.State != "active" && g.State != "retired" && g.State != "foreign") {
			return nil, fmt.Errorf("invalid L2 MSS generation journal entry")
		}
	}
	return &m, nil
}

func writeL2MSSOwner(path string, m l2MSSOwnerManifest) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".l2-mss-owner-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, path); err != nil {
		return err
	}
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func l2OwnerProof(g l2MSSGeneration) string {
	mac := hmac.New(sha256.New, []byte(g.Token))
	_, _ = mac.Write([]byte(g.Table + "\x00" + g.Chain + "\x00" + g.Digest))
	return hex.EncodeToString(mac.Sum(nil))
}

func jsonMap(v any) map[string]any { m, _ := v.(map[string]any); return m }

func canonicalL2Rule(exprs []any) (render.L2MSSCanonicalRule, error) {
	var r render.L2MSSCanonicalRule
	r.Family = 4
	flags, threshold, mangle := false, false, false
	meaningful := 0
	for _, raw := range exprs {
		e := jsonMap(raw)
		if _, ok := e["counter"]; ok {
			continue
		}
		meaningful++
		if rawMatch, ok := e["match"]; ok {
			m := jsonMap(rawMatch)
			op, _ := m["op"].(string)
			left := jsonMap(m["left"])
			if meta := jsonMap(left["meta"]); meta != nil {
				key, _ := meta["key"].(string)
				right, _ := m["right"].(string)
				if op != "==" || right == "" {
					return r, fmt.Errorf("invalid interface match")
				}
				if key == "iifname" {
					r.IIF = right
				} else if key == "oifname" {
					r.OIF = right
				} else {
					return r, fmt.Errorf("unexpected meta key %s", key)
				}
				continue
			}
			if payload := jsonMap(left["payload"]); payload != nil {
				if op != "==" || payload["protocol"] != "ether" || payload["field"] != "type" || m["right"] != "ip6" {
					return r, fmt.Errorf("unexpected payload match")
				}
				r.Family = 6
				continue
			}
			if _, ok := left["&"]; ok {
				b, _ := json.Marshal(left)
				if op != "==" || m["right"] != "syn" || !bytes.Contains(b, []byte(`"protocol":"tcp"`)) || !bytes.Contains(b, []byte(`"field":"flags"`)) {
					return r, fmt.Errorf("unexpected TCP flags match")
				}
				flags = true
				continue
			}
			if opt := jsonMap(left["tcp option"]); opt != nil {
				if op != ">" || opt["name"] != "maxseg" || opt["field"] != "size" {
					return r, fmt.Errorf("unexpected MSS threshold")
				}
				v, ok := m["right"].(float64)
				if !ok {
					return r, fmt.Errorf("invalid MSS threshold")
				}
				r.MSS = int(v)
				threshold = true
				continue
			}
			return r, fmt.Errorf("unexpected match expression")
		}
		if rawMangle, ok := e["mangle"]; ok {
			m := jsonMap(rawMangle)
			key := jsonMap(m["key"])
			opt := jsonMap(key["tcp option"])
			v, ok := m["value"].(float64)
			if !ok || opt["name"] != "maxseg" || opt["field"] != "size" {
				return r, fmt.Errorf("unexpected mangle")
			}
			if r.MSS != 0 && r.MSS != int(v) {
				return r, fmt.Errorf("MSS threshold/mangle mismatch")
			}
			r.MSS = int(v)
			mangle = true
			continue
		}
		return r, fmt.Errorf("unexpected rule expression")
	}
	want := 5
	if r.Family == 6 {
		want = 6
	}
	if meaningful != want || r.IIF == "" || r.OIF == "" || !flags || !threshold || !mangle || r.MSS == 0 {
		return r, fmt.Errorf("incomplete L2 MSS rule")
	}
	return r, nil
}

func nftListMissing(out []byte, err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(string(out) + " " + err.Error())
	return strings.Contains(s, "no such file or directory") || strings.Contains(s, "does not exist")
}

func nftEchoTableHandle(data []byte, table string) (uint64, error) {
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return 0, err
	}
	var walk func(any) uint64
	walk = func(v any) uint64 {
		switch x := v.(type) {
		case map[string]any:
			if name, _ := x["name"].(string); name == table {
				if h, ok := x["handle"].(float64); ok && h > 0 {
					return uint64(h)
				}
			}
			for _, child := range x {
				if h := walk(child); h != 0 {
					return h
				}
			}
		case []any:
			for _, child := range x {
				if h := walk(child); h != 0 {
					return h
				}
			}
		}
		return 0
	}
	if h := walk(root); h != 0 {
		return h, nil
	}
	return 0, fmt.Errorf("nft create echo missing table handle for %s", table)
}

func l2HookChain(g *l2MSSGeneration) string {
	return "forward_" + strings.TrimPrefix(g.Chain, "rules_")
}

func canonicalL2Jump(exprs []any, target string) bool {
	meaningful := 0
	for _, raw := range exprs {
		e := jsonMap(raw)
		if _, ok := e["counter"]; ok {
			continue
		}
		meaningful++
		jump := jsonMap(e["jump"])
		if jump == nil || jump["target"] != target {
			return false
		}
	}
	return meaningful == 1
}

func (c PathMTUController) observeL2Owner(ctx context.Context, nft string, g *l2MSSGeneration) (bool, bool, bool, bool, uint64, error) {
	if g == nil {
		return false, false, false, false, 0, nil
	}
	out, err := c.runNFT(ctx, nft, "--json", "-a", "list", "table", "bridge", g.Table)
	if err != nil {
		if nftListMissing(out, err) {
			return false, false, false, true, 0, nil
		}
		return false, false, false, false, 0, fmt.Errorf("list L2 MSS table %s: %w: %s", g.Table, err, strings.TrimSpace(string(out)))
	}
	var doc struct {
		Nftables []map[string]json.RawMessage `json:"nftables"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return false, false, false, false, 0, err
	}
	var handle uint64
	var rules []render.L2MSSCanonicalRule
	tableCount, chainCount, hookRuleCount := 0, 0, 0
	rulesProof, rulesChainStructure, hookChainOK, contentInvalid := false, false, false, false
	hookChain := l2HookChain(g)
	proof := render.NftablesL2MSSPrivateProofMarker + l2OwnerProof(*g)
	for _, item := range doc.Nftables {
		if item["metainfo"] != nil {
			continue
		}
		if raw := item["table"]; raw != nil {
			tableCount++
			var v struct {
				Family, Name string
				Handle       uint64
			}
			if json.Unmarshal(raw, &v) == nil && v.Family == "bridge" && v.Name == g.Table {
				handle = v.Handle
			}
			continue
		}
		if raw := item["chain"]; raw != nil {
			chainCount++
			var v struct {
				Family, Table, Name, Comment, Type, Hook, Policy string
				Prio                                             int `json:"prio"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Family != "bridge" || v.Table != g.Table {
				contentInvalid = true
				continue
			}
			switch v.Name {
			case g.Chain:
				rulesProof = v.Comment == proof
				rulesChainStructure = v.Type == "" && v.Hook == ""
			case hookChain:
				hookChainOK = v.Comment == proof && v.Type == "filter" && v.Hook == "forward" && v.Prio == -150 && v.Policy == "accept"
			default:
				contentInvalid = true
			}
			continue
		}
		if raw := item["rule"]; raw != nil {
			var v struct {
				Family, Table, Chain string
				Expr                 []any `json:"expr"`
			}
			if err := json.Unmarshal(raw, &v); err != nil {
				return false, false, false, false, 0, err
			}
			if v.Family != "bridge" || v.Table != g.Table {
				contentInvalid = true
				continue
			}
			switch v.Chain {
			case g.Chain:
				r, err := canonicalL2Rule(v.Expr)
				if err != nil {
					contentInvalid = true
					continue
				}
				rules = append(rules, r)
			case hookChain:
				hookRuleCount++
				if !canonicalL2Jump(v.Expr, g.Chain) {
					contentInvalid = true
				}
			default:
				contentInvalid = true
			}
			continue
		}
		if item["rule"] == nil {
			contentInvalid = true
		}
	}
	activated := chainCount == 2 && hookChainOK && hookRuleCount == 1
	// The manifest's nonzero table handle, captured in this boot/netns, is the
	// durable kernel-object identity. Proofs and all chain/rule structure are
	// content validity only: losing them is owned drift, not foreign ownership.
	identity := tableCount == 1 && g.Handle != 0 && handle == g.Handle
	structureValid := rulesProof && rulesChainStructure && !contentInvalid && ((chainCount == 1 && hookRuleCount == 0) || activated)
	if handle == 0 {
		return false, false, false, false, 0, fmt.Errorf("nft table handle missing")
	}
	sort.Slice(rules, func(i, j int) bool {
		a, b := rules[i], rules[j]
		if a.IIF != b.IIF {
			return a.IIF < b.IIF
		}
		if a.OIF != b.OIF {
			return a.OIF < b.OIF
		}
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		return a.MSS < b.MSS
	})
	b, err := json.Marshal(rules)
	if err != nil {
		return false, false, false, false, 0, err
	}
	sum := sha256.Sum256(b)
	return identity, structureValid && fmt.Sprintf("%x", sum) == g.Digest, activated, false, handle, nil
}

func (c PathMTUController) deleteOwnedL2Table(ctx context.Context, nft string, g *l2MSSGeneration) (bool, error) {
	owned, _, _, missing, handle, err := c.observeL2Owner(ctx, nft, g)
	if err != nil {
		return false, err
	}
	if missing {
		return true, nil
	}
	if !owned {
		return false, fmt.Errorf("refusing to remove foreign replacement for L2 MSS generation %s", g.Table)
	}
	if g.Handle == 0 {
		g.Handle = handle
	}
	if handle != g.Handle {
		return false, fmt.Errorf("L2 MSS table handle drift for %s: journal=%d live=%d", g.Table, g.Handle, handle)
	}
	if out, err := c.runNFT(ctx, nft, "delete", "table", "bridge", "handle", strconv.FormatUint(g.Handle, 10)); err != nil {
		return false, fmt.Errorf("delete owned L2 MSS table handle %d: %w: %s", g.Handle, err, strings.TrimSpace(string(out)))
	}
	return true, nil
}

func writeL2Journal(path string, m *l2MSSOwnerManifest) error { return writeL2MSSOwner(path, *m) }

func acquireL2OwnerLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func (c PathMTUController) recoverL2Journal(ctx context.Context, nft, ownerPath, desiredDigest string, m *l2MSSOwnerManifest) (bool, error) {
	drifted := false
	for i := range m.Generations {
		g := &m.Generations[i]
		owned, rulesMatch, activated, missing, handle, err := c.observeL2Owner(ctx, nft, g)
		if err != nil {
			return drifted, err
		}
		if missing {
			g.State = "retired"
			continue
		}
		if !owned || g.Handle == 0 || g.Handle != handle {
			drifted = true
			g.State = "foreign"
			if m.ActiveToken == g.Token {
				m.ActiveToken = ""
			}
			continue
		}
		if !rulesMatch {
			drifted = true
			g.State = "retired"
			if m.ActiveToken == g.Token {
				m.ActiveToken = ""
			}
			continue
		}
		if g.State == "staged" {
			g.State = "retired"
			continue
		}
		if g.State == "active" && !activated {
			drifted = true
			g.State = "retired"
			if m.ActiveToken == g.Token {
				m.ActiveToken = ""
			}
			continue
		}
		if g.State == "activating" {
			if activated && desiredDigest != "" && g.Digest == desiredDigest {
				prior := m.ActiveToken
				for j := range m.Generations {
					if m.Generations[j].Token == prior && prior != g.Token && m.Generations[j].State == "active" {
						m.Generations[j].State = "retired"
					}
				}
				g.State = "active"
				m.ActiveToken = g.Token
			} else {
				g.State = "retired"
			}
		}
	}
	if err := writeL2Journal(ownerPath, m); err != nil {
		return drifted, err
	}
	return drifted, nil
}

func (c PathMTUController) cleanupRetiredL2(ctx context.Context, nft, ownerPath string, m *l2MSSOwnerManifest) (bool, bool, error) {
	changed, drifted := false, false
	var cleanupErrs []error
	kept := m.Generations[:0]
	for i := range m.Generations {
		g := m.Generations[i]
		if g.State != "retired" {
			kept = append(kept, g)
			continue
		}
		removed, err := c.deleteOwnedL2Table(ctx, nft, &g)
		if err != nil {
			drifted = true
			kept = append(kept, g)
			cleanupErrs = append(cleanupErrs, fmt.Errorf("generation %s handle %d: %w", g.Table, g.Handle, err))
			continue
		}
		changed = changed || removed
	}
	m.Generations = kept
	if err := writeL2Journal(ownerPath, m); err != nil {
		return changed, drifted, err
	}
	if len(cleanupErrs) != 0 {
		return changed, drifted, fmt.Errorf("L2 MSS drift cleanup failed: %w", errors.Join(cleanupErrs...))
	}
	return changed, drifted, nil
}

func (c PathMTUController) applyL2MSSTable(ctx context.Context, nft, path, ownerPath string, enabled bool) (l2MSSApplyResult, error) {
	var result l2MSSApplyResult
	if !enabled {
		if _, err := os.Stat(ownerPath); os.IsNotExist(err) {
			return result, nil
		}
	}
	if !c.DryRun {
		lock, err := acquireL2OwnerLock(ownerPath)
		if err != nil {
			return result, err
		}
		defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN); _ = lock.Close() }()
	}
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		return result, err
	}
	m, err := readL2MSSOwner(ownerPath)
	if err != nil {
		return result, err
	}
	if m == nil && !enabled {
		return result, nil
	}
	if m != nil && (m.BootID != bootID || m.NetNS != netns) {
		// Never use handles captured in another boot/network namespace. The old
		// journal is intentionally not consulted for cleanup.
		m = nil
		result.Drifted = true
	}
	if m == nil {
		m = &l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns}
	}
	base, err := render.NftablesL2TCPMSSClamp(c.Router)
	if err != nil {
		return result, err
	}
	digest := ""
	if enabled {
		digest, err = render.NftablesL2TCPMSSClampDigest(c.Router)
		if err != nil {
			return result, err
		}
	}
	if c.DryRun {
		if !enabled {
			return result, nil
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return result, err
		}
		changed, err := writeFileIfChanged(path, base, 0600, false)
		return l2MSSApplyResult{Changed: changed, Table: "routerd_l2_mss"}, err
	}
	drifted, err := c.recoverL2Journal(ctx, nft, ownerPath, digest, m)
	if err != nil {
		return result, err
	}
	result.Drifted = result.Drifted || drifted
	for i := range m.Generations {
		if m.Generations[i].State == "foreign" && m.Generations[i].Handle == 0 {
			result.Quarantined++
		}
	}
	if !enabled {
		for i := range m.Generations {
			if m.Generations[i].State == "active" {
				m.Generations[i].State = "retired"
			}
		}
		m.ActiveToken = ""
		if err := writeL2Journal(ownerPath, m); err != nil {
			return result, err
		}
		changed, d, err := c.cleanupRetiredL2(ctx, nft, ownerPath, m)
		result.Changed, result.Drifted = changed, result.Drifted || d
		return result, err
	}
	for i := range m.Generations {
		g := &m.Generations[i]
		if g.State == "active" && g.Token == m.ActiveToken && g.Digest == digest {
			result.Table = g.Table
			changed, d, err := c.cleanupRetiredL2(ctx, nft, ownerPath, m)
			result.Changed, result.Drifted = changed, result.Drifted || d
			return result, err
		}
	}
	token, err := c.randomToken()
	if err != nil {
		return result, err
	}
	short := token
	if len(short) > 12 {
		short = short[:12]
	}
	stage := l2MSSGeneration{Table: "routerd_l2_" + short, Chain: "rules_" + short, Token: token, Digest: digest, State: "staged"}
	m.Generations = append(m.Generations, stage)
	if err := writeL2Journal(ownerPath, m); err != nil {
		return result, err
	}
	stageData, err := render.NftablesL2TCPMSSClampStaged(c.Router, stage.Table, stage.Chain, l2OwnerProof(stage))
	if err != nil {
		return result, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return result, err
	}
	if _, err := writeFileIfChanged(path, stageData, 0600, false); err != nil {
		return result, err
	}
	if out, err := c.runNFT(ctx, nft, "-c", "-f", path); err != nil {
		return result, fmt.Errorf("%s stage -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	echo, err := c.runNFT(ctx, nft, "--echo", "--handle", "--json", "-f", path)
	if err != nil {
		return result, fmt.Errorf("%s stage create -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(echo)))
	}
	if c.L2Failpoint != nil {
		if err := c.L2Failpoint("stage-created-before-handle-journal"); err != nil {
			return result, err
		}
	}
	stageHandle, err := nftEchoTableHandle(echo, stage.Table)
	if err != nil {
		return result, err
	}
	for i := range m.Generations {
		if m.Generations[i].Token == stage.Token {
			m.Generations[i].Handle = stageHandle
		}
	}
	if err := writeL2Journal(ownerPath, m); err != nil {
		return result, err
	}
	stage.Handle = stageHandle
	owned, rulesMatch, activated, missing, handle, err := c.observeL2Owner(ctx, nft, &stage)
	if err != nil || missing || !owned || !rulesMatch || activated || handle != stageHandle {
		return result, fmt.Errorf("staged L2 MSS identity verification failed for %s", stage.Table)
	}

	// The exact table handle is already durable. Mark activation pending before
	// adding the hook chain, so either side of the kernel transaction is safely
	// recoverable without ever adopting a handle-zero hooked object.
	for i := range m.Generations {
		if m.Generations[i].Token == stage.Token {
			m.Generations[i].State = "activating"
		}
	}
	stage.State = "activating"
	if err := writeL2Journal(ownerPath, m); err != nil {
		return result, err
	}
	activation := render.NftablesL2TCPMSSClampActivate(stage.Table, stage.Chain, l2HookChain(&stage), l2OwnerProof(stage))
	if _, err := writeFileIfChanged(path, activation, 0600, false); err != nil {
		return result, err
	}
	if out, err := c.runNFT(ctx, nft, "-c", "-f", path); err != nil {
		return result, fmt.Errorf("%s activate -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	out, err := c.runNFT(ctx, nft, "-f", path)
	if err != nil {
		return result, fmt.Errorf("%s activate -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if c.L2Failpoint != nil {
		if err := c.L2Failpoint("active-hook-created-before-promotion"); err != nil {
			return result, err
		}
	}
	owned, rulesMatch, activated, missing, handle, err = c.observeL2Owner(ctx, nft, &stage)
	if err != nil || missing || !owned || !rulesMatch || !activated || handle != stageHandle {
		return result, fmt.Errorf("active L2 MSS identity verification failed for %s", stage.Table)
	}
	prior := m.ActiveToken
	for i := range m.Generations {
		g := &m.Generations[i]
		switch {
		case g.Token == stage.Token:
			g.State = "active"
		case g.Token == prior && prior != stage.Token && g.State == "active":
			g.State = "retired"
		}
	}
	m.ActiveToken = stage.Token
	if err := writeL2Journal(ownerPath, m); err != nil {
		return result, err
	}
	_, d, err := c.cleanupRetiredL2(ctx, nft, ownerPath, m)
	result.Drifted = result.Drifted || d
	if err != nil {
		return result, err
	}
	result.Changed, result.Table = true, stage.Table
	return result, nil

}

func (c PathMTUController) applyTable(ctx context.Context, nft, path, family, table string, data []byte) (bool, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		if !c.DryRun {
			if nftstate.RecentlyVerified(path, time.Now().UTC()) {
				return false, nil
			}
			_, _ = c.runNFT(ctx, nft, "delete", "table", family, table)
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
	if !changed && nftstate.RecentlyVerified(path, time.Now().UTC()) {
		return false, nil
	}
	current, listErr := c.runNFT(ctx, nft, "list", "table", family, table)
	missing := listErr != nil
	_ = current
	if !changed && !missing {
		_ = nftstate.MarkVerified(path, time.Now().UTC())
		return false, nil
	}
	if out, err := c.runNFT(ctx, nft, "-c", "-f", path); err != nil {
		return changed, fmt.Errorf("%s -c -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	if out, err := c.runNFT(ctx, nft, "-f", path); err != nil {
		return changed, fmt.Errorf("%s -f %s: %w: %s", nft, path, err, strings.TrimSpace(string(out)))
	}
	_ = nftstate.MarkVerified(path, time.Now().UTC())
	return changed || missing, nil
}
