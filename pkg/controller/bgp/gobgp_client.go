// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/manageddaemon"
)

type remoteGoBGPServer struct {
	daemon manageddaemon.Spec
	mu     sync.Mutex
	conn   *grpc.ClientConn
	client gobgpapi.GoBgpServiceClient
	closed bool
}

func newRemoteGoBGPServer(daemon manageddaemon.Spec) GoBGPServer {
	return &remoteGoBGPServer{daemon: daemon}
}

func (s *remoteGoBGPServer) Serve() {}

func (s *remoteGoBGPServer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = nil
	s.client = nil
}

func (s *remoteGoBGPServer) api(ctx context.Context) (gobgpapi.GoBgpServiceClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("managed GoBGP client is stopped")
	}
	if s.client != nil {
		return s.client, nil
	}
	if err := s.daemon.Validate(); err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, s.daemon.UnixTarget(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		return nil, err
	}
	s.conn = conn
	s.client = gobgpapi.NewGoBgpServiceClient(conn)
	return s.client, nil
}

func (s *remoteGoBGPServer) httpClient() *http.Client {
	// Issue #40: this client is called from the BGP controller's periodic
	// reconcile (AppliedConfig + SaveAppliedConfig, roughly every 30 s),
	// dialing /run/routerd/bgp/control.sock on the routerd-bgp daemon.
	// Without DisableKeepAlives the Transport's idle-conn pool kept one
	// Unix socket open per dial until GC, accounting for the +4 fd /
	// minute drift observed on homert02 v20260528.0244 / .0325 after the
	// SQLite ledger leak (#39) was already closed. Mirror the
	// conntrack-observer / dhcpv4-client pattern: disable keep-alives,
	// flag the request Close, and CloseIdleConnections() on return so
	// the connection is gone before the next reconcile tick.
	return &http.Client{Transport: &http.Transport{
		DisableKeepAlives: true,
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: 3 * time.Second}
			return dialer.DialContext(ctx, "unix", s.daemon.ControlSocket())
		},
	}}
}

func (s *remoteGoBGPServer) AppliedConfig(ctx context.Context) (bgpdaemon.AppliedConfig, error) {
	if err := s.daemon.Validate(); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://routerd-bgp/v1/applied", nil)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	req.Close = true
	client := s.httpClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	defer resp.Body.Close()
	return readAppliedResponse(resp)
}

func (s *remoteGoBGPServer) ReconcilePolicy(ctx context.Context, intent bgpdaemon.PolicyIntent) error {
	data, err := json.Marshal(intent)
	if err != nil {
		return err
	}
	_, err = s.writeApplied(ctx, http.MethodPost, "/v1/policy", data)
	return err
}

func (s *remoteGoBGPServer) ReconcileStaticPaths(ctx context.Context, paths []bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, error) {
	data, err := json.Marshal(paths)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	return s.writeApplied(ctx, http.MethodPut, "/v1/static-paths", data)
}

func (s *remoteGoBGPServer) SaveAppliedConfig(ctx context.Context, config bgpdaemon.AppliedConfig) error {
	data, err := json.Marshal(bgpdaemon.Normalize(config))
	if err != nil {
		return err
	}
	_, err = s.writeApplied(ctx, http.MethodPut, "/v1/applied", data)
	return err
}

func (s *remoteGoBGPServer) writeApplied(ctx context.Context, method, path string, data []byte) (bgpdaemon.AppliedConfig, error) {
	if err := s.daemon.Validate(); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	if len(data) > maxBGPControlBodyBytes {
		return bgpdaemon.AppliedConfig{}, errors.New("BGP control request exceeds body limit")
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://routerd-bgp"+path, bytes.NewReader(data))
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	client := s.httpClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	defer resp.Body.Close()
	return readAppliedResponse(resp)
}

const maxBGPControlBodyBytes = 4 << 20

// An acknowledgement is the complete typed response, not just its status line.
func readAppliedResponse(resp *http.Response) (bgpdaemon.AppliedConfig, error) {
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBGPControlBodyBytes+1))
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	if len(data) > maxBGPControlBodyBytes {
		return bgpdaemon.AppliedConfig{}, errors.New("BGP control response exceeds body limit")
	}
	if resp.StatusCode != http.StatusOK {
		return bgpdaemon.AppliedConfig{}, fmt.Errorf("BGP control returned HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(data))
	}
	var config *bgpdaemon.AppliedConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	if config == nil {
		return bgpdaemon.AppliedConfig{}, errors.New("BGP control returned null applied configuration")
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return bgpdaemon.AppliedConfig{}, errors.New("BGP control returned trailing data")
	}
	return bgpdaemon.Normalize(*config), nil
}

