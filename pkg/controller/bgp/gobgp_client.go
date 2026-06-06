// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/manageddaemon"
)

type remoteGoBGPServer struct {
	daemon manageddaemon.Spec
	conn   *grpc.ClientConn
	client gobgpapi.GobgpApiClient
}

func newRemoteGoBGPServer(daemon manageddaemon.Spec) GoBGPServer {
	return &remoteGoBGPServer{daemon: daemon}
}

func (s *remoteGoBGPServer) Serve() {}

func (s *remoteGoBGPServer) Stop() {
	if s.conn != nil {
		_ = s.conn.Close()
	}
	s.conn = nil
	s.client = nil
}

func (s *remoteGoBGPServer) api(ctx context.Context) (gobgpapi.GobgpApiClient, error) {
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
	s.client = gobgpapi.NewGobgpApiClient(conn)
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
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return bgpdaemon.AppliedConfig{}, errors.New(string(bytes.TrimSpace(data)))
	}
	var config bgpdaemon.AppliedConfig
	if err := json.NewDecoder(resp.Body).Decode(&config); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	return bgpdaemon.Normalize(config), nil
}

func (s *remoteGoBGPServer) SaveAppliedConfig(ctx context.Context, config bgpdaemon.AppliedConfig) error {
	if err := s.daemon.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(bgpdaemon.Normalize(config))
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, "http://routerd-bgp/v1/applied", bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Close = true
	client := s.httpClient()
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(resp.Body)
		return errors.New(string(bytes.TrimSpace(data)))
	}
	return nil
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

func (s *remoteGoBGPServer) StopBgp(ctx context.Context, req *gobgpapi.StopBgpRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.StopBgp(ctx, req)
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

func (s *remoteGoBGPServer) SetPolicies(ctx context.Context, req *gobgpapi.SetPoliciesRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.SetPolicies(ctx, req)
	return err
}

func (s *remoteGoBGPServer) SetPolicyAssignment(ctx context.Context, req *gobgpapi.SetPolicyAssignmentRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.SetPolicyAssignment(ctx, req)
	return err
}

func (s *remoteGoBGPServer) ListDefinedSet(ctx context.Context, req *gobgpapi.ListDefinedSetRequest, fn func(*gobgpapi.DefinedSet)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListDefinedSet(ctx, req)
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
		fn(resp.GetDefinedSet())
	}
}

func (s *remoteGoBGPServer) ListPolicy(ctx context.Context, req *gobgpapi.ListPolicyRequest, fn func(*gobgpapi.Policy)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListPolicy(ctx, req)
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
		fn(resp.GetPolicy())
	}
}

func (s *remoteGoBGPServer) ListPolicyAssignment(ctx context.Context, req *gobgpapi.ListPolicyAssignmentRequest, fn func(*gobgpapi.PolicyAssignment)) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.ListPolicyAssignment(ctx, req)
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
		fn(resp.GetAssignment())
	}
}

func (s *remoteGoBGPServer) AddPath(ctx context.Context, req *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error) {
	client, err := s.api(ctx)
	if err != nil {
		return nil, err
	}
	return client.AddPath(ctx, req)
}

func (s *remoteGoBGPServer) DeletePath(ctx context.Context, req *gobgpapi.DeletePathRequest) error {
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	_, err = client.DeletePath(ctx, req)
	return err
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
	client, err := s.api(ctx)
	if err != nil {
		return err
	}
	stream, err := client.WatchEvent(ctx, req)
	if err != nil {
		s.Stop()
		return err
	}
	for {
		resp, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			s.Stop()
			return err
		}
		if err := fn(resp); err != nil {
			return err
		}
	}
}
