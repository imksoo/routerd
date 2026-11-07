// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/render"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/log/global"
	logs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	metrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	traces "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	resourcepb "go.opentelemetry.io/proto/otlp/resource/v1"
	"google.golang.org/grpc"
)

type deliveryLogs struct {
	logs.UnimplementedLogsServiceServer
	received chan *logs.ExportLogsServiceRequest
}

func (r *deliveryLogs) Export(_ context.Context, req *logs.ExportLogsServiceRequest) (*logs.ExportLogsServiceResponse, error) {
	r.received <- req
	return &logs.ExportLogsServiceResponse{}, nil
}

type deliveryMetrics struct {
	metrics.UnimplementedMetricsServiceServer
	received chan *metrics.ExportMetricsServiceRequest
}

func (r *deliveryMetrics) Export(_ context.Context, req *metrics.ExportMetricsServiceRequest) (*metrics.ExportMetricsServiceResponse, error) {
	r.received <- req
	return &metrics.ExportMetricsServiceResponse{}, nil
}

type deliveryTraces struct {
	traces.UnimplementedTraceServiceServer
	received chan *traces.ExportTraceServiceRequest
}

func (r *deliveryTraces) Export(_ context.Context, req *traces.ExportTraceServiceRequest) (*traces.ExportTraceServiceResponse, error) {
	r.received <- req
	return &traces.ExportTraceServiceResponse{}, nil
}

func TestRenderedTelemetryDeliversAllSignals(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := grpc.NewServer()
	defer server.Stop()
	lr := &deliveryLogs{received: make(chan *logs.ExportLogsServiceRequest, 8)}
	mr := &deliveryMetrics{received: make(chan *metrics.ExportMetricsServiceRequest, 8)}
	tr := &deliveryTraces{received: make(chan *traces.ExportTraceServiceRequest, 8)}
	logs.RegisterLogsServiceServer(server, lr)
	metrics.RegisterMetricsServiceServer(server, mr)
	traces.RegisterTraceServiceServer(server, tr)
	go func() { _ = server.Serve(listener) }()
	previousLogs, previousMetrics, previousTraces := global.GetLoggerProvider(), otel.GetMeterProvider(), otel.GetTracerProvider()
	defer func() {
		global.SetLoggerProvider(previousLogs)
		otel.SetMeterProvider(previousMetrics)
		otel.SetTracerProvider(previousTraces)
	}()
	for _, signal := range []string{"LOGS", "METRICS", "TRACES"} {
		for _, suffix := range []string{"ENDPOINT", "HEADERS", "CERTIFICATE", "CLIENT_CERTIFICATE", "CLIENT_KEY", "INSECURE"} {
			t.Setenv("OTEL_EXPORTER_OTLP_"+signal+"_"+suffix, "")
		}
		t.Setenv("OTEL_"+signal+"_EXPORTER", "otlp")
	}
	t.Setenv("ROUTERD_OTLP_TLS_SKIP_VERIFY", "false")
	t.Setenv("OTEL_TRACES_SAMPLER", "always_on")
	router := &api.Router{Spec: api.RouterSpec{Resources: []api.Resource{{TypeMeta: api.TypeMeta{APIVersion: api.ObservabilityAPIVersion, Kind: "Telemetry"}, Metadata: api.ObjectMeta{Name: "delivery"}, Spec: api.TelemetrySpec{OTLP: api.TelemetryOTLPSpec{Endpoint: "http://" + listener.Addr().String(), Insecure: true}, ServiceNamespace: "delivery", Attributes: map[string]string{"site": "test, one"}}}}}}
	env, err := render.TelemetryEnvironment(router)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		t.Setenv(key, value)
	}
	runtime, err := Setup(t.Context(), "delivery-test")
	if err != nil {
		t.Fatal(err)
	}
	runtime.Logger.InfoContext(t.Context(), "delivery-log")
	runtime.Counter("delivery.count").Add(t.Context(), 7)
	_, span := runtime.Tracer.Start(t.Context(), "delivery-span")
	span.End()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := runtime.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case req := <-lr.received:
		found := false
		for _, rs := range req.ResourceLogs {
			checkDeliveredResource(t, rs.Resource)
			for _, ss := range rs.ScopeLogs {
				for _, v := range ss.LogRecords {
					found = found || v.Body.GetStringValue() == "delivery-log"
				}
			}
		}
		if !found {
			t.Fatalf("log body not delivered: %v", req)
		}
	case <-ctx.Done():
		t.Fatal("logs not delivered")
	}
	select {
	case req := <-mr.received:
		found := false
		for _, rs := range req.ResourceMetrics {
			checkDeliveredResource(t, rs.Resource)
			for _, ss := range rs.ScopeMetrics {
				for _, v := range ss.Metrics {
					if v.Name == "delivery.count" {
						for _, p := range v.GetSum().DataPoints {
							found = found || p.GetAsInt() == 7
						}
					}
				}
			}
		}
		if !found {
			t.Fatalf("metric value not delivered: %v", req)
		}
	case <-ctx.Done():
		t.Fatal("metrics not delivered")
	}
	select {
	case req := <-tr.received:
		found := false
		for _, rs := range req.ResourceSpans {
			checkDeliveredResource(t, rs.Resource)
			for _, ss := range rs.ScopeSpans {
				for _, v := range ss.Spans {
					found = found || v.Name == "delivery-span"
				}
			}
		}
		if !found {
			t.Fatalf("span not delivered: %v", req)
		}
	case <-ctx.Done():
		t.Fatal("traces not delivered")
	}
}

func checkDeliveredResource(t *testing.T, resource *resourcepb.Resource) {
	t.Helper()
	values := map[string]string{}
	for _, a := range resource.GetAttributes() {
		values[a.Key] = a.Value.GetStringValue()
	}
	for key, want := range map[string]string{"service.name": "delivery-test", "service.namespace": "delivery", "site": "test, one"} {
		if values[key] != want {
			t.Fatalf("received resource %s=%q, want %q", key, values[key], want)
		}
	}
}
