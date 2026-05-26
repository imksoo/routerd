// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/imksoo/routerd/pkg/version"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/log/global"
	otelmetric "go.opentelemetry.io/otel/metric"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

type Runtime struct {
	ServiceName string
	Enabled     bool
	Logger      *slog.Logger
	Meter       otelmetric.Meter
	Tracer      trace.Tracer

	mu            sync.Mutex
	counters      map[string]otelmetric.Int64Counter
	gauges        map[string]otelmetric.Int64Gauge
	float64Gauges map[string]otelmetric.Float64Gauge
	shutdown      []func(context.Context) error
}

func Setup(ctx context.Context, serviceName string, attrs ...attribute.KeyValue) (*Runtime, error) {
	runtime := &Runtime{
		ServiceName: serviceName,
		Logger:      slog.Default(),
		Meter:       otel.Meter(serviceName),
		Tracer:      otel.Tracer(serviceName),
	}
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" &&
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") == "" &&
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" &&
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		return runtime, nil
	}

	res := resource.NewWithAttributes(semconv.SchemaURL, resourceAttributes(serviceName, attrs...)...)

	if signalEnabled("logs") {
		logExporter, err := otlploggrpc.New(ctx)
		if err != nil {
			return nil, err
		}
		logProvider := sdklog.NewLoggerProvider(
			sdklog.WithResource(res),
			sdklog.WithProcessor(sdklog.NewBatchProcessor(logExporter)),
		)
		global.SetLoggerProvider(logProvider)
		runtime.shutdown = append(runtime.shutdown, logProvider.Shutdown)
		runtime.Logger = otelslog.NewLogger(serviceName, otelslog.WithLoggerProvider(logProvider))
	}

	if signalEnabled("metrics") {
		metricExporter, err := otlpmetricgrpc.New(ctx)
		if err != nil {
			_ = runtime.Shutdown(context.Background())
			return nil, err
		}
		meterProvider := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(30*time.Second))),
		)
		otel.SetMeterProvider(meterProvider)
		runtime.shutdown = append(runtime.shutdown, meterProvider.Shutdown)
		runtime.Meter = meterProvider.Meter(serviceName)
	}

	if signalEnabled("traces") {
		traceExporter, err := otlptracegrpc.New(ctx)
		if err != nil {
			_ = runtime.Shutdown(context.Background())
			return nil, err
		}
		traceProvider := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(traceExporter),
		)
		otel.SetTracerProvider(traceProvider)
		runtime.shutdown = append(runtime.shutdown, traceProvider.Shutdown)
		runtime.Tracer = traceProvider.Tracer(serviceName)
	}
	runtime.Enabled = true
	return runtime, nil
}

func resourceAttributes(serviceName string, attrs ...attribute.KeyValue) []attribute.KeyValue {
	hostName, _ := os.Hostname()
	values := []attribute.KeyValue{
		semconv.ServiceName(serviceName),
		semconv.ServiceVersion(version.Version),
		semconv.OSTypeKey.String(runtime.GOOS),
		attribute.String("routerd.service.name", serviceName),
		attribute.String("routerd.version", version.Version),
		attribute.String("routerd.os", runtime.GOOS),
	}
	if hostName != "" {
		values = append(values, semconv.HostName(hostName))
		values = append(values, attribute.String("routerd.host.role", hostName))
	}
	if namespace := strings.TrimSpace(os.Getenv("OTEL_SERVICE_NAMESPACE")); namespace != "" {
		values = append(values, semconv.ServiceNamespace(namespace))
	}
	values = append(values, parseResourceAttributes(os.Getenv("OTEL_RESOURCE_ATTRIBUTES"))...)
	values = append(values, attrs...)
	return dedupeAttributes(values)
}

func parseResourceAttributes(raw string) []attribute.KeyValue {
	var out []attribute.KeyValue
	for _, field := range strings.Split(raw, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		key, value, ok := strings.Cut(field, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		out = append(out, attribute.String(key, strings.TrimSpace(value)))
	}
	return out
}

func dedupeAttributes(values []attribute.KeyValue) []attribute.KeyValue {
	last := map[attribute.Key]int{}
	out := make([]attribute.KeyValue, 0, len(values))
	for _, value := range values {
		if idx, ok := last[value.Key]; ok {
			out[idx] = value
			continue
		}
		last[value.Key] = len(out)
		out = append(out, value)
	}
	return out
}

func signalEnabled(signal string) bool {
	env := map[string]string{
		"logs":    "OTEL_LOGS_EXPORTER",
		"metrics": "OTEL_METRICS_EXPORTER",
		"traces":  "OTEL_TRACES_EXPORTER",
	}[signal]
	return strings.ToLower(strings.TrimSpace(os.Getenv(env))) != "none"
}

func (r *Runtime) Shutdown(ctx context.Context) error {
	var lastErr error
	for i := len(r.shutdown) - 1; i >= 0; i-- {
		if err := r.shutdown[i](ctx); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (r *Runtime) Counter(name string) otelmetric.Int64Counter {
	if r.Meter == nil {
		r.Meter = otel.Meter(r.ServiceName)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counters == nil {
		r.counters = map[string]otelmetric.Int64Counter{}
	}
	if counter, ok := r.counters[name]; ok {
		return counter
	}
	counter, _ := r.Meter.Int64Counter(name)
	r.counters[name] = counter
	return counter
}

func (r *Runtime) Gauge(name string) otelmetric.Int64Gauge {
	if r.Meter == nil {
		r.Meter = otel.Meter(r.ServiceName)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.gauges == nil {
		r.gauges = map[string]otelmetric.Int64Gauge{}
	}
	if gauge, ok := r.gauges[name]; ok {
		return gauge
	}
	gauge, _ := r.Meter.Int64Gauge(name)
	r.gauges[name] = gauge
	return gauge
}

func (r *Runtime) Float64Gauge(name string) otelmetric.Float64Gauge {
	if r.Meter == nil {
		r.Meter = otel.Meter(r.ServiceName)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.float64Gauges == nil {
		r.float64Gauges = map[string]otelmetric.Float64Gauge{}
	}
	if gauge, ok := r.float64Gauges[name]; ok {
		return gauge
	}
	gauge, _ := r.Meter.Float64Gauge(name)
	r.float64Gauges[name] = gauge
	return gauge
}

func (r *Runtime) Ensure() {
	if r.Logger == nil {
		r.Logger = slog.Default()
	}
	if r.Meter == nil {
		r.Meter = otel.Meter(r.ServiceName)
	}
	if r.Tracer == nil {
		r.Tracer = otel.Tracer(r.ServiceName)
	}
}
