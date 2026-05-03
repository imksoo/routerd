package lan

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func RecordDHCPv4LeaseGranted(ctx context.Context, iface string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.dhcpv4.server.lease.granted")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.interface", iface)))
}

func RecordDHCPv6LeaseGranted(ctx context.Context, iface, mode string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.dhcpv6.server.lease.granted")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.interface", iface), attribute.String("routerd.dhcpv6.mode", mode)))
}

func RecordRASent(ctx context.Context, iface string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.lan.ra.sent")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.interface", iface)))
}

func RecordDNSHostLookup(ctx context.Context, scope string) {
	counter, _ := otel.Meter("routerd.lan").Int64Counter("routerd.dns.host.lookups")
	counter.Add(ctx, 1, metric.WithAttributes(attribute.String("routerd.dns.scope", scope)))
}
