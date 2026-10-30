// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
)

func realDynamicPathServer(t *testing.T) *localPathServer {
	t.Helper()
	server := gobgpserver.NewBgpServer()
	go server.Serve()
	t.Cleanup(server.Stop)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := server.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: &gobgpapi.Global{Asn: 64512, RouterId: "192.0.2.1", ListenPort: -1}}); err != nil {
		t.Fatal(err)
	}
	return &localPathServer{BgpServer: server}
}

type nativePathIdentity struct {
	identifier, peerASN uint32
	med                 uint32
}

func nativePathIdentities(t *testing.T, server *localPathServer) []nativePathIdentity {
	t.Helper()
	var result []nativePathIdentity
	if err := server.BgpServer.ListPath(gobgpapiutil.ListPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Family: gobgp.RF_IPv4_UC}, func(_ gobgp.NLRI, paths []*gobgpapiutil.Path) {
		for _, path := range paths {
			entry := nativePathIdentity{identifier: path.RemoteID, peerASN: path.PeerASN}
			for _, attr := range path.Attrs {
				if med, ok := attr.(*gobgp.PathAttributeMultiExitDisc); ok {
					entry.med = med.Value
				}
			}
			result = append(result, entry)
		}
	}); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestGoBGPDynamicIdentifiersReplaceAndWithdrawExactLocalOwner(t *testing.T) {
	server := realDynamicPathServer(t)
	ctx := context.Background()
	add := func(identifier, peerASN, med uint32) *gobgpapi.Path {
		path, err := bgpdaemon.PathFromAppliedPath(bgpdaemon.AppliedPath{Source: "MobilityPool/identity/node/a", Prefix: "10.77.60.11/32", Attrs: bgpdaemon.AppliedPathAttrs{NextHop: "192.0.2.2", MED: med}})
		if err != nil {
			t.Fatal(err)
		}
		path.Identifier = identifier
		if peerASN != 0 {
			path.SourceAsn = peerASN
			path.SourceId = "192.0.2.2"
			path.NeighborIp = "192.0.2.2"
		}
		if _, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path}); err != nil {
			t.Fatal(err)
		}
		return path
	}
	add(41, 0, 1)
	add(42, 0, 2)
	// Supply an actual GoBGP peer-origin path with the same prefix/id. This
	// tests local-owner withdrawal against the RIB, without a second simulator.
	add(41, 64513, 3)
	replacement := add(41, 0, 4)
	seen := nativePathIdentities(t, server)
	if len(seen) != 3 || !slices.Contains(seen, nativePathIdentity{identifier: 41, med: 4}) || !slices.Contains(seen, nativePathIdentity{identifier: 42, med: 2}) || !slices.Contains(seen, nativePathIdentity{identifier: 41, peerASN: 64513, med: 3}) {
		t.Fatalf("native replacement changed exact ownership: %#v", seen)
	}
	if err := server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: replacement}); err != nil {
		t.Fatal(err)
	}
	seen = nativePathIdentities(t, server)
	if len(seen) != 2 || !slices.Contains(seen, nativePathIdentity{identifier: 42, med: 2}) || !slices.Contains(seen, nativePathIdentity{identifier: 41, peerASN: 64513, med: 3}) {
		t.Fatalf("native withdrawal changed another owner or peer path: %#v", seen)
	}
}

type interruptedDynamicPathServer struct {
	*localPathServer
	beforeAdd func()
	afterAdd  func()
	addErr    error
	policyErr error
}

func (s *interruptedDynamicPathServer) AddPath(ctx context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	if s.beforeAdd != nil {
		s.beforeAdd()
	}
	if s.addErr != nil {
		return nil, s.addErr
	}
	response, err := s.localPathServer.AddPath(ctx, req)
	if err == nil && s.afterAdd != nil {
		s.afterAdd()
	}
	return response, err
}

func (s *interruptedDynamicPathServer) SetPolicies(ctx context.Context, req *gobgpapi.SetPoliciesRequest) error {
	if s.policyErr != nil {
		return s.policyErr
	}
	return s.localPathServer.SetPolicies(ctx, req)
}

func dynamicJournal(t *testing.T) (string, bgpdaemon.AppliedConfig) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal", "applied.json")
	initial := bgpdaemon.AppliedConfig{Policy: bgpdaemon.PolicyIntent{RouterName: "test"}, Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1", ListenPort: -1}}
	if err := bgpdaemon.WriteApplied(path, initial); err != nil {
		t.Fatal(err)
	}
	return path, bgpdaemon.Normalize(initial)
}

