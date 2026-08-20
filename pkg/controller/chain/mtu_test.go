// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/render"
)

func TestPathMTUControllerRendersMSSClamp(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}}},
	}}}
	store := mapStore{}
	controller := PathMTUController{Router: router, OS: platform.OSLinux, Store: store, DryRun: true, Path: dir + "/mss.nft"}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`table inet routerd_mss`, `iifname "ens19" oifname "ds-lite-a" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1414 tcp option maxseg size set 1414`} {
		if !strings.Contains(got, want) {
			t.Fatalf("mss rules missing %q:\n%s", want, got)
		}
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu")
	if status["phase"] != "Applied" {
		t.Fatalf("status = %#v", status)
	}
}

func TestPathMTUControllerRendersBridgeFamilyMSSClamp(t *testing.T) {
	router := &api.Router{TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"}, Metadata: api.ObjectMeta{Name: "l2"}, Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "underlay0", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 42, LocalAddress: "198.18.0.1", UnderlayInterface: "underlay", MTU: 1280, Bridge: "br", TCPMSSClamp: true}},
	}}}
	dir := t.TempDir()
	store := mapStore{}
	path := filepath.Join(dir, "l2-mss.nft")
	controller := PathMTUController{Router: router, OS: platform.OSLinux, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft"), L2Path: path, ForceFragmentPath: filepath.Join(dir, "forcefrag.nft")}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "table bridge routerd_l2_mss") {
		t.Fatalf("missing bridge family table:\n%s", data)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu")
	if status["l2MSSActive"] != true || status["l2MSSNftTable"] != "routerd_l2_mss" {
		t.Fatalf("status=%#v", status)
	}
}

func TestPathMTUControllerKeepsImplicitL2ArtifactBesideConfiguredPath(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "underlay0", MTU: 1500, Managed: true}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 42, LocalAddress: "198.18.0.1", UnderlayInterface: "underlay", MTU: 1280, Bridge: "br", TCPMSSClamp: true}},
	}}}
	path := filepath.Join(dir, "artifacts", "mss.nft")
	controller := PathMTUController{Router: router, OS: platform.OSLinux, Store: mapStore{}, DryRun: true, Path: path, ForceFragmentPath: filepath.Join(dir, "artifacts", "forcefrag.nft")}
	if err := controller.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "artifacts", "l2-mss.nft")); err != nil {
		t.Fatalf("implicit L2 artifact beside configured path: %v", err)
	}
}

func TestPathMTUControllerSkipsNftablesOnFreeBSD(t *testing.T) {
	dir := t.TempDir()
	store := mapStore{}
	controller := PathMTUController{
		Router:            &api.Router{},
		OS:                platform.OSFreeBSD,
		Store:             store,
		NftCommand:        filepath.Join(dir, "must-not-run"),
		Path:              filepath.Join(dir, "mss.nft"),
		ForceFragmentPath: filepath.Join(dir, "forcefrag.nft"),
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(controller.Path); !os.IsNotExist(err) {
		t.Fatalf("nftables artifact must not be written on FreeBSD: %v", err)
	}
	if _, err := os.Stat(controller.ForceFragmentPath); !os.IsNotExist(err) {
		t.Fatalf("force-fragment nftables artifact must not be written on FreeBSD: %v", err)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu")
	if status["phase"] != "Skipped" || status["reason"] != "path MTU policy is enforced by pf" {
		t.Fatalf("status = %#v", status)
	}
}

func TestPathMTUControllerRendersForceFragment(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"}, Metadata: api.ObjectMeta{Name: "wg-hybrid"}, Spec: api.WireGuardInterfaceSpec{MTU: 1420}},
		{TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"}, Metadata: api.ObjectMeta{Name: "onprem-main"}, Spec: api.OverlayPeerSpec{
			Role:     "onprem",
			NodeID:   "onprem-router",
			Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"},
			PathMTU:  api.PathMTUOptions{ForceFragmentIPv4: true},
		}},
	}}}
	store := mapStore{}
	controller := PathMTUController{
		Router: router, OS: platform.OSLinux, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft"), ForceFragmentPath: filepath.Join(dir, "forcefrag.nft"),
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureProtectExisting, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3",
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.ForceFragmentPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`table ip routerd_forcefrag`, `type filter hook prerouting priority mangle; policy accept;`, `iifname "ens3" fib daddr oifname "wg-hybrid" ip length > 1340 ip frag-off 0x4000 ip frag-off set 0`} {
		if !strings.Contains(got, want) {
			t.Fatalf("forcefrag rules missing %q:\n%s", want, got)
		}
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu")
	if status["phase"] != "Applied" || status["forceFragmentIPv4Active"] != true {
		t.Fatalf("status = %#v", status)
	}
}

