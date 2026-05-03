package lan

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func RecordDHCP4LeaseGranted(ctx context.Context, iface string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.dhcp4.server.lease.granted")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.interface", iface)))
}

func RecordDHCP6LeaseGranted(ctx context.Context, iface, mode string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.dhcp6.server.lease.granted")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.interface", iface), attribute.String("routerd.dhcp6.mode", mode)))
}

func RecordRASent(ctx context.Context, iface string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.lan.ra.sent")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.interface", iface)))
}

func RecordDNSHostLookup(ctx context.Context, scope string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.dns.host.lookups")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.dns.scope", scope)))
}