func (s *remoteGoBGPServer) GetBgp(ctx context.Context, req *gobgpapi.GetBgpRequest) (*gobgpapi.GetBgpResponse, error) {
	client, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	return client.GetBgp(ctx, req)
}

func (s *remoteGoBGPServer) StartBgp(ctx context.Context, req *gobgpapi.StartBgpRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.StartBgp(ctx, req)
	return err
}

func (s *remoteGoBGPServer) AddPeer(ctx context.Context, req *gobgpapi.AddPeerRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.AddPeer(ctx, req)
	return err
}

func (s *remoteGoBGPServer) UpdatePeer(ctx context.Context, req *gobgpapi.UpdatePeerRequest) (*gobgpapi.UpdatePeerResponse, error) {
	client, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	return client.UpdatePeer(ctx, req)
}

func (s *remoteGoBGPServer) ResetPeer(ctx context.Context, req *gobgpapi.ResetPeerRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.ResetPeer(ctx, req)
	return err
}

func (s *remoteGoBGPServer) DeletePeer(ctx context.Context, req *gobgpapi.DeletePeerRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeletePeer(ctx, req)
	return err
}

func (s *remoteGoBGPServer) ListPeer(ctx context.Context, req *gobgpapi.ListPeerRequest, fn func(*gobgpapi.Peer)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListPeer(ctx, req)
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fn(resp.GetPeer())
	}
}

func (s *remoteGoBGPServer) AddPeerGroup(ctx context.Context, req *gobgpapi.AddPeerGroupRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.AddPeerGroup(ctx, req)
	return err
}

func (s *remoteGoBGPServer) DeletePeerGroup(ctx context.Context, req *gobgpapi.DeletePeerGroupRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeletePeerGroup(ctx, req)
	return err
}

func (s *remoteGoBGPServer) ListPeerGroup(ctx context.Context, req *gobgpapi.ListPeerGroupRequest, fn func(*gobgpapi.PeerGroup)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListPeerGroup(ctx, req)
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fn(resp.GetPeerGroup())
	}
}

func (s *remoteGoBGPServer) AddDynamicNeighbor(ctx context.Context, req *gobgpapi.AddDynamicNeighborRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.AddDynamicNeighbor(ctx, req)
	return err
}

func (s *remoteGoBGPServer) DeleteDynamicNeighbor(ctx context.Context, req *gobgpapi.DeleteDynamicNeighborRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeleteDynamicNeighbor(ctx, req)
	return err
}

func (s *remoteGoBGPServer) ListDynamicNeighbor(ctx context.Context, req *gobgpapi.ListDynamicNeighborRequest, fn func(*gobgpapi.DynamicNeighbor)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListDynamicNeighbor(ctx, req)
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fn(resp.GetDynamicNeighbor())
	}
}

func (s *remoteGoBGPServer) ListPath(ctx context.Context, req *gobgpapi.ListPathRequest, fn func(*gobgpapi.Destination)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListPath(ctx, req)
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		fn(resp.GetDestination())
	}
}

func (s *remoteGoBGPServer) WatchEvent(ctx context.Context, req *gobgpapi.WatchEventRequest, fn func(*gobgpapi.WatchEventResponse) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.WatchEvent(ctx, req)
	if err != nil {
		return err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := fn(resp); err != nil {
			return err
		}
	}
}