func TestPathMTUControllerSkipsUnchangedLiveReload(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "nft.log")
	statePath := filepath.Join(dir, "routerd_mss.present")
	nftPath := filepath.Join(dir, "nft")
	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + shellQuote(logPath) + "\n" +
		"if [ \"$1\" = \"list\" ]; then [ -f " + shellQuote(statePath) + " ]; exit $?; fi\n" +
		"if [ \"$1\" = \"-f\" ]; then touch " + shellQuote(statePath) + "; fi\n" +
		"exit 0\n"
	if err := os.WriteFile(nftPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}}},
	}}}
	controller := PathMTUController{Router: router, OS: platform.OSLinux, Store: mapStore{}, NftCommand: nftPath, Path: filepath.Join(dir, "mss.nft"), ForceFragmentPath: filepath.Join(dir, "forcefrag.nft")}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logData)
	if count := countLogLine(got, "-f "+controller.Path); count != 1 {
		t.Fatalf("nft -f count = %d, want 1:\n%s", count, got)
	}
	if count := countLogLine(got, "-c -f "+controller.Path); count != 1 {
		t.Fatalf("nft -c -f count = %d, want 1:\n%s", count, got)
	}
}

func TestPathMTUControllerL2OwnerManifestIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, identityErr := currentL2OwnerIdentity()
	if identityErr != nil {
		t.Fatal(identityErr)
	}
	g := l2MSSGeneration{Table: "t", Chain: "c", Token: "secret", Digest: "digest", Handle: 7, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	if err := writeL2MSSOwner(path, m); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode=%o, want 600", info.Mode().Perm())
	}
	got, err := readL2MSSOwner(path)
	if err != nil || got.Version != 3 || len(got.Generations) != 1 || got.Generations[0].Token != "secret" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestPathMTUControllerDeleteRaceNeverDeletesTable(t *testing.T) {
	g := &l2MSSGeneration{Table: "routerd_l2_deadbeef", Chain: "forward_deadbeef", Token: "secret", Digest: "digest", Handle: 7, State: "retired"}
	owned := "table bridge " + g.Table + " { # handle 7\n chain " + g.Chain + " { comment \"" + render.NftablesL2MSSPrivateProofMarker + l2OwnerProof(*g) + "\"; }\n}"
	var calls []string
	foreignPreserved := true
	c := PathMTUController{RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "-a" {
			return []byte(owned), nil
		}
		if args[0] == "delete" {
			foreignPreserved = true // stale handle 7 cannot target replacement handle 8
			return []byte("No such file or directory"), fmt.Errorf("stale handle")
		}
		return nil, nil
	}}
	if _, err := c.deleteOwnedL2Table(t.Context(), "nft", g); err == nil {
		t.Fatal("want fail-closed delete error")
	}
	if !foreignPreserved {
		t.Fatal("foreign replacement was modified")
	}
	for _, call := range calls {
		if strings.Contains(call, g.Table) && strings.HasPrefix(call, "delete") {
			t.Fatalf("name-based delete %q", call)
		}
		if strings.HasPrefix(call, "delete") && call != "delete table bridge handle 7" {
			t.Fatalf("wrong identity delete %q", call)
		}
	}
}

