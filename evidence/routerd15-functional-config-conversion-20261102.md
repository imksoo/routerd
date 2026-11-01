# routerd 1.5 functional configuration conversion

## Result

A routerd 1.5 configuration candidate represents the intended behavior of 109
resources from the isolated source configuration with 108 resources. The one
resource-count difference is deliberate: the former OTLP `LogSink` intent is
represented by the retained `Telemetry` resource instead of a second OTLP
configuration path.

The candidate SHA-256 is
`a9679817400480c11830385d180938342095d84017a2930ff124280396d9f545`.
The Linux binary built from
`d9623bbd867d450ff8d1be03c59222592dbf43ad` accepted it:

```text
routerd v20261027.1620 (d9623bbd)
config is valid
```

## Conversion boundaries

This is a standalone 1.5 configuration. It does not use an old-format reader,
database import, handoff record, dual write, or in-place upgrade path.

The candidate retains configuration intent for interfaces, addresses and routes,
DHCPv4/DHCPv6, DNS, NAT, firewall policy, BGP, DS-Lite, IPv6 delegation and
router advertisements, high-availability resources, observability, management,
and service integrations. Test-only endpoints, addresses, and credentials remain
test inputs and must be replaced for each target topology.

## Scope limits

Schema validation confirms that the current routerd parser accepts the converted
configuration. It does not prove every resource can be applied simultaneously
on one fixture or that every protocol path interoperates with an independent
peer. Those remain feature-specific Linux qualification work.
