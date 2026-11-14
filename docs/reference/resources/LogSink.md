# LogSink

[Resource configuration reference](../resources.md#logsink) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

Defines an operational event-log destination selected by its type. It is separate from ObservabilityPipeline log sinks.

## Conditions and references

- type selects one implementation. Only that branch is used; other branch objects are not mutually rejected or merged. For otlp, exactly one of telemetryRef and endpoint is required by validation, but the current OTLPSink.Emit does not send events.
- The journald branch writes through Go log.Printf; it relies on the process log destination and service manager to reach journald. It is not a direct journald API client.

## Configuration syntax

This is explanatory notation, not YAML to copy. Unbracketed fields are required within their parent; `[ ... ]` marks fields that can be omitted, subject to the conditions in the table. `<type>` is a placeholder, and `(a | b)` lists alternatives. Optional parents may contain required children. Conditional requirements, exclusions and runtime defaults are described below. Use the linked YAML example for an ordinary configuration.

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: LogSink
  metadata:
    name: "<name>"
  spec:
    [ enabled: <boolean> ]
    [ file:
        [ name: <string> ]
    ]
    [ journald:
        [ identifier: <string> ]
    ]
    [ minLevel: ("debug" | "info" | "warning" | "error") ]
    [ otlp:
        [ endpoint: <string> ]
        [ telemetryRef: <string> ]
    ]
    [ syslog:
        [ address: <string> ]
        [ facility: ("kern" | "user" | "mail" | "daemon" | "auth" | "syslog" | "lpr" | "news" | "uucp" | "cron" | "authpriv" | "ftp" | "local0" | "local1" | "local2" | "local3" | "local4" | "local5" | "local6" | "local7") ]
        [ network: ("" | "unix" | "unixgram" | "tcp" | "udp") ]
        [ tag: <string> ]
    ]
    type: ("syslog" | "otlp" | "webhook" | "file" | "journald")
    [ webhook:
        [ headers:
            [ <name>: <string> ]
        ]
        [ timeout: <string> ]
        [ url: <string> ]
    ]
```

## Field meaning and default

Required children of optional objects apply only when their parent is used. Other exact conditions are in the explanation; bracketed alternatives may still require one choice.

| YAML | Presence | Type / constraints | Field meaning and default |
| --- | --- | --- | --- |
| `spec.type` | **Required** | string; enum=["syslog", "otlp", "webhook", "file", "journald"] | Required destination selector: syslog, otlp, webhook, file or journald. No default. |
| `spec.enabled` | *Optional* | boolean | Whether the logger selects this sink; default true. False skips it even when referenced by LogRetention. |
| `spec.minLevel` | *Optional* | string; enum=["debug", "info", "warning", "error"] | Minimum event severity, default info. Accepts debug, info, warning or error; lower-severity events are dropped. |
| `spec.syslog` | *Optional* | object | Settings used only with type=syslog. Empty settings use local syslog, local6 and tag routerd. |
| `spec.syslog.network` | *Optional* | string; enum=["", "unix", "unixgram", "tcp", "udp"] | Transport: unix, unixgram, tcp or udp. Empty delegates to Go syslog local-socket discovery. |
| `spec.syslog.address` | *Optional* | string | Destination socket path or host:port, default empty. Set a suitable address for an explicit transport; validation does not enforce this pairing, and connection errors surface during emission. |
| `spec.syslog.facility` | *Optional* | string; enum=["kern", "user", "mail", "daemon", "auth", "syslog", "lpr", "news", "uucp", "cron", "authpriv", "ftp", "local0", "local1", "local2", "local3", "local4", "local5", "local6", "local7"] | Syslog facility, default local6. Accepted values are listed in the YAML enum. |
| `spec.syslog.tag` | *Optional* | string | Syslog tag; default routerd. Not a resource reference. |
| `spec.otlp` | **Conditionally required** | object | OTLP declaration. Exactly one of telemetryRef and endpoint is required for type=otlp; actual transmission is not implemented in this sink. |
| `spec.otlp.telemetryRef` | **Conditionally required** | string | Telemetry/name or plain Telemetry resource name; mutually exclusive with endpoint, default empty. Router-wide validation checks the target exists, even if another type branch is selected. The current OTLPSink does not use it to send. |
| `spec.otlp.endpoint` | **Conditionally required** | string | Intended endpoint URI, mutually exclusive with telemetryRef; no default. URI syntax is validated, but this LogSink does not create an exporter. |
| `spec.webhook` | **Conditionally required** | object | JSON HTTP POST destination settings, used only for type=webhook. |
| `spec.webhook.url` | **Conditionally required** | string | Required when type=webhook, no default. Use an absolute HTTP(S) URL; the validator only checks URI syntax. Only HTTP 2xx counts as successful delivery. |
| `spec.webhook.headers` | *Optional* | object | Additional request headers, default empty. Entries override headers with the same name, including the default Content-Type: application/json. |
| `spec.webhook.timeout` | *Optional* | string | HTTP request timeout, default 5s. Uses Go duration syntax. The validator accepts zero and negative durations; HTTP client values at or below zero mean no timeout. |
| `spec.file` | *Optional* | object | File destination settings for type=file. Output is append-only JSON Lines. |
| `spec.file.name` | *Optional* | string | Logical filename, default routerd-events. The path is /var/lib/routerd/logs/<name>.jsonl; separators, NUL and line breaks are forbidden. It is not an arbitrary filesystem path. |
| `spec.journald` | *Optional* | object | Settings for type=journald; output goes through the process logger. |
| `spec.journald.identifier` | *Optional* | string | Prefix added to the process log message, default routerd. It is not a direct assignment of the journald SYSLOG_IDENTIFIER field. |

Implementation: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/eventlog/eventlog.go](https://github.com/imksoo/routerd/blob/main/pkg/eventlog/eventlog.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## Referenceable status fields

The ordinary status-reference validator accepts these field names for this kind. This allowlist does not guarantee that every field is currently populated; inspect runtime status. These are outputs, not YAML spec fields.

`phase`, `type`