func TestPathMTUControllerCreateRaceFailsWithoutMutatingReplacement(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "lan0", MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Bridge"}, Metadata: api.ObjectMeta{Name: "br"}, Spec: api.BridgeSpec{IfName: "br0", Members: []string{"lan"}, MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "underlay"}, Spec: api.InterfaceSpec{IfName: "eth0", MTU: 1500}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "VXLANTunnel"}, Metadata: api.ObjectMeta{Name: "vx"}, Spec: api.VXLANTunnelSpec{IfName: "vx0", VNI: 42, LocalAddress: "198.18.0.1", UnderlayInterface: "underlay", Bridge: "br", MTU: 1280, TCPMSSClamp: true}},
	}}}
	var calls []string
	c := PathMTUController{Router: router, RandomToken: func() (string, error) { return "0123456789abcdef", nil }, RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		call := strings.Join(args, " ")
		calls = append(calls, call)
		switch {
		case args[0] == "-a":
			return nil, fmt.Errorf("missing")
		case args[0] == "-c":
			return nil, nil
		case args[0] == "--echo":
			return []byte("File exists"), fmt.Errorf("foreign replacement won race")
		}
		return nil, nil
	}}
	dir := t.TempDir()
	_, err := c.applyL2MSSTable(t.Context(), "nft", filepath.Join(dir, "l2.nft"), filepath.Join(dir, "owner.json"), true)
	if err == nil {
		t.Fatal("want create race error")
	}
	for _, call := range calls {
		if strings.Contains(call, "flush") || strings.Contains(call, "delete") {
			t.Fatalf("unsafe call %q", call)
		}
	}
	journal, readErr := readL2MSSOwner(filepath.Join(dir, "owner.json"))
	if readErr != nil || len(journal.Generations) != 1 || journal.Generations[0].State != "staged" || journal.Generations[0].Handle != 0 {
		t.Fatalf("recoverable pending journal=%#v err=%v", journal, readErr)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "l2.nft"))
	if !strings.Contains(string(data), "create table bridge") || strings.Contains(string(data), "add table bridge") {
		t.Fatalf("non-exclusive create:\n%s", data)
	}
}

func TestPathMTUControllerNeverUsesHandleFromStaleBoot(t *testing.T) {
	dir := t.TempDir()
	owner := filepath.Join(dir, "owner.json")
	_, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_stale", Chain: "forward_stale", Token: "secret", Digest: "digest", Handle: 7, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: "different-boot", NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	if err := writeL2MSSOwner(owner, m); err != nil {
		t.Fatal(err)
	}
	var calls []string
	c := PathMTUController{Router: &api.Router{}, RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		return nil, nil
	}}
	result, err := c.applyL2MSSTable(t.Context(), "nft", filepath.Join(dir, "l2.nft"), owner, false)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Drifted {
		t.Fatal("stale identity must be reported as drift")
	}
	for _, call := range calls {
		if strings.Contains(call, "handle 7") {
			t.Fatalf("stale handle used: %s", call)
		}
	}
}

func emptyL2RulesDigest() string {
	sum := sha256.Sum256([]byte("null"))
	return hex.EncodeToString(sum[:])
}

func l2OwnerJSON(g l2MSSGeneration, handle uint64) []byte {
	proof := render.NftablesL2MSSPrivateProofMarker + l2OwnerProof(g)
	hook := l2HookChain(&g)
	return []byte(fmt.Sprintf(`{"nftables":[{"metainfo":{"json_schema_version":1}},{"table":{"family":"bridge","name":%q,"handle":%d}},{"chain":{"family":"bridge","table":%q,"name":%q,"handle":1,"comment":%q}},{"chain":{"family":"bridge","table":%q,"name":%q,"handle":2,"type":"filter","hook":"forward","prio":-150,"policy":"accept","comment":%q}},{"rule":{"family":"bridge","table":%q,"chain":%q,"expr":[{"jump":{"target":%q}}]}}]}`,
		g.Table, handle, g.Table, g.Chain, proof, g.Table, hook, proof, g.Table, hook, g.Chain))
}

