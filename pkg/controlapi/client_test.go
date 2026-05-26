// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/apply"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestUnixClientRetriesTransientStartupErrors(t *testing.T) {
	socketPath := filepath.Join(t.TempDir(), "routerd.sock")
	client := NewUnixClient(socketPath)
	client.retryAttempts = 20
	client.retryDelay = 50 * time.Millisecond

	errCh := make(chan error, 1)
	cleanupCh := make(chan func(), 1)
	go func() {
		time.Sleep(150 * time.Millisecond)
		listener, err := net.Listen("unix", socketPath)
		if err != nil {
			errCh <- err
			return
		}
		server := &http.Server{Handler: Handler{
			Status: func(r *http.Request) (*Status, error) {
				status := NewStatus(&apply.Result{Phase: "Healthy", Generation: 7})
				return &status, nil
			},
		}}
		cleanupCh <- func() {
			_ = server.Close()
			_ = listener.Close()
		}
		errCh <- server.Serve(listener)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status, err := client.Status(ctx)
	if err != nil {
		t.Fatalf("Status returned error before delayed socket was ready: %v", err)
	}
	if status.Status.Phase != "Healthy" || status.Status.Generation != 7 {
		t.Fatalf("status = %+v", status.Status)
	}
	select {
	case cleanup := <-cleanupCh:
		cleanup()
	case err := <-errCh:
		t.Fatalf("delayed server failed: %v", err)
	case <-time.After(time.Second):
		t.Fatal("delayed server did not start")
	}
}

func TestClientDoesNotRetryMutatingRequests(t *testing.T) {
	attempts := 0
	client := &Client{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			attempts++
			return nil, syscall.ECONNREFUSED
		})},
		baseURL:       "http://routerd",
		retryAttempts: 5,
		retryDelay:    time.Millisecond,
	}
	req, err := http.NewRequest(http.MethodPost, client.baseURL+Prefix+"/apply", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	_, err = client.doWithRetry(req)
	if err == nil {
		t.Fatal("doWithRetry returned nil error")
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}
