// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
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

func TestHTTPClientWithBearerTokenSetsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	client := NewHTTPClient("http://routerd.test").WithBearerToken(" test-token \n")
	client.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		gotAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{"apiVersion":"control.routerd.net/v1alpha1","kind":"Status","metadata":{"name":"routerd"},"status":{"phase":"Healthy","currentError":false}}`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	if _, err := client.Status(context.Background()); err != nil {
		t.Fatalf("Status: %v", err)
	}
	if gotAuth != "Bearer test-token" {
		t.Fatalf("Authorization = %q, want bearer token", gotAuth)
	}
}

func TestClientRuntimeDecodesResponse(t *testing.T) {
	want := NewRuntimeStats()
	want.HeapAllocBytes = 7 * 1024 * 1024
	want.NumGoroutine = 21
	want.OpenFDs = 9
	want.MaxFDs = 1024
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var gotPath string
	client := &Client{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(payload))),
				Header:     make(http.Header),
			}, nil
		})},
		baseURL:       "http://routerd",
		retryAttempts: 1,
		retryDelay:    time.Millisecond,
	}
	stats, err := client.Runtime(context.Background())
	if err != nil {
		t.Fatalf("Runtime: %v", err)
	}
	if gotPath != Prefix+"/runtime" {
		t.Fatalf("requested path = %q", gotPath)
	}
	if stats.NumGoroutine != 21 || stats.OpenFDs != 9 || stats.MaxFDs != 1024 || stats.HeapAllocBytes != want.HeapAllocBytes {
		t.Fatalf("stats = %#v", stats)
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

func TestClientGetSAMRRSet(t *testing.T) {
	want := NewSAMRRSetGetResult("pve-rrs", api.Resource{
		TypeMeta: api.TypeMeta{APIVersion: api.MobilityAPIVersion, Kind: "SAMRRSet"},
		Metadata: api.ObjectMeta{Name: "pve-rrs"},
	})
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var gotPath, gotClaim string
	client := &Client{
		httpClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotPath = req.URL.Path
			gotClaim = req.URL.Query().Get("claim")
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(string(payload))),
				Header:     make(http.Header),
			}, nil
		})},
		baseURL:       "http://routerd",
		retryAttempts: 1,
		retryDelay:    time.Millisecond,
	}
	result, err := client.GetSAMRRSet(context.Background(), SAMRRSetGetRequest{Name: "pve-rrs", ClaimRef: "SAMEnrollmentClaim/pve-leaf-a"})
	if err != nil {
		t.Fatalf("GetSAMRRSet: %v", err)
	}
	if gotPath != Prefix+"/sam-rrsets/pve-rrs" || gotClaim != "SAMEnrollmentClaim/pve-leaf-a" {
		t.Fatalf("request path/query = %q claim=%q", gotPath, gotClaim)
	}
	if result.RRSet.Kind != "SAMRRSet" || result.RRSet.Metadata.Name != "pve-rrs" {
		t.Fatalf("result = %#v", result)
	}
}