func TestPathMTUControllerRecoveryRetiresPriorActiveBeforePromotion(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	digest := emptyL2RulesDigest()
	a := l2MSSGeneration{Table: "routerd_l2_a", Chain: "forward_a", Token: "a-secret", Digest: digest, Handle: 11, State: "active"}
	b := l2MSSGeneration{Table: "routerd_l2_b", Chain: "forward_b", Token: "b-secret", Digest: digest, Handle: 22, State: "activating"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: a.Token, Generations: []l2MSSGeneration{a, b}}
	c := PathMTUController{RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		name := args[len(args)-1]
		if name == a.Table {
			return l2OwnerJSON(a, a.Handle), nil
		}
		if name == b.Table {
			return l2OwnerJSON(b, b.Handle), nil
		}
		return nil, fmt.Errorf("unexpected table %s", name)
	}}
	if _, err := c.recoverL2Journal(t.Context(), "nft", owner, digest, &m); err != nil {
		t.Fatal(err)
	}
	if m.ActiveToken != b.Token || m.Generations[0].State != "retired" || m.Generations[1].State != "active" {
		t.Fatalf("non-atomic promotion: %#v", m)
	}
	got, err := readL2MSSOwner(owner)
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveToken != b.Token || got.Generations[0].State != "retired" || got.Generations[1].State != "active" {
		t.Fatalf("durable journal lost retirement: %#v", got)
	}
}

