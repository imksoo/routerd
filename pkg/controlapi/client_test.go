// SPDX-License-Identifier: BSD-3-Clause

package controlapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestHTTPClientWithTLSPresentsClientCertificate(t *testing.T) {
	dir := t.TempDir()
	caCert, caKey, caPEM := testCertificateAuthority(t)
	serverCertPEM, serverKeyPEM := testSignedCertificate(t, caCert, caKey, "routerd-server", true)
	clientCertPEM, clientKeyPEM := testSignedCertificate(t, caCert, caKey, "routerd-client", false)
	caFile := writeTestPEM(t, dir, "ca.pem", caPEM)
	clientCertFile := writeTestPEM(t, dir, "client.crt", clientCertPEM)
	clientKeyFile := writeTestPEM(t, dir, "client.key", clientKeyPEM)
	serverCert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("append CA")
	}
	server := httptest.NewUnstartedServer(Handler{
		Status: func(r *http.Request) (*Status, error) {
			if r.TLS == nil || len(r.TLS.PeerCertificates) != 1 {
				t.Fatalf("peer certificates = %#v", r.TLS)
			}
			return &Status{TypeMeta: TypeMeta{APIVersion: APIVersion, Kind: "Status"}, Metadata: ObjectMeta{Name: "routerd"}, Status: StatusStatus{Phase: "Healthy"}}, nil
		},
	})
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS12,
	}
	server.StartTLS()
	defer server.Close()

	client, err := NewHTTPClientWithTLS(server.URL, TLSOptions{
		CAFile:     caFile,
		CertFile:   clientCertFile,
		KeyFile:    clientKeyFile,
		ServerName: "127.0.0.1",
	})
	if err != nil {
		t.Fatalf("NewHTTPClientWithTLS: %v", err)
	}
	status, err := client.Status(context.Background())
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if status.Status.Phase != "Healthy" {
		t.Fatalf("status = %#v", status)
	}
}

func testCertificateAuthority(t *testing.T) (*x509.Certificate, *rsa.PrivateKey, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "routerd-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tmpl, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func testSignedCertificate(t *testing.T, ca *x509.Certificate, caKey *rsa.PrivateKey, commonName string, server bool) ([]byte, []byte) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	usage := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	if server {
		usage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  usage,
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: keyDER})
}

func writeTestPEM(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
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
