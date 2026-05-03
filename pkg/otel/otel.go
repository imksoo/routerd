package otel

import (
	"context"
	"log/slog"
	"os"
	"time"

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

	shutdown []func(context.Context) error
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

	res := resource.NewWithAttributes(semconv.SchemaURL, append([]attribute.KeyValue{
		semconv.ServiceName(serviceName),
	}, attrs...)...)

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
	runtime.Enabled = true
	return runtime, nil
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
	counter, _ := r.Meter.Int64Counter(name)
	return counter
}

func (r *Runtime) Gauge(name string) otelmetric.Int64Gauge {
	if r.Meter == nil {
		r.Meter = otel.Meter(r.ServiceName)
	}
	gauge, _ := r.Meter.Int64Gauge(name)
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
