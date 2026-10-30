// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

type tlsTraceReceiver struct {
	collectortrace.UnimplementedTraceServiceServer
	received chan bool
}

func (s *tlsTraceReceiver) Export(ctx context.Context, request *collectortrace.ExportTraceServiceRequest) (*collectortrace.ExportTraceServiceResponse, error) {
	remote, ok := peer.FromContext(ctx)
	secure := false
	if ok {
		if tlsInfo, ok := remote.AuthInfo.(credentials.TLSInfo); ok {
			secure = len(tlsInfo.State.PeerCertificates) > 0
		}
	}
	select {
	case s.received <- secure && len(request.ResourceSpans) > 0:
	default:
	}
	return &collectortrace.ExportTraceServiceResponse{}, nil
}

func TestOTLPSkipVerifyRetainsTLSOnActualExporter(t *testing.T) {
	certificate := httptest.NewTLSServer(nil)
	defer certificate.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, Certificates: certificate.TLS.Certificates, ClientAuth: tls.RequireAnyClientCert})))
	receiver := &tlsTraceReceiver{received: make(chan bool, 1)}
	collectortrace.RegisterTraceServiceServer(server, receiver)
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://"+listener.Addr().String())
	for _, signal := range []string{"LOGS", "METRICS", "TRACES"} {
		t.Setenv("OTEL_EXPORTER_OTLP_"+signal+"_ENDPOINT", "")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_INSECURE", "false")
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "client.crt"), filepath.Join(dir, "client.key")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.TLS.Certificates[0].Certificate[0]}), 0600); err != nil {
		t.Fatal(err)
	}
	key, err := x509.MarshalPKCS8PrivateKey(certificate.TLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", certPath)
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_KEY", keyPath)
	t.Setenv("OTEL_LOGS_EXPORTER", "none")
	t.Setenv("OTEL_METRICS_EXPORTER", "none")
	t.Setenv("OTEL_TRACES_EXPORTER", "otlp")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	t.Setenv("ROUTERD_OTLP_TLS_SKIP_VERIFY", "true")
	previous := otel.GetTracerProvider()
	defer otel.SetTracerProvider(previous)
	runtime, err := Setup(t.Context(), "tls-export-test")
	if err != nil {
		t.Fatal(err)
	}
	_, span := runtime.Tracer.Start(t.Context(), "encrypted-export")
	if !span.IsRecording() {
		t.Fatal("test span was not recording")
	}
	span.End()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case secure := <-receiver.received:
		if !secure {
			t.Fatal("skip verification disabled TLS or lost spans")
		}
	default:
		t.Fatal("actual exporter did not send through self-signed TLS")
	}
}

func TestOTLPSkipVerifyCannotDropConfiguredClientIdentity(t *testing.T) {
	t.Setenv("ROUTERD_OTLP_TLS_SKIP_VERIFY", "true")
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE", "/missing/client.crt")
	t.Setenv("OTEL_EXPORTER_OTLP_CLIENT_KEY", "/missing/client.key")
	if _, err := otlpTLSCredentials("traces"); err == nil {
		t.Fatal("missing mTLS identity silently omitted while disabling server verification")
	}
}
