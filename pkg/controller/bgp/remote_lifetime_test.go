// SPDX-License-Identifier: BSD-3-Clause

package bgp

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	gobgpapi "github.com/osrg/gobgp/v4/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type lifetimeRPCServer struct {
	gobgpapi.UnimplementedGoBgpServiceServer
	watchClosed chan struct{}
}

func (lifetimeRPCServer) GetBgp(context.Context, *gobgpapi.GetBgpRequest) (*gobgpapi.GetBgpResponse, error) {
	return &gobgpapi.GetBgpResponse{Global: &gobgpapi.Global{Asn: 64512}}, nil
}

func (s lifetimeRPCServer) WatchEvent(_ *gobgpapi.WatchEventRequest, stream grpc.ServerStreamingServer[gobgpapi.WatchEventResponse]) error {
	defer close(s.watchClosed)
	if err := stream.Send(&gobgpapi.WatchEventResponse{}); err != nil {
		return err
	}
	<-stream.Context().Done()
	return stream.Context().Err()
}

// Only the connection establishment uses TCP: the cached client, real RPCs and
// stream lifetime below use the production remote server implementation.
func loopbackRemote(t *testing.T) (*remoteGoBGPServer, <-chan struct{}) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	watchClosed := make(chan struct{})
	gobgpapi.RegisterGoBgpServiceServer(server, lifetimeRPCServer{watchClosed: watchClosed})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = server.Serve(listener)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithBlock())
	if err != nil {
		server.Stop()
		<-done
		t.Fatal(err)
	}
	remote := &remoteGoBGPServer{conn: conn, client: gobgpapi.NewGoBgpServiceClient(conn)}
	t.Cleanup(func() {
		remote.Stop()
		server.Stop()
		<-done
	})
	return remote, watchClosed
}

func TestRemoteWatchCancellationPreservesUnaryConnection(t *testing.T) {
	remote, _ := loopbackRemote(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	streamCtx, stopWatch := context.WithCancel(ctx)
	err := remote.WatchEvent(streamCtx, &gobgpapi.WatchEventRequest{}, func(*gobgpapi.WatchEventResponse) error {
		stopWatch()
		return nil
	})
	if err == nil {
		t.Fatal("canceled stream unexpectedly succeeded")
	}
	reply, err := remote.GetBgp(ctx, &gobgpapi.GetBgpRequest{})
	if err != nil || reply.GetGlobal().GetAsn() != 64512 {
		t.Fatalf("watch cancellation closed the shared unary client: reply=%v err=%v", reply, err)
	}
}

func TestRemoteWatchCallbackErrorReleasesServerSubscription(t *testing.T) {
	remote, closed := loopbackRemote(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	failure := errors.New("current observation failed")
	err := remote.WatchEvent(ctx, &gobgpapi.WatchEventRequest{}, func(*gobgpapi.WatchEventResponse) error { return failure })
	if !errors.Is(err, failure) {
		t.Fatalf("callback error = %v", err)
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("callback return left the native subscription alive")
	}
	if _, err := remote.GetBgp(ctx, &gobgpapi.GetBgpRequest{}); err != nil {
		t.Fatalf("stream cleanup closed the shared connection: %v", err)
	}
}

func TestRemoteStopFencesConcurrentCallsAndJoinsStream(t *testing.T) {
	remote, _ := loopbackRemote(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	watching := make(chan struct{})
	watchDone := make(chan error, 1)
	go func() {
		watchDone <- remote.WatchEvent(ctx, &gobgpapi.WatchEventRequest{}, func(*gobgpapi.WatchEventResponse) error {
			close(watching)
			return nil
		})
	}()
	select {
	case <-watching:
	case <-ctx.Done():
		t.Fatal("stream did not start")
	}
	var calls sync.WaitGroup
	start := make(chan struct{})
	for range 12 {
		calls.Add(1)
		go func() {
			defer calls.Done()
			<-start
			for range 20 {
				_, _ = remote.GetBgp(ctx, &gobgpapi.GetBgpRequest{})
			}
		}()
	}
	close(start)
	remote.Stop()
	calls.Wait()
	select {
	case err := <-watchDone:
		if err == nil {
			t.Fatal("owner stop did not fail the active stream")
		}
	case <-ctx.Done():
		t.Fatal("owner stop left the stream running")
	}
	if _, err := remote.api(context.Background()); err == nil || !strings.Contains(err.Error(), "stopped") {
		t.Fatalf("stopped owner attempted to create another connection: %v", err)
	}
}

func TestBGPControlRequiresCompleteBoundedAcknowledgement(t *testing.T) {
	for _, body := range []string{"{} {}", "null", "{\"unknownIntent\":true}", strings.Repeat(" ", maxBGPControlBodyBytes+1)} {
		response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body))}
		if _, err := readAppliedResponse(response); err == nil {
			t.Fatalf("accepted incomplete/unknown/oversized acknowledgement (%d bytes)", len(body))
		}
	}
	response := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}\n"))}
	if _, err := readAppliedResponse(response); err != nil {
		t.Fatal(err)
	}
}