func TestPathMTUControllerLiveRuleDriftRetiresGeneration(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_drift", Chain: "forward_drift", Token: "secret", Digest: "not-empty-rules", Handle: 31, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	c := PathMTUController{RunNFT: func(context.Context, ...string) ([]byte, error) { return l2OwnerJSON(g, g.Handle), nil }}
	drifted, err := c.recoverL2Journal(t.Context(), "nft", owner, g.Digest, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !drifted || m.ActiveToken != "" || m.Generations[0].State != "retired" {
		t.Fatalf("live rules drift was trusted: %#v", m)
	}
}

func TestPathMTUControllerOwnedUnknownDropIsDeletedByExactHandle(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_owned_drop", Chain: "rules_owned_drop", Token: "secret", Digest: emptyL2RulesDigest(), Handle: 37, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	var doc map[string]any
	if err := json.Unmarshal(l2OwnerJSON(g, g.Handle), &doc); err != nil {
		t.Fatal(err)
	}
	items := doc["nftables"].([]any)
	doc["nftables"] = append(items, map[string]any{"rule": map[string]any{"family": "bridge", "table": g.Table, "chain": g.Chain, "expr": []any{map[string]any{"counter": nil}, map[string]any{"drop": nil}}}})
	driftJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	deleted := false
	c := PathMTUController{RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "delete" {
			if got := strings.Join(args, " "); got != "delete table bridge handle 37" {
				t.Fatalf("unsafe delete %q", got)
			}
			deleted = true
			return nil, nil
		}
		return driftJSON, nil
	}}
	drifted, err := c.recoverL2Journal(t.Context(), "nft", owner, g.Digest, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !drifted || m.Generations[0].State != "retired" {
		t.Fatalf("owned content drift demoted to foreign: %#v", m)
	}
	_, _, err = c.cleanupRetiredL2(t.Context(), "nft", owner, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted || len(m.Generations) != 0 {
		t.Fatalf("owned DROP generation not removed: deleted=%v manifest=%#v", deleted, m)
	}
}

func TestPathMTUControllerSameHandleProofLossRemainsOwned(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_proof_loss", Chain: "rules_proof_loss", Token: "secret", Digest: emptyL2RulesDigest(), Handle: 39, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	var doc map[string]any
	if err := json.Unmarshal(l2OwnerJSON(g, g.Handle), &doc); err != nil {
		t.Fatal(err)
	}
	items := doc["nftables"].([]any)
	rulesChain := items[2].(map[string]any)["chain"].(map[string]any)
	rulesChain["comment"] = "proof removed"
	doc["nftables"] = append(items, map[string]any{"rule": map[string]any{"family": "bridge", "table": g.Table, "chain": g.Chain, "expr": []any{map[string]any{"counter": nil}, map[string]any{"drop": nil}}}})
	driftJSON, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	deleted := false
	c := PathMTUController{RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "delete" {
			if got := strings.Join(args, " "); got != "delete table bridge handle 39" {
				t.Fatalf("unsafe delete %q", got)
			}
			deleted = true
			return nil, nil
		}
		return driftJSON, nil
	}}
	drifted, err := c.recoverL2Journal(t.Context(), "nft", owner, g.Digest, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !drifted || m.Generations[0].State != "retired" {
		t.Fatalf("same-handle proof loss demoted to foreign: %#v", m)
	}
	_, _, err = c.cleanupRetiredL2(t.Context(), "nft", owner, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !deleted || len(m.Generations) != 0 {
		t.Fatalf("proof-loss table not exact-handle deleted: deleted=%v manifest=%#v", deleted, m)
	}
}

func TestPathMTUControllerDeleteFailurePreservesRetiredJournal(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_delete_fail", Chain: "rules_delete_fail", Token: "secret", Digest: emptyL2RulesDigest(), Handle: 47, State: "retired"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, Generations: []l2MSSGeneration{g}}
	c := PathMTUController{RunNFT: func(_ context.Context, args ...string) ([]byte, error) {
		if args[0] == "delete" {
			return []byte("Operation not permitted"), fmt.Errorf("exit status 1")
		}
		return l2OwnerJSON(g, g.Handle), nil
	}}
	_, drifted, err := c.cleanupRetiredL2(t.Context(), "nft", owner, &m)
	if err == nil || !strings.Contains(err.Error(), "drift cleanup failed") || !drifted {
		t.Fatalf("delete failure was reported as success: drifted=%v err=%v", drifted, err)
	}
	if len(m.Generations) != 1 || m.Generations[0].State != "retired" || m.Generations[0].Handle != 47 {
		t.Fatalf("retry identity lost: %#v", m)
	}
	got, readErr := readL2MSSOwner(owner)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(got.Generations) != 1 || got.Generations[0].State != "retired" || got.Generations[0].Handle != 47 {
		t.Fatalf("durable retry journal lost: %#v", got)
	}
}

func TestPathMTUControllerCleanupFailureSavesErrorDriftStatus(t *testing.T) {
	store := mapStore{}
	c := PathMTUController{Store: store}
	cause := fmt.Errorf("L2 MSS drift cleanup failed: injected delete failure")
	if err := c.savePathMTUError("L2MSSApplyFailed", cause); err == nil || err.Error() != cause.Error() {
		t.Fatalf("cause not propagated: %v", err)
	}
	status := store.ObjectStatus(api.RouterAPIVersion, "Router", "derived-path-mtu")
	if status["phase"] != "Error" || status["reason"] != "L2MSSApplyFailed" || status["drifted"] != true {
		t.Fatalf("cleanup failure reported as applied: %#v", status)
	}
}

func TestPathMTUControllerNeverRebindsNonzeroHandle(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_replay", Chain: "forward_replay", Token: "secret", Digest: emptyL2RulesDigest(), Handle: 41, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	c := PathMTUController{RunNFT: func(context.Context, ...string) ([]byte, error) { return l2OwnerJSON(g, 42), nil }}
	drifted, err := c.recoverL2Journal(t.Context(), "nft", owner, g.Digest, &m)
	if err != nil {
		t.Fatal(err)
	}
	if !drifted || m.Generations[0].Handle != 41 || m.Generations[0].State != "foreign" || m.ActiveToken != "" {
		t.Fatalf("foreign replacement was rebound: %#v", m)
	}
}

func TestPathMTUControllerTransientListErrorPreservesJournal(t *testing.T) {
	dir := t.TempDir()
	owner := filepath.Join(dir, "owner.json")
	bootID, netns, err := currentL2OwnerIdentity()
	if err != nil {
		t.Fatal(err)
	}
	g := l2MSSGeneration{Table: "routerd_l2_live", Chain: "forward_live", Token: "secret", Digest: "digest", Handle: 51, State: "active"}
	m := l2MSSOwnerManifest{Version: 3, BootID: bootID, NetNS: netns, ActiveToken: g.Token, Generations: []l2MSSGeneration{g}}
	if err := writeL2MSSOwner(owner, m); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(owner)
	if err != nil {
		t.Fatal(err)
	}
	c := PathMTUController{Router: &api.Router{}, RunNFT: func(context.Context, ...string) ([]byte, error) {
		return []byte("Operation not permitted"), fmt.Errorf("exit status 1")
	}}
	if _, err := c.applyL2MSSTable(t.Context(), "nft", filepath.Join(dir, "l2.nft"), owner, false); err == nil {
		t.Fatal("transient list failure treated as missing")
	}
	after, err := os.ReadFile(owner)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("journal mutated on transient list failure: before=%s after=%s", before, after)
	}
}

func TestPathMTUControllerRendersLocalCaptureMSSClamp(t *testing.T) {
	dir := t.TempDir()
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-hybrid"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "OverlayPeer"},
			Metadata: api.ObjectMeta{Name: "onprem-main"},
			Spec: api.OverlayPeerSpec{
				Role:     "onprem",
				NodeID:   "onprem-router",
				Underlay: api.OverlayUnderlay{Type: "wireguard", Interface: "wg-hybrid"},
			},
		},
	}}}
	store := mapStore{}
	controller := PathMTUController{
		Router: router, OS: platform.OSLinux, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft"),
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureProtectExisting, CaptureType: "provider-secondary-ip", CaptureInterface: "ens3",
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := `iifname "ens3" oifname "wg-hybrid" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1300 tcp option maxseg size set 1300`
	if !strings.Contains(got, want) {
		t.Fatalf("BGP mobility MSS clamp missing %q:\n%s", want, got)
	}
}

