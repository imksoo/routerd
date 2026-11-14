# ObservabilityPipeline

[Resource configuration reference](../resources.md#observabilitypipeline) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

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

[Example](https://github.com/imksoo/routerd/blob/main/examples/observability-loki.yaml)
