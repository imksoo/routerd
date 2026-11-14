# LogSink

[Resource configuration reference](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Defines an operational event-log destination selected by its type. It is separate from ObservabilityPipeline log sinks.

## Conditions and references

- type selects one implementation. Only that branch is used; other branch objects are not mutually rejected or merged. For otlp, exactly one of telemetryRef and endpoint is required by validation, but the current OTLPSink.Emit does not send events.
- The journald branch writes through Go log.Printf; it relies on the process log destination and service manager to reach journald. It is not a direct journald API client.

## YAML definition

All fields are shown as a structural reference, not a configuration to apply unchanged. Replace placeholders, omit unused options, and select compatible alternatives. Shown values illustrate types, not defaults; defaults and runtime conditions are described in the field explanations below. Required markers come from JSON Schema and apply inside the parent object; semantic validation can require additional fields or references.

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    enabled: true # optional; boolean
    file: # optional; object
      name: "<string>" # optional; string
    journald: # optional; object
      identifier: "<string>" # optional; string
    minLevel: "debug" # optional; string; enum=["debug", "info", "warning", "error"]
    otlp: # optional; object
      endpoint: "<string>" # optional; string
      telemetryRef: "<string>" # optional; string
    syslog: # optional; object
      address: "<string>" # optional; string
      facility: "kern" # optional; string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"]
      network: "" # optional; string; enum=["", "unix", "unixgram", "tcp", "udp"]
      tag: "<string>" # optional; string
    type: "syslog" # Required; string; enum=["syslog", "otlp", "webhook", "file", "journald"]
    webhook: # optional; object
      headers: # optional; object
        "<key>": "<string>" # optional; string
      timeout: "<string>" # optional; string
      url: "<string>" # optional; string
```

## Field meaning and default

| YAML | Field meaning and default |
| --- | --- |
| `spec.type` | Required destination selector: syslog, otlp, webhook, file or journald. No default. |
| `spec.enabled` | Whether the logger selects this sink; default true. False skips it even when referenced by LogRetention. |
| `spec.minLevel` | Minimum event severity, default info. Accepts debug, info, warning or error; lower-severity events are dropped. |
| `spec.syslog` | Settings used only with type=syslog. Empty settings use local syslog, local6 and tag routerd. |
| `spec.syslog.network` | Transport: unix, unixgram, tcp or udp. Empty delegates to Go syslog local-socket discovery. |
| `spec.syslog.address` | Destination socket path or host:port, default empty. Set a suitable address for an explicit transport; validation does not enforce this pairing, and connection errors surface during emission. |
| `spec.syslog.facility` | Syslog facility, default local6. Accepted values are listed in the YAML enum. |
| `spec.syslog.tag` | Syslog tag; default routerd. Not a resource reference. |
| `spec.otlp` | OTLP declaration. Exactly one of telemetryRef and endpoint is required for type=otlp; actual transmission is not implemented in this sink. |
| `spec.otlp.telemetryRef` | Intended Telemetry reference; mutually exclusive with endpoint and default empty. The current implementation checks only that a value is present; it does not resolve a Telemetry target or send through it. |
| `spec.otlp.endpoint` | Intended endpoint URI, mutually exclusive with telemetryRef; no default. URI syntax is validated, but this LogSink does not create an exporter. |
| `spec.webhook` | JSON HTTP POST destination settings, used only for type=webhook. |
| `spec.webhook.url` | Required when type=webhook, no default. Use an absolute HTTP(S) URL; the validator only checks URI syntax. Only HTTP 2xx counts as successful delivery. |
| `spec.webhook.headers` | Additional request headers, default empty. Entries override headers with the same name, including the default Content-Type: application/json. |
| `spec.webhook.timeout` | HTTP request timeout, default 5s. Uses Go duration syntax. The validator accepts zero and negative durations; HTTP client values at or below zero mean no timeout. |
| `spec.file` | File destination settings for type=file. Output is append-only JSON Lines. |
| `spec.file.name` | Logical filename, default routerd-events. The path is /var/lib/routerd/logs/<name>.jsonl; separators, NUL and line breaks are forbidden. It is not an arbitrary filesystem path. |
| `spec.journald` | Settings for type=journald; output goes through the process logger. |
| `spec.journald.identifier` | Prefix added to the process log message, default routerd. It is not a direct assignment of the journald SYSLOG_IDENTIFIER field. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go)
