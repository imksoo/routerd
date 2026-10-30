// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	gobgpapi "github.com/osrg/gobgp/v4/api"
	gobgpapiutil "github.com/osrg/gobgp/v4/pkg/apiutil"
	gobgp "github.com/osrg/gobgp/v4/pkg/packet/bgp"
	gobgpserver "github.com/osrg/gobgp/v4/pkg/server"

	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/version"
)

var defaultSocketPath, defaultControlSocketPath, defaultStatePath = func() (string, string, string) {
	defaults, _ := platform.Current()
	return defaults.BGPPaths()
}()

// Keep shutdown bounded even when a control request or a GoBGP peer goroutine
// does not drain.  The service manager can then complete a host power-down
// without waiting for its much longer default stop timeout.
const (
	defaultControlShutdownTimeout = 5 * time.Second
	defaultBGPShutdownTimeout     = 10 * time.Second
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "daemon"
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version.String())
		return nil
	}
	if len(args) > 0 && args[0] != "daemon" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	if len(args) > 0 {
		args = args[1:]
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socketPath := fs.String("socket", defaultSocketPath, "GoBGP gRPC Unix socket path")
	controlSocketPath := fs.String("control-socket", defaultControlSocketPath, "routerd-bgp control Unix socket path")
	statePath := fs.String("state-file", defaultStatePath, "applied BGP state JSON path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socketPath == "" {
		return fmt.Errorf("--socket is required")
	}
	if *controlSocketPath == "" {
		return fmt.Errorf("--control-socket is required")
	}
	if *statePath == "" {
		return fmt.Errorf("--state-file is required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	defaults, _ := platform.Current()
	return runBGPDaemon(ctx, defaults.RuntimeDir, *socketPath, *controlSocketPath, *statePath)
}

// runtimeDir comes from platform defaults, independently of all public path
// flags. One native GoBGP instance owns each actual kernel network world.
func runBGPDaemon(ctx context.Context, runtimeDir, socketPath, controlSocketPath, statePath string) (result error) {
	owner, err := acquireBGPProcessOwner(ctx, runtimeDir)
	if err != nil {
		return err
	}
	nativeStarted := false
	defer func() { result = errors.Join(result, owner.finish(!nativeStarted)) }()
	if err := os.MkdirAll(filepath.Dir(socketPath), 0755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(controlSocketPath), 0755); err != nil {
		return fmt.Errorf("create control socket directory: %w", err)
	}
	_ = os.Remove(socketPath)
	_ = os.Remove(controlSocketPath)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	// GoBGP starts its private gRPC listener goroutine in the constructor,
	// without exposing a join. Once started, keep singleton ownership until
	// OS exit even after normal Stop; an unobserved listener cannot authorize
	// another instance in this process.
	nativeStarted = true
	server := gobgpserver.NewBgpServer(gobgpserver.GrpcListenAddress("unix://" + socketPath))
	coreDone := make(chan struct{})
	go func() {
		defer close(coreDone)
		server.Serve()
	}()
	var control *http.Server
	defer func() {
		acknowledged := stopBGPDaemon(control, func() {
			// The management round trip also handles cancellation before Serve
			// was scheduled: Stop alone would return "not running" too early.
			_, _ = server.GetBgp(context.Background(), &gobgpapi.GetBgpRequest{})
			server.Stop()
			<-coreDone
		}, logger, defaultControlShutdownTimeout, defaultBGPShutdownTimeout)
		if !acknowledged {
			result = errors.Join(result, errors.New("BGP shutdown was not acknowledged; process ownership retained until exit"))
		}
	}()
	paths := &localPathServer{BgpServer: server}
	if err := restoreApplied(ctx, server, paths, statePath, logger); err != nil {
		return err
	}
	control, err = serveControlSocket(controlSocketPath, statePath, paths)
	if err != nil {
		return err
	}
	logger.Info("routerd-bgp daemon started", "socket", socketPath, "controlSocket", controlSocketPath, "stateFile", statePath, "version", version.String())

	<-ctx.Done()
	logger.Info("routerd-bgp daemon stopping")
	return nil
}

type bgpControlShutdowner interface {
	Shutdown(context.Context) error
	Close() error
}

// stopBGPDaemon gives the control HTTP server a brief graceful drain, then
// gives GoBGP a separate bounded chance to withdraw its sessions.  Both
// dependencies have historically exposed unbounded stop operations.  Returning
// after an unacknowledged deadline retains the process lock until OS exit.
func stopBGPDaemon(control bgpControlShutdowner, stop func(), logger *slog.Logger, controlTimeout, bgpTimeout time.Duration) bool {
	acknowledged := true
	if controlTimeout <= 0 {
		controlTimeout = defaultControlShutdownTimeout
	}
	if bgpTimeout <= 0 {
		bgpTimeout = defaultBGPShutdownTimeout
	}
	if control != nil {
		controlCtx, cancel := context.WithTimeout(context.Background(), controlTimeout)
		err := control.Shutdown(controlCtx)
		cancel()
		if err != nil {
			acknowledged = false
			if logger != nil {
				logger.Warn("routerd-bgp control socket did not drain before shutdown deadline", "error", err)
			}
			// Shutdown leaves active requests alive on a deadline.  Close makes
			// the stop boundary explicit rather than carrying those requests into
			// the remaining BGP shutdown window.
			_ = control.Close()
		}
	}
	if stop == nil {
		return acknowledged
	}
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	timer := time.NewTimer(bgpTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		acknowledged = false
		if logger != nil {
			logger.Warn("routerd-bgp GoBGP shutdown exceeded deadline; exiting process", "timeout", bgpTimeout)
		}
	}
	return acknowledged
}

type pathServer interface {
	AddPath(context.Context, *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error)
	DeletePath(context.Context, *gobgpapi.DeletePathRequest) error
}

// observedPathServer is intentionally separate from pathServer.  The control
// API's /v1/paths endpoint is a serialized read-modify-write view of
// applied.json; a live RIB observation must never be mistaken for that local
// desired-path journal.
type observedPathServer interface {
	ListObservedPaths(context.Context) ([]bgpdaemon.ObservedPath, error)
}

// localPathServer keeps routerd's narrow protobuf-shaped path interface while
// adapting GoBGP v4's in-process native path API. The remote controller uses
// the same protobuf requests over gRPC.
type localPathServer struct {
	*gobgpserver.BgpServer
}

func (s *localPathServer) AddPath(_ context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	path, err := nativePath(req.GetPath())
	if err != nil {
		return nil, err
	}
	results, err := s.BgpServer.AddPath(gobgpapiutil.AddPathRequest{
		VRFID: req.GetVrfId(),
		Paths: []*gobgpapiutil.Path{path},
	})
	if err != nil {
		return nil, err
	}
	if len(results) != 1 {
		return nil, fmt.Errorf("GoBGP AddPath returned %d results, want 1", len(results))
	}
	id, err := results[0].UUID.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return &gobgpapi.AddPathResponse{Uuid: id}, results[0].Error
}

func (s *localPathServer) DeletePath(_ context.Context, req *gobgpapi.DeletePathRequest) error {
	request := gobgpapiutil.DeletePathRequest{VRFID: req.GetVrfId()}
	switch {
	case len(req.GetUuid()) > 0:
		id, err := uuid.FromBytes(req.GetUuid())
		if err != nil {
			return err
		}
		request.UUIDs = []uuid.UUID{id}
	case req.GetPath() != nil:
		path, err := nativePath(req.GetPath())
		if err != nil {
			return err
		}
		request.Paths = []*gobgpapiutil.Path{path}
	default:
		request.DeleteAll = true
		if family := req.GetFamily(); family != nil {
			nativeFamily := gobgp.NewFamily(uint16(family.GetAfi()), uint8(family.GetSafi()))
			request.DeleteFamily = &nativeFamily
		}
	}
	return s.BgpServer.DeletePath(request)
}

// ListObservedPaths returns the live IPv4 unicast global RIB.  The response
// keeps only the fields needed by routerd's control-plane safety checks; it is
// never persisted and is not an alternate configuration API.
func (s *localPathServer) ListObservedPaths(ctx context.Context) ([]bgpdaemon.ObservedPath, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []bgpdaemon.ObservedPath
	var callbackErr error
	err := s.BgpServer.ListPath(gobgpapiutil.ListPathRequest{
		TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL,
		Family:    gobgp.RF_IPv4_UC,
	}, func(prefix gobgp.NLRI, paths []*gobgpapiutil.Path) {
		if callbackErr != nil || ctx.Err() != nil {
			callbackErr = ctx.Err()
			return
		}
		for _, path := range paths {
			if path == nil || path.Withdrawal {
				continue
			}
			out = append(out, observedPath(prefix.String(), path))
		}
	})
	if err != nil {
		return nil, err
	}
	if callbackErr != nil {
		return nil, callbackErr
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Prefix != out[j].Prefix {
			return out[i].Prefix < out[j].Prefix
		}
		if out[i].PeerAddress != out[j].PeerAddress {
			return out[i].PeerAddress < out[j].PeerAddress
		}
		return out[i].Best && !out[j].Best
	})
	return out, nil
}

func observedPath(prefix string, path *gobgpapiutil.Path) bgpdaemon.ObservedPath {
	out := bgpdaemon.ObservedPath{
		Prefix:      strings.TrimSpace(prefix),
		Best:        path.Best,
		Valid:       !path.IsNexthopInvalid,
		Stale:       path.Stale,
		PeerAddress: path.PeerAddress.String(),
	}
	if !path.PeerAddress.IsValid() {
		out.PeerAddress = ""
	}
	for _, attr := range path.Attrs {
		switch value := attr.(type) {
		case *gobgp.PathAttributeCommunities:
			for _, community := range value.Value {
				out.Communities = append(out.Communities, fmt.Sprintf("%d:%d", community>>16, community&0xffff))
			}
		}
	}
	out.Communities = stringutil.UniqueTrimmedSorted(out.Communities)
	return out
}

func nativePath(path *gobgpapi.Path) (*gobgpapiutil.Path, error) {
	if path == nil {
		return nil, fmt.Errorf("path is required")
	}
	nlri, err := gobgpapiutil.GetNativeNlri(path)
	if err != nil {
		return nil, err
	}
	attrs, err := gobgpapiutil.GetNativePathAttributes(path)
	if err != nil {
		return nil, err
	}
	family := path.GetFamily()
	if family == nil {
		return nil, fmt.Errorf("path family is required")
	}
	peerID, _ := netip.ParseAddr(path.GetSourceId())
	peerAddress, _ := netip.ParseAddr(path.GetNeighborIp())
	out := &gobgpapiutil.Path{
		Family:             gobgp.NewFamily(uint16(family.GetAfi()), uint8(family.GetSafi())),
		Nlri:               nlri,
		Attrs:              attrs,
		Age:                path.GetAge().GetSeconds(),
		Best:               path.GetBest(),
		Withdrawal:         path.GetIsWithdraw(),
		Stale:              path.GetStale(),
		PeerASN:            path.GetSourceAsn(),
		PeerID:             peerID,
		PeerAddress:        peerAddress,
		IsFromExternal:     path.GetIsFromExternal(),
		NoImplicitWithdraw: path.GetNoImplicitWithdraw(),
		IsNexthopInvalid:   path.GetIsNexthopInvalid(),
		RemoteID:           path.GetIdentifier(),
		LocalID:            path.GetLocalIdentifier(),
	}
	if out.PeerASN != 0 && !out.PeerID.IsValid() {
		return nil, fmt.Errorf("source ID must be set when source ASN is nonzero")
	}
	return out, nil
}

func serveControlSocket(socketPath, statePath string, paths policyOwnerServer) (*http.Server, error) {
	return serveControlSocketWithStateLock(socketPath, statePath, paths, &sync.Mutex{})
}

func serveControlSocketWithStateLock(socketPath, statePath string, paths policyOwnerServer, appliedStateMu sync.Locker) (*http.Server, error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen control socket: %w", err)
	}
	server := &http.Server{Handler: controlHandler(statePath, paths, appliedStateMu)}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "routerd-bgp control socket failed: %v\n", err)
		}
	}()
	return server, nil
}