func interruptDynamicUUIDWrite(t *testing.T, server *localPathServer, statePath string, path bgpdaemon.AppliedPath) {
	t.Helper()
	dir := filepath.Dir(statePath)
	held := dir + "-held"
	fault := &interruptedDynamicPathServer{localPathServer: server}
	fault.beforeAdd = func() {
		stored, found, err := bgpdaemon.ReadApplied(statePath)
		if err != nil || !found || len(stored.PendingPathAdds) != 1 || stored.PendingPathAdds[0].UUID != "" {
			t.Fatalf("native Add preceded durable intent: %#v found=%t err=%v", stored, found, err)
		}
	}
	fault.afterAdd = func() {
		if err := os.Rename(dir, held); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dir, []byte("block checkpoint directory"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := upsertDynamicPath(context.Background(), fault, statePath, path); err == nil {
		t.Fatal("post-Add checkpoint failure reported success")
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(held, dir); err != nil {
		t.Fatal(err)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.PendingPathAdds) != 1 {
		t.Fatalf("lost pending native effect: %#v error=%v", stored, err)
	}
}

func TestDynamicPathRecoveryListsOnlyDurableNativeEffects(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	foreign := bgpdaemon.AppliedPath{Source: "MobilityPool/other/node/b", Prefix: "10.77.60.11/32", Attrs: bgpdaemon.AppliedPathAttrs{MED: 2}}
	if _, _, err := upsertDynamicPath(context.Background(), server, statePath, foreign); err != nil {
		t.Fatal(err)
	}
	owned := bgpdaemon.AppliedPath{Source: "MobilityPool/owner/node/a", Prefix: "10.77.60.11/32", Attrs: bgpdaemon.AppliedPathAttrs{MED: 1}}
	interruptDynamicUUIDWrite(t, server, statePath, owned)
	if paths := nativePathIdentities(t, server); len(paths) != 2 {
		t.Fatalf("native paths before recovery=%#v", paths)
	}
	handler := controlHandler(statePath, server, &sync.Mutex{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/paths?source="+url.QueryEscape(owned.Source), nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("recover listing=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var listed []bgpdaemon.AppliedPath
	if err := json.Unmarshal(recorder.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].Source != owned.Source || listed[0].UUID == "" {
		t.Fatalf("recovered listing=%#v", listed)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.PendingPathAdds) != 0 || len(stored.Paths) != 2 {
		t.Fatalf("recovered journal=%#v error=%v", stored, err)
	}
	if paths := nativePathIdentities(t, server); len(paths) != 2 {
		t.Fatalf("recovery duplicated local identity: %#v", paths)
	}
	if _, err := deleteDynamicPath(context.Background(), server, statePath, owned); err != nil {
		t.Fatal(err)
	}
	paths := nativePathIdentities(t, server)
	if len(paths) != 1 || paths[0].identifier != bgpdaemon.NativePathIdentifier(foreign.Source) {
		t.Fatalf("withdrawal changed another source: %#v", paths)
	}
}

func TestDynamicPendingPathWithdrawsOrphanWithoutUUID(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	owned := bgpdaemon.AppliedPath{Source: "MobilityPool/owner/node/a", Prefix: "10.77.60.11/32"}
	interruptDynamicUUIDWrite(t, server, statePath, owned)
	if _, err := deleteDynamicPath(context.Background(), server, statePath, owned); err != nil {
		t.Fatal(err)
	}
	if paths := nativePathIdentities(t, server); len(paths) != 0 {
		t.Fatalf("unrecorded-UUID native path survived delete: %#v", paths)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Paths) != 0 || len(stored.PendingPathAdds) != 0 {
		t.Fatalf("withdrawn journal=%#v error=%v", stored, err)
	}
}

func TestPendingNativeAddErrorsAreNotListedAsAbsent(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	path := bgpdaemon.AppliedPath{Source: "MobilityPool/owner/node/a", Prefix: "10.77.60.11/32"}
	failure := errors.New("native add unavailable")
	fault := &interruptedDynamicPathServer{localPathServer: server, addErr: failure}
	if _, _, err := upsertDynamicPath(context.Background(), fault, statePath, path); !errors.Is(err, failure) {
		t.Fatalf("add failure=%v", err)
	}
	handler := controlHandler(statePath, fault, &sync.Mutex{})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/paths?source="+url.QueryEscape(path.Source), nil))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("unresolved native operation looked absent: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.PendingPathAdds) != 1 || len(stored.Paths) != 0 {
		t.Fatalf("unresolved journal=%#v error=%v", stored, err)
	}
}

func TestDynamicPathPolicyFailureRetainsPendingRecovery(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	path := bgpdaemon.AppliedPath{Source: "MobilityPool/owner/node/a", Prefix: "10.77.60.11/32"}
	failure := errors.New("policy unavailable")
	fault := &interruptedDynamicPathServer{localPathServer: server, policyErr: failure}
	if _, _, err := upsertDynamicPath(context.Background(), fault, statePath, path); !errors.Is(err, failure) {
		t.Fatalf("policy failure=%v", err)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.PendingPathAdds) != 1 || len(stored.Paths) != 0 {
		t.Fatalf("policy failure lost recovery: %#v error=%v", stored, err)
	}
	fault.policyErr = nil
	if _, err := recoverPendingPaths(context.Background(), fault, statePath, stored, path.Source); err != nil {
		t.Fatal(err)
	}
	if paths := nativePathIdentities(t, server); len(paths) != 1 {
		t.Fatalf("policy retry duplicated native path: %#v", paths)
	}
}

func TestControllerCheckpointPreservesDaemonDynamicOwnership(t *testing.T) {
	server := realDynamicPathServer(t)
	statePath, stale := dynamicJournal(t)
	path := bgpdaemon.AppliedPath{Source: "MobilityPool/owner/node/a", Prefix: "10.77.60.11/32"}
	fault := &interruptedDynamicPathServer{localPathServer: server, addErr: errors.New("add interrupted")}
	if _, _, err := upsertDynamicPath(context.Background(), fault, statePath, path); err == nil {
		t.Fatal("expected pending add")
	}
	handler := controlHandler(statePath, server, &sync.Mutex{})
	checkpoint := func(config bgpdaemon.AppliedConfig) {
		wire, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/v1/applied", bytes.NewReader(wire)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("controller checkpoint=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	checkpoint(stale)
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.PendingPathAdds) != 1 {
		t.Fatalf("stale controller erased pending effect: %#v error=%v", stored, err)
	}
	applied, err := recoverPendingPaths(context.Background(), server, statePath, stored, path.Source)
	if err != nil {
		t.Fatal(err)
	}
	checkpoint(stale)
	stored, _, err = bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Paths) != 1 {
		t.Fatalf("stale controller erased applied dynamic path: %#v error=%v", stored, err)
	}
	if _, err := deleteDynamicPath(context.Background(), server, statePath, path); err != nil {
		t.Fatal(err)
	}
	checkpoint(applied)
	stored, _, err = bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Paths) != 0 {
		t.Fatalf("stale controller resurrected removed dynamic path: %#v error=%v", stored, err)
	}
}

func TestDaemonRestartReplaysPendingDynamicPathBeforeServingControl(t *testing.T) {
	old := realDynamicPathServer(t)
	statePath, _ := dynamicJournal(t)
	owned := bgpdaemon.AppliedPath{Source: "MobilityPool/owner/node/a", Prefix: "10.77.60.11/32"}
	interruptDynamicUUIDWrite(t, old, statePath, owned)
	old.BgpServer.Stop()
	restarted := gobgpserver.NewBgpServer()
	go restarted.Serve()
	t.Cleanup(restarted.Stop)
	server := &localPathServer{BgpServer: restarted}
	if err := restoreApplied(context.Background(), restarted, server, statePath, slog.Default()); err != nil {
		t.Fatal(err)
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Paths) != 1 || stored.Paths[0].UUID == "" || len(stored.PendingPathAdds) != 0 {
		t.Fatalf("restart checkpoint=%#v error=%v", stored, err)
	}
	if paths := nativePathIdentities(t, server); len(paths) != 1 || paths[0].identifier != bgpdaemon.NativePathIdentifier(owned.Source) {
		t.Fatalf("restart native ownership=%#v", paths)
	}
	if _, err := deleteDynamicPath(context.Background(), server, statePath, owned); err != nil {
		t.Fatal(err)
	}
	if paths := nativePathIdentities(t, server); len(paths) != 0 {
		t.Fatalf("restarted path survived owned withdrawal: %#v", paths)
	}
}

func TestInitialControllerCheckpointCannotPublishUnappliedDynamicPath(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "applied.json")
	config := bgpdaemon.AppliedConfig{Global: bgpdaemon.AppliedGlobal{ASN: 64512, RouterID: "192.0.2.1", ListenPort: -1}, Paths: []bgpdaemon.AppliedPath{{Source: "MobilityPool/stale/node/a", Prefix: "10.77.60.11/32"}}}
	wire, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	server := realDynamicPathServer(t)
	recorder := httptest.NewRecorder()
	controlHandler(statePath, server, &sync.Mutex{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/v1/applied", bytes.NewReader(wire)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("initial checkpoint=%d body=%s", recorder.Code, recorder.Body.String())
	}
	stored, _, err := bgpdaemon.ReadApplied(statePath)
	if err != nil || len(stored.Paths) != 0 || len(nativePathIdentities(t, server)) != 0 {
		t.Fatalf("controller fabricated a dynamic effect: %#v error=%v", stored, err)
	}
}

func TestDynamicPathListingRejectsUnknownOrInvalidJournal(t *testing.T) {
	for _, body := range []string{"", "{}"} {
		t.Run("state="+body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "applied.json")
			if body != "" {
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			recorder := httptest.NewRecorder()
			controlHandler(path, realDynamicPathServer(t), &sync.Mutex{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/paths?source=MobilityPool/owner/node/a", nil))
			if recorder.Code < 400 {
				t.Fatalf("unknown journal fabricated absent path: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestRestoreRejectsPreviousNativeIdentityJournalBeforeGoBGP(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applied.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"global":{"asn":64512,"routerID":"192.0.2.1"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := restoreApplied(context.Background(), nil, nil, path, nil); err == nil || !strings.Contains(err.Error(), "unsupported applied BGP config version") {
		t.Fatalf("old native format restore=%v", err)
	}
}
