# ObservabilityPipeline

[Resource configuration reference](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Combines an OpenTelemetry environment declaration for managed services with an independent built-in operational-event log consumer.

## Conditions and references

- OTLP environment selection and built-in log forwarding are separate paths. signals controls the former; logs.enabled controls the latter. Empty log sinks do not imply stdout.
- sampling.rate=0 is promoted to 1. kafka sinks and OTLP certificate-file fields are declarations without corresponding runtime execution in this implementation.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ObservabilityPipeline
  metadata:
    name: "<name>"
  spec:
    attributes: # optional; object
      "<key>": "<string>" # optional; string
    logs: # optional; object
      enabled: true # optional; boolean
      sinks: # optional; array
        - # object
          enabled: true # optional; boolean
          kafka: # optional; object
            brokers: # optional; array
              - "<string>" # string
            topic: "<string>" # optional; string
          labels: # optional; object
            "<key>": "<string>" # optional; string
          loki: # optional; object
            headers: # optional; object
              "<key>": "<string>" # optional; string
            tenant: "<string>" # optional; string
            url: "<string>" # optional; string
          minLevel: "debug" # optional; string; enum=["debug", "info", "warning", "error"]
          name: "<string>" # optional; string
          syslog: # optional; object
            address: "<string>" # optional; string
            facility: "kern" # optional; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
            network: "" # optional; string; enum=["", "unix", "unixgram", "tcp", "udp"]
            tag: "<string>" # optional; string
          type: "stdout" # Required; string; enum=["stdout", "syslog", "loki", "kafka"]
    otlp: # optional; object
      endpoint: "<string>" # optional; string
      headers: # optional; object
        "<key>": "<string>" # optional; string
      insecure: true # optional; boolean
      tls: # optional; object
        caFile: "<string>" # optional; string
        certFile: "<string>" # optional; string
        insecureSkipVerify: true # optional; boolean
        keyFile: "<string>" # optional; string
    sampling: # optional; object
      rate: 0 # optional; number
    serviceNamespace: "<string>" # optional; string
    signals: # optional; array
      - "logs" # string; enum=["logs", "metrics", "traces"]
    when: # optional; object
      state: # optional; object
        "<key>": # optional; object
          contains: "<string>" # optional; string
          equals: "<string>" # optional; string
          exists: true # optional; boolean
          for: "<string>" # optional; string
          in: # optional; array
            - "<string>" # string
          maxAge: "<string>" # optional; string
          status: "set" # optional; string; enum=["set", "unset", "unknown"]
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.serviceNamespace` | OpenTelemetry service namespace, default routerd after trimming whitespace. |
| `spec.attributes` | Resource-attribute map, default empty. Sorted and serialized into OTEL_RESOURCE_ATTRIBUTES as comma-separated key=value entries; values are not escaped by this renderer. |
| `spec.otlp` | Optional OTLP environment settings for generated services. A nonblank endpoint selects this resource as the telemetry environment source. Built-in log sinks operate separately. |
| `spec.otlp.endpoint` | Exporter endpoint, default empty. A nonblank value must parse as a URI and is emitted as OTEL_EXPORTER_OTLP_ENDPOINT. Multiple Telemetry/endpoint-bearing pipeline sources fail environment rendering. |
| `spec.otlp.insecure` | Default false. True sets OTEL_EXPORTER_OTLP_INSECURE=true. tls.insecureSkipVerify=true also sets that same insecure variable. |
| `spec.otlp.headers` | Exporter request-header map, default empty. Serialized as sorted comma-separated key=value entries in OTEL_EXPORTER_OTLP_HEADERS without value escaping. It is not the header map of a Loki sink. |
| `spec.otlp.tls` | Decoded TLS options. Only insecureSkipVerify affects the current environment renderer; the three file fields are not wired into exporter configuration. |
| `spec.otlp.tls.caFile` | CA-file declaration, default empty. The current renderer and built-in exporter do not read it; setting it does not configure a trust store. |
| `spec.otlp.tls.certFile` | Client-certificate declaration, default empty and not consumed by the current OTLP configuration path. |
| `spec.otlp.tls.keyFile` | Client-key declaration, default empty and not consumed. A certFile/keyFile pair here does not enable mutual TLS. |
| `spec.otlp.tls.insecureSkipVerify` | Default false. The current renderer maps true to OTEL_EXPORTER_OTLP_INSECURE=true; it does not configure a TLS client that merely skips certificate checks. |
| `spec.signals` | Default logs/metrics/traces for generated OpenTelemetry environment. Omitted members of a nonempty list get exporter=none. The built-in log consumer ignores this list and is controlled by logs.enabled. |
| `spec.sampling` | Built-in event-log sampling settings, independent of the generated OTLP environment. |
| `spec.sampling.rate` | Must be between 0 and 1. Omitted/zero becomes 1, sending all eligible events. Fractions use a deterministic sequence modulo 1000, not random sampling. Use logs.enabled=false to stop this consumer. |
| `spec.logs` | Built-in database event-log forwarding settings, separate from the OTLP environment. |
| `spec.logs.enabled` | Default true. False stops the built-in log consumer, but does not disable OTLP variables in rendered services. |
| `spec.logs.sinks` | Sink list, default empty: no built-in output destination. There is no implicit stdout sink. Kafka entries are skipped by the exporter even when enabled. |
| `spec.logs.sinks[].name` | Optional sink label, default empty. Not a LogSink resource reference; the current exporter does not use it to select destinations. |
| `spec.logs.sinks[].type` | Required stdout, syslog, loki or kafka. kafka is accepted as a declaration but not executed. Only the selected branch is used. |
| `spec.logs.sinks[].enabled` | Default true. False excludes this sink; kafka is excluded independently of this flag. |
| `spec.logs.sinks[].minLevel` | Minimum severity, default info; accepts debug/info/warning/error. Applied after sampling. |
| `spec.logs.sinks[].labels` | Additional Loki stream labels, default empty. Sink labels override same-name global attributes and built-in labels after key sanitization. They are not used by stdout or syslog. |
| `spec.logs.sinks[].loki` | Settings used only for type=loki. |
| `spec.logs.sinks[].loki.url` | Required for loki, with no default. Use the complete Loki push URL; the exporter does not append a push path. Only HTTP 2xx counts as success. |
| `spec.logs.sinks[].loki.tenant` | Optional tenant ID, default empty. Nonempty values set X-Scope-OrgID. |
| `spec.logs.sinks[].loki.headers` | Additional HTTP headers, default empty. Override matching headers including Content-Type and the tenant header. |
| `spec.logs.sinks[].syslog` | Settings used only for type=syslog. |
| `spec.logs.sinks[].syslog.network` | Transport: unix, unixgram, tcp or udp. Empty delegates to Go syslog local-socket discovery. |
| `spec.logs.sinks[].syslog.address` | Destination socket path or host:port, default empty. Set a suitable address for an explicit transport; validation does not enforce this pairing, and connection errors surface during emission. |
| `spec.logs.sinks[].syslog.tag` | Syslog tag; default routerd. Not a resource reference. |
| `spec.logs.sinks[].syslog.facility` | Default local6. This exporter maps local0–local7; any other supplied facility also falls back to local6, unlike the standalone LogSink syslog implementation. |
| `spec.logs.sinks[].kafka` | Kafka intention settings. They are validated but the built-in exporter skips this sink. |
| `spec.logs.sinks[].kafka.brokers` | For type=kafka, at least one nonblank broker is required. No connections are made by the current exporter. |
| `spec.logs.sinks[].kafka.topic` | Required nonblank topic for type=kafka. Not an EventRule topic or a resource reference; no current Kafka transmission. |
| `spec.when` | Activation condition; omitted means no gate. See the shared condition reference for state/all/any, unknown handling and age predicates. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/render/telemetry.go](https://github.com/imksoo/routerd/blob/main/pkg/render/telemetry.go) · [pkg/observabilitypipeline/exporter.go](https://github.com/imksoo/routerd/blob/main/pkg/observabilitypipeline/exporter.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `signals`

[Example](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