func controlHandler(statePath string, paths policyOwnerServer, appliedStateMu sync.Locker) http.Handler {
	mux := http.NewServeMux()
	registerOwnedRoutes(mux, statePath, paths, appliedStateMu)
	// All applied.json access through this control socket is serialized. Path
	// requests are read-modify-write transactions; without this gate a request
	// which read state before routerd stored a direct-peer transition fence could
	// overwrite that fence after it was written.
	mux.HandleFunc("/v1/applied", func(w http.ResponseWriter, r *http.Request) {
		appliedStateMu.Lock()
		defer appliedStateMu.Unlock()
		switch r.Method {
		case http.MethodGet:
			config, _, err := bgpdaemon.ReadApplied(statePath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, config)
		case http.MethodPut:
			var config bgpdaemon.AppliedConfig
			if err := decodeControlRequest(w, r, &config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := bgpdaemon.Validate(config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			current, found, err := bgpdaemon.ReadApplied(statePath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Only the daemon's native transactions may acknowledge policy or
			// local paths. A controller transport checkpoint cannot invent them.
			config.Paths, config.PendingPathAdds, config.PendingPathRemovals = nil, nil, nil
			config.Policy, config.PendingPolicy = bgpdaemon.PolicyIntent{}, nil
			if found {
				config.Paths = current.Paths
				config.PendingPathAdds = current.PendingPathAdds
				config.PendingPathRemovals = current.PendingPathRemovals
				config.Policy, config.PendingPolicy = current.Policy, current.PendingPolicy
			}
			if err := bgpdaemon.Validate(config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := validateNativePathOwners(config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := bgpdaemon.WriteApplied(statePath, config); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, bgpdaemon.Normalize(config))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/paths", func(w http.ResponseWriter, r *http.Request) {
		appliedStateMu.Lock()
		defer appliedStateMu.Unlock()
		switch r.Method {
		case http.MethodGet:
			config, found, err := bgpdaemon.ReadApplied(statePath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if !found {
				http.Error(w, "applied BGP config is not initialized", http.StatusServiceUnavailable)
				return
			}
			if err := bgpdaemon.Validate(config); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			source := strings.TrimSpace(r.URL.Query().Get("source"))
			config, err = recoverPendingPaths(r.Context(), paths, statePath, config, source)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			out := append(make([]bgpdaemon.AppliedPath, 0, len(config.Paths)), config.Paths...)
			if source != "" {
				out = make([]bgpdaemon.AppliedPath, 0)
				for _, path := range config.Paths {
					if path.Source == source {
						out = append(out, path)
					}
				}
			}
			writeJSON(w, out)
		case http.MethodPost:
			path, err := decodePathRequest(w, r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			applied, updated, err := upsertDynamicPath(r.Context(), paths, statePath, path)
			if err != nil {
				http.Error(w, err.Error(), httpStatusForPathError(err))
				return
			}
			if updated == nil {
				writeJSON(w, applied)
				return
			}
			writeJSON(w, updated)
		case http.MethodDelete:
			path, err := decodeDeletePathRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			config, err := deleteDynamicPath(r.Context(), paths, statePath, path)
			if err != nil {
				http.Error(w, err.Error(), httpStatusForPathError(err))
				return
			}
			writeJSON(w, config)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/observed-paths", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		observer, ok := paths.(observedPathServer)
		if !ok {
			http.Error(w, "live BGP RIB observation is unavailable", http.StatusServiceUnavailable)
			return
		}
		observed, err := observer.ListObservedPaths(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if observed == nil {
			observed = make([]bgpdaemon.ObservedPath, 0)
		}
		writeJSON(w, observed)
	})
	return mux
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func restoreApplied(ctx context.Context, server *gobgpserver.BgpServer, paths policyOwnerServer, statePath string, logger *slog.Logger) error {
	pending, err := bgpdaemon.RemovalPending(statePath)
	if err != nil {
		return fmt.Errorf("read BGP removal intent: %w", err)
	}
	if pending {
		return nil
	}
	applied, ok, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return fmt.Errorf("read applied BGP state: %w", err)
	}
	if !ok {
		return nil
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return fmt.Errorf("validate applied BGP state: %w", err)
	}
	if err := server.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: appliedGlobal(applied.Global)}); err != nil {
		return fmt.Errorf("restore BGP global: %w", err)
	}
	withheldDirectPeers := pendingDirectPeerTransitions(applied)
	applied, err = refreshOwnedPathPolicies(ctx, paths, statePath, applied)
	if err != nil {
		return fmt.Errorf("restore BGP policy: %w", err)
	}
	for _, peer := range sortedPeers(applied.Peers) {
		if withheldDirectPeers[peer.Address] {
			logger.Warn("skip pending direct peer transition during BGP restore", "peer", peer.Address)
			continue
		}
		if err := server.AddPeer(ctx, &gobgpapi.AddPeerRequest{Peer: appliedPeer(peer, applied.Global)}); err != nil {
			return fmt.Errorf("restore BGP peer %s: %w", peer.Address, err)
		}
	}
	if err := restoreAppliedPaths(ctx, paths, &applied); err != nil {
		return err
	}
	applied, err = refreshOwnedPathPolicies(ctx, paths, statePath, applied)
	if err != nil {
		return fmt.Errorf("restore BGP dynamic policy refresh: %w", err)
	}
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return fmt.Errorf("persist restored BGP path UUIDs: %w", err)
	}
	logger.Info("restored applied BGP state", "peers", len(applied.Peers), "paths", len(applied.Paths), "hash", bgpdaemon.Hash(applied))
	return nil
}

func pendingDirectPeerTransitions(applied bgpdaemon.AppliedConfig) map[string]bool {
	out := map[string]bool{}
	// A pending policy target has not acknowledged its native replacement and
	// route refresh. Keep direct accelerators withheld while startup recovers
	// that target; the controller re-adds them from current transport intent.
	// RR peers remain the forwarding path during this recovery.
	if applied.PendingPolicy != nil {
		for address, peer := range applied.Peers {
			if peer.PreserveImportPrefixes {
				out[address] = true
			}
		}
	}
	for _, address := range applied.PendingDirectPeerAdditions {
		if address = strings.TrimSpace(address); address != "" {
			out[address] = true
		}
	}
	for _, address := range applied.PendingDirectPeerRemovals {
		if address = strings.TrimSpace(address); address != "" {
			out[address] = true
		}
	}
	return out
}

func restoreAppliedPaths(ctx context.Context, server pathServer, applied *bgpdaemon.AppliedConfig) error {
	if applied == nil {
		return nil
	}
	normalized := bgpdaemon.Normalize(*applied)
	for _, pending := range normalized.PendingPathAdds {
		normalized.Paths = append(withoutAppliedPath(normalized.Paths, bgpdaemon.AppliedPathKey(pending)), pending)
	}
	if err := validateNativePathOwners(normalized); err != nil {
		return err
	}
	for i, appliedPath := range normalized.Paths {
		path, err := bgpdaemon.PathFromAppliedPath(appliedPath)
		if err != nil {
			return fmt.Errorf("restore BGP path %s/%s: %w", appliedPath.Source, appliedPath.Prefix, err)
		}
		resp, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: path})
		if err != nil {
			return fmt.Errorf("restore BGP path %s/%s: %w", appliedPath.Source, appliedPath.Prefix, err)
		}
		normalized.Paths[i].UUID = bgpdaemon.EncodeUUID(resp.GetUuid())
	}
	normalized.PendingPathAdds = nil
	*applied = bgpdaemon.Normalize(normalized)
	return nil
}

func appliedGlobal(global bgpdaemon.AppliedGlobal) *gobgpapi.Global {
	out := &gobgpapi.Global{
		Asn:              global.ASN,
		RouterId:         global.RouterID,
		ListenPort:       int32(global.ListenPort),
		ListenAddresses:  global.ListenAddresses,
		UseMultiplePaths: global.UseMultiplePaths,
	}
	for _, family := range global.Families {
		switch family {
		case "ipv6-unicast":
			out.Families = append(out.Families, 1)
		default:
			out.Families = append(out.Families, 0)
		}
	}
	if len(out.Families) == 0 {
		out.Families = []uint32{0}
	}
	if gr := global.GracefulRestart; gr != nil && gr.Enabled {
		out.GracefulRestart = &gobgpapi.GracefulRestart{Enabled: true, RestartTime: gr.RestartTime, StaleRoutesTime: gr.StaleRoutesTime}
	}
	return out
}

func appliedPeer(peer bgpdaemon.AppliedPeer, global bgpdaemon.AppliedGlobal) *gobgpapi.Peer {
	send := peer.SendCommunity
	if send == 0 {
		send = 3
	}
	peerType := gobgpapi.PeerType_PEER_TYPE_EXTERNAL
	if global.ASN != 0 && peer.ASN == global.ASN {
		peerType = gobgpapi.PeerType_PEER_TYPE_INTERNAL
	}
	out := &gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{
			NeighborAddress: peer.Address,
			PeerAsn:         peer.ASN,
			AuthPassword:    peer.Password,
			Type:            peerType,
			SendCommunity:   send,
		},
		Timers: &gobgpapi.Timers{Config: timers(peer.TimersProfile)},
		AfiSafis: []*gobgpapi.AfiSafi{
			afiSafi(ipv4Family()),
			afiSafi(ipv6Family()),
		},
		Transport: appliedPeerTransport(peer.PassiveMode),
	}
	if gr := peer.GracefulRestart; gr != nil && gr.Enabled {
		out.GracefulRestart = &gobgpapi.GracefulRestart{Enabled: true, RestartTime: gr.RestartTime, StaleRoutesTime: gr.StaleRoutesTime}
	}
	if peer.EbgpMultihop > 1 {
		out.EbgpMultihop = &gobgpapi.EbgpMultihop{Enabled: true, MultihopTtl: uint32(peer.EbgpMultihop)}
	}
	if peer.RouteReflectorClient {
		out.RouteReflector = &gobgpapi.RouteReflector{
			RouteReflectorClient:    true,
			RouteReflectorClusterId: strings.TrimSpace(peer.RouteReflectorClusterID),
		}
	}
	return out
}

func appliedPeerTransport(passiveMode bool) *gobgpapi.Transport {
	if !passiveMode {
		return nil
	}
	return &gobgpapi.Transport{PassiveMode: true}
}

func timers(profile string) *gobgpapi.TimersConfig {
	switch profile {
	case "fast":
		return &gobgpapi.TimersConfig{ConnectRetry: 1, HoldTime: 9, KeepaliveInterval: 3, IdleHoldTimeAfterReset: 1}
	case "slow":
		return &gobgpapi.TimersConfig{ConnectRetry: 30, HoldTime: 180, KeepaliveInterval: 60, IdleHoldTimeAfterReset: 5}
	default:
		return &gobgpapi.TimersConfig{ConnectRetry: 10, HoldTime: 90, KeepaliveInterval: 30, IdleHoldTimeAfterReset: 1}
	}
}

func afiSafi(family *gobgpapi.Family) *gobgpapi.AfiSafi {
	return &gobgpapi.AfiSafi{
		Config: &gobgpapi.AfiSafiConfig{Family: family, Enabled: true},
		UseMultiplePaths: &gobgpapi.UseMultiplePaths{
			Config: &gobgpapi.UseMultiplePathsConfig{Enabled: true},
			Ebgp:   &gobgpapi.Ebgp{Config: &gobgpapi.EbgpConfig{MaximumPaths: 16}},
		},
	}
}

func decodePathRequest(w http.ResponseWriter, r *http.Request) (bgpdaemon.AppliedPath, error) {
	defer r.Body.Close()
	var path bgpdaemon.AppliedPath
	if err := decodeControlRequest(w, r, &path); err != nil {
		return bgpdaemon.AppliedPath{}, err
	}
	return validateDynamicMobilityPath(path)
}

func decodeDeletePathRequest(r *http.Request) (bgpdaemon.AppliedPath, error) {
	path := bgpdaemon.AppliedPath{
		Source: strings.TrimSpace(r.URL.Query().Get("source")),
		Prefix: strings.TrimSpace(r.URL.Query().Get("prefix")),
	}
	return validateDynamicMobilityPath(path)
}

func validateDynamicMobilityPath(path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error) {
	path = bgpdaemon.NormalizeAppliedPath(path)
	if !bgpdaemon.IsMobilityPathSource(path.Source) {
		return bgpdaemon.AppliedPath{}, fmt.Errorf("dynamic BGP path source %q is not a MobilityPool source", path.Source)
	}
	prefix, err := netip.ParsePrefix(path.Prefix)
	if err != nil {
		return bgpdaemon.AppliedPath{}, fmt.Errorf("dynamic BGP path prefix: %w", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return bgpdaemon.AppliedPath{}, fmt.Errorf("dynamic mobility BGP paths must be IPv4 /32, got %s", prefix.String())
	}
	path.Prefix = prefix.String()
	path.Family = bgpdaemon.AppliedPathFamilyIPv4Unicast
	if err := bgpdaemon.ValidateAppliedPath(path); err != nil {
		return bgpdaemon.AppliedPath{}, err
	}
	return path, nil
}

func upsertDynamicPath(ctx context.Context, server policyOwnerServer, statePath string, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, *bgpdaemon.AppliedPath, error) {
	path, err := validateDynamicMobilityPath(path)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	return upsertOwnedPath(ctx, server, statePath, path)
}

func upsertOwnedPath(ctx context.Context, server policyOwnerServer, statePath string, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, *bgpdaemon.AppliedPath, error) {
	if err := ctx.Err(); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	applied, ok, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	if !ok {
		return bgpdaemon.AppliedConfig{}, nil, fmt.Errorf("applied BGP config is not initialized")
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	if applied.PendingPolicy != nil {
		applied, err = refreshOwnedPathPolicies(ctx, server, statePath, applied)
		if err != nil {
			return applied, nil, err
		}
	}
	path = bgpdaemon.NormalizeAppliedPath(path)
	if err := bgpdaemon.ValidateAppliedPath(path); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	path.UUID = ""
	key := bgpdaemon.AppliedPathKey(path)
	pending := false
	for _, intent := range applied.PendingPathAdds {
		pending = pending || bgpdaemon.AppliedPathKey(intent) == key
	}
	if !pending {
		for i, existing := range applied.Paths {
			if bgpdaemon.AppliedPathKey(existing) == key && reflect.DeepEqual(existing.Attrs, path.Attrs) && existing.UUID != "" {
				return applied, &applied.Paths[i], nil
			}
		}
	}
	applied.PendingPathAdds = append(withoutAppliedPath(applied.PendingPathAdds, key), path)
	if err := validateNativePathOwners(applied); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	applied, err = finishPathAdd(ctx, server, statePath, applied, path)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	for i := range applied.Paths {
		if bgpdaemon.AppliedPathKey(applied.Paths[i]) == key {
			return applied, &applied.Paths[i], nil
		}
	}
	return applied, nil, fmt.Errorf("completed dynamic path %s is missing from applied state", key)
}

// finishPathAdd retries the same native identity, replacing an orphan
// from an earlier successful AddPath whose returned UUID was never persisted.
func finishPathAdd(ctx context.Context, server policyOwnerServer, statePath string, applied bgpdaemon.AppliedConfig, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, error) {
	if err := ctx.Err(); err != nil {
		return applied, err
	}
	reqPath, err := bgpdaemon.PathFromAppliedPath(path)
	if err != nil {
		return applied, err
	}
	resp, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: reqPath})
	if err != nil {
		return applied, err
	}
	if len(resp.GetUuid()) == 0 {
		return applied, fmt.Errorf("GoBGP returned no UUID for dynamic path %s", path.Prefix)
	}
	path.UUID = bgpdaemon.EncodeUUID(resp.GetUuid())
	key := bgpdaemon.AppliedPathKey(path)
	applied, err = refreshOwnedPathPolicies(ctx, server, statePath, applied)
	if err != nil {
		return applied, err
	}
	applied.Paths = append(withoutAppliedPath(applied.Paths, key), path)
	applied.PendingPathAdds = withoutAppliedPath(applied.PendingPathAdds, key)
	applied = bgpdaemon.Normalize(applied)
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return applied, err
	}
	return applied, nil
}

func recoverPendingPaths(ctx context.Context, server policyOwnerServer, statePath string, applied bgpdaemon.AppliedConfig, source string) (bgpdaemon.AppliedConfig, error) {
	var err error
	for _, path := range append([]bgpdaemon.AppliedPath(nil), applied.PendingPathRemovals...) {
		if source != "" && source != path.Source {
			continue
		}
		applied, err = deleteOwnedPath(ctx, server, statePath, path)
		if err != nil {
			return applied, err
		}
	}
	if applied.PendingPolicy != nil {
		applied, err = refreshOwnedPathPolicies(ctx, server, statePath, applied)
		if err != nil {
			return applied, err
		}
	}
	if len(applied.PendingPathAdds) == 0 {
		return applied, nil
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return applied, err
	}
	if err := validateNativePathOwners(applied); err != nil {
		return applied, err
	}
	pending := append([]bgpdaemon.AppliedPath(nil), applied.PendingPathAdds...)
	for _, path := range pending {
		if source != "" && source != path.Source {
			continue
		}
		next, err := finishPathAdd(ctx, server, statePath, applied, path)
		if err != nil {
			return applied, fmt.Errorf("recover pending dynamic path %s: %w", path.Prefix, err)
		}
		applied = next
	}
	return applied, nil
}

func withoutAppliedPath(paths []bgpdaemon.AppliedPath, key string) []bgpdaemon.AppliedPath {
	out := make([]bgpdaemon.AppliedPath, 0, len(paths))
	for _, path := range paths {
		if bgpdaemon.AppliedPathKey(path) != key {
			out = append(out, path)
		}
	}
	return out
}

// GoBGP's local path key is Identifier plus prefix. Reserve zero for static
// paths and retain each dynamic source's identity across a lost UUID/restart.

func validateNativePathOwners(applied bgpdaemon.AppliedConfig) error {
	owners := map[string]string{}
	for _, group := range [][]bgpdaemon.AppliedPath{applied.Paths, applied.PendingPathAdds, applied.PendingPathRemovals} {
		for _, path := range group {
			key := fmt.Sprintf("%s/%d", path.Prefix, bgpdaemon.NativePathIdentifier(path.Source))
			if owner := owners[key]; owner != "" && owner != path.Source {
				return fmt.Errorf("dynamic BGP native identity collision between %s and %s for %s", owner, path.Source, path.Prefix)
			}
			owners[key] = path.Source
		}
	}
	return nil
}

func deleteDynamicPath(ctx context.Context, server policyOwnerServer, statePath string, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, error) {
	path, err := validateDynamicMobilityPath(path)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	return deleteOwnedPath(ctx, server, statePath, path)
}

func deleteOwnedPath(ctx context.Context, server policyOwnerServer, statePath string, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, error) {
	if err := ctx.Err(); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	applied, ok, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	if !ok {
		return bgpdaemon.AppliedConfig{}, fmt.Errorf("applied BGP config is not initialized")
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	path = bgpdaemon.NormalizeAppliedPath(path)
	if err := bgpdaemon.ValidateAppliedPath(path); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	if err := validateNativePathOwners(applied); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	key := bgpdaemon.AppliedPathKey(path)
	var recorded *bgpdaemon.AppliedPath
	pending := false
	for i := range applied.Paths {
		if bgpdaemon.AppliedPathKey(applied.Paths[i]) == key {
			recorded = &applied.Paths[i]
		}
	}
	for _, intent := range applied.PendingPathAdds {
		pending = pending || bgpdaemon.AppliedPathKey(intent) == key
	}
	for _, intent := range applied.PendingPathRemovals {
		pending = pending || bgpdaemon.AppliedPathKey(intent) == key
	}
	if recorded == nil && !pending {
		return applied, nil
	}
	applied.Paths = withoutAppliedPath(applied.Paths, key)
	applied.PendingPathAdds = withoutAppliedPath(applied.PendingPathAdds, key)
	applied.PendingPathRemovals = append(withoutAppliedPath(applied.PendingPathRemovals, key), path)
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return applied, err
	}
	// Native source identity is stable across lost or stale returned UUIDs.
	// GoBGP confirms this exact local withdrawal idempotently, preserving
	// another local source and peer-origin paths for the same prefix.
	native, err := bgpdaemon.PathFromAppliedPath(path)
	if err != nil {
		return applied, err
	}
	if err := server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_TABLE_TYPE_GLOBAL, Path: native}); err != nil {
		return applied, err
	}
	applied.Paths = withoutAppliedPath(applied.Paths, key)
	applied.PendingPathAdds = withoutAppliedPath(applied.PendingPathAdds, key)
	applied.PendingPathRemovals = withoutAppliedPath(applied.PendingPathRemovals, key)
	applied = bgpdaemon.Normalize(applied)
	applied, err = refreshOwnedPathPolicies(ctx, server, statePath, applied)
	if err != nil {
		return applied, err
	}
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return applied, err
	}
	return applied, nil
}

func refreshOwnedPathPolicies(ctx context.Context, server policyOwnerServer, statePath string, applied bgpdaemon.AppliedConfig) (bgpdaemon.AppliedConfig, error) {
	target := applied.Policy
	if applied.PendingPolicy != nil {
		target = *applied.PendingPolicy
	}
	return reconcilePolicyConfig(ctx, server, statePath, applied, target)
}

func httpStatusForPathError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	if strings.Contains(msg, "not initialized") {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func ipv4Family() *gobgpapi.Family {
	return &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP, Safi: gobgpapi.Family_SAFI_UNICAST}
}

func ipv6Family() *gobgpapi.Family {
	return &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP6, Safi: gobgpapi.Family_SAFI_UNICAST}
}

func sortedPeers(peers map[string]bgpdaemon.AppliedPeer) []bgpdaemon.AppliedPeer {
	var keys []string
	for key := range peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]bgpdaemon.AppliedPeer, 0, len(keys))
	for _, key := range keys {
		out = append(out, peers[key])
	}
	return out
}
