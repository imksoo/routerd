// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"io"
	"time"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"routerd/pkg/manageddaemon"
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
