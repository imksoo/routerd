// SPDX-License-Identifier: BSD-3-Clause

package otel

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func Reconcile(ctx context.Context, controller, trigger string, interval time.Duration, fn func(context.Context) error) error {
	ctx, span := otel.Tracer("routerd").Start(ctx, "controller.reconcile")
	span.SetAttributes(
		attribute.String("routerd.controller.name", controller),
		attribute.String("routerd.controller.trigger", trigger),
		attribute.Int64("routerd.controller.interval_ms", interval.Milliseconds()),
	)
	defer span.End()
	start := time.Now()
	err := fn(ctx)
	duration := time.Since(start)
	counter, _ := otel.Meter("routerd").Int64Counter("routerd.controller.reconcile")
	durationHistogram, _ := otel.Meter("routerd").Float64Histogram("routerd.controller.reconcile.duration_ms")
	intervalGauge, _ := otel.Meter("routerd").Int64Gauge("routerd.controller.reconcile.interval_ms")
	attrs := []attribute.KeyValue{
		attribute.String("routerd.controller.name", controller),
		attribute.String("routerd.controller.trigger", trigger),
		attribute.Bool("routerd.controller.error", err != nil),
	}
	counter.Add(ctx, 1, metric.WithAttributes(attrs...))
	durationHistogram.Record(ctx, float64(duration)/float64(time.Millisecond), metric.WithAttributes(attrs...))
	intervalGauge.Record(ctx, interval.Milliseconds(), metric.WithAttributes(attribute.String("routerd.controller.name", controller)))
	if err != nil {
		span.RecordError(err)
	}
	return err
}
