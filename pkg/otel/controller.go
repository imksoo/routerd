package otel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func Reconcile(ctx context.Context, controller string, fn func(context.Context) error) error {
	ctx, span := otel.Tracer("routerd").Start(ctx, "controller.reconcile")
	defer span.End()
	err := fn(ctx)
	counter, _ := otel.Meter("routerd").Int64Counter("routerd.controller.reconcile")
	attrs := []attribute.KeyValue{
		attribute.String("routerd.controller.name", controller),
		attribute.Bool("routerd.controller.error", err != nil),
	}
	counter.Add(ctx, 1, metric.WithAttributes(attrs...))
	if err != nil {
		span.RecordError(err)
	}
	return err
}