func TestPathMTUControllerRendersSAMTransportMSSClamp(t *testing.T) {
	dir := t.TempDir()
	owner := []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "cloudedge-transport"}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "ens5"},
			Spec:     api.InterfaceSpec{IfName: "ens5", MTU: 1500},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-hybrid"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "samt-aws-onprem", OwnerRefs: owner},
			Spec: api.TunnelInterfaceSpec{
				Mode:              "ipip",
				Local:             "10.99.0.2",
				Remote:            "10.99.0.1",
				Address:           "10.255.0.10/31",
				UnderlayInterface: "wg-hybrid",
			},
		},
	}}}
	store := mapStore{}
	controller := PathMTUController{
		Router: router, OS: platform.OSLinux, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft"),
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureProtectExisting, CaptureType: "provider-secondary-ip", CaptureInterface: "ens5",
			TunnelInterfaces: []string{"samt-aws-onprem"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := `iifname "ens5" oifname "samt-aws-onprem" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1280 tcp option maxseg size set 1280`
	if !strings.Contains(got, want) {
		t.Fatalf("SAM transport MSS clamp missing %q:\n%s", want, got)
	}
}

func TestPathMTUControllerRendersSAMTransportHubRelayMSSClamp(t *testing.T) {
	dir := t.TempDir()
	owner := []api.OwnerRef{{APIVersion: api.MobilityAPIVersion, Kind: "SAMTransportProfile", Name: "cloudedge-transport"}}
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"},
			Metadata: api.ObjectMeta{Name: "ens20"},
			Spec:     api.InterfaceSpec{IfName: "ens20", MTU: 1500},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "WireGuardInterface"},
			Metadata: api.ObjectMeta{Name: "wg-hybrid"},
			Spec:     api.WireGuardInterfaceSpec{MTU: 1420},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "samt-aws", OwnerRefs: owner},
			Spec: api.TunnelInterfaceSpec{
				Mode:              "ipip",
				Local:             "10.99.0.1",
				Remote:            "10.99.0.2",
				Address:           "10.255.0.11/31",
				UnderlayInterface: "wg-hybrid",
			},
		},
		{
			TypeMeta: api.TypeMeta{APIVersion: api.HybridAPIVersion, Kind: "TunnelInterface"},
			Metadata: api.ObjectMeta{Name: "samt-oci", OwnerRefs: owner},
			Spec: api.TunnelInterfaceSpec{
				Mode:              "ipip",
				Local:             "10.99.0.1",
				Remote:            "10.99.0.4",
				Address:           "10.255.0.39/31",
				UnderlayInterface: "wg-hybrid",
			},
		},
	}}}
	store := mapStore{}
	controller := PathMTUController{
		Router: router, OS: platform.OSLinux, Store: store, DryRun: true, Path: filepath.Join(dir, "mss.nft"),
		LocalCaptureIntents: []dynamicconfig.LocalCaptureIntent{{
			ID: "cloudedge/10.77.60.9", PoolRef: "cloudedge", Address: "10.77.60.9/32",
			Disposition: dynamicconfig.CaptureDesired, CaptureType: "proxy-arp", CaptureInterface: "ens20",
			TunnelInterfaces: []string{"samt-aws", "samt-oci"},
		}},
	}
	if err := controller.Reconcile(t.Context()); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(controller.Path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{
		`iifname "ens20" oifname "samt-aws" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1280 tcp option maxseg size set 1280`,
		`iifname "ens20" oifname "samt-oci" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1280 tcp option maxseg size set 1280`,
		`iifname "samt-aws" oifname "samt-oci" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1280 tcp option maxseg size set 1280`,
		`iifname "samt-oci" oifname "samt-aws" ip protocol tcp tcp flags syn / syn,rst tcp option maxseg size > 1280 tcp option maxseg size set 1280`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("SAM transport hub relay MSS clamp missing %q:\n%s", want, got)
		}
	}
}

func TestPathMTUEffectiveViewEmptyPartsMatchesRawRouter(t *testing.T) {
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "Interface"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.InterfaceSpec{IfName: "ens19"}},
		{TypeMeta: api.TypeMeta{APIVersion: api.NetAPIVersion, Kind: "DSLiteTunnel"}, Metadata: api.ObjectMeta{Name: "ds-lite-a"}, Spec: api.DSLiteTunnelSpec{TunnelName: "ds-lite-a", MTU: 1454}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "lan"}, Spec: api.FirewallZoneSpec{Role: "trust", Interfaces: []string{"lan"}}},
		{TypeMeta: api.TypeMeta{APIVersion: api.FirewallAPIVersion, Kind: "FirewallZone"}, Metadata: api.ObjectMeta{Name: "wan"}, Spec: api.FirewallZoneSpec{Role: "untrust", Interfaces: []string{"ds-lite-a"}}},
	}}}
	view, err := buildDynamicRouteSAMView(router, &dynamicRouteSAMStore{objects: map[string]map[string]any{}}, time.Now().UTC(), platform.OSLinux)
	if err != nil {
		t.Fatalf("buildDynamicRouteSAMView: %v", err)
	}
	raw, err := render.NftablesTCPMSSClamp(router)
	if err != nil {
		t.Fatalf("raw render: %v", err)
	}
	effective, err := render.NftablesTCPMSSClamp(view.EffectiveRouter)
	if err != nil {
		t.Fatalf("effective render: %v", err)
	}
	if string(effective) != string(raw) {
		t.Fatalf("effective MSS render differs from raw\nraw:\n%s\neffective:\n%s", raw, effective)
	}
}

func countLogLine(logData, want string) int {
	count := 0
	for _, line := range strings.Split(logData, "\n") {
		if line == want {
			count++
		}
	}
	return count
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}
