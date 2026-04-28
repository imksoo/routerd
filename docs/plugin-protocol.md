---
title: Plugin Protocol
slug: /reference/plugin-protocol
---

# Plugin Protocol

routerd plugins are trusted local executables. They extend
resource-specific behavior — observing, planning, applying, and removing —
without baking that logic into the core. There is no remote plugin
registry and no remote plugin install: every plugin is a file on the
router's own filesystem, vetted before install.

A plugin is laid out as a small directory containing a manifest and one
executable:

```text
/usr/local/libexec/routerd/plugins/net.interface/0.1.0/
├── plugin.yaml
└── plugin.sh
```

The manifest declares which routerd resource the plugin serves and which
actions it implements:

```yaml
apiVersion: plugin.routerd.net/v1alpha1
kind: Plugin
metadata:
  name: net.interface
  version: 0.1.0
spec:
  resource:
    apiVersion: net.routerd.net/v1alpha1
    kind: Interface
  runtime:
    executable: plugin.sh
  actions:
    validate: true
    observe: true
    plan: true
    ensure: true
    delete: true
  requirements:
    commands:
      - ip
      - jq
```

## How routerd invokes a plugin

When routerd needs the plugin to act on a resource, it runs the executable
with:

- a JSON document on stdin describing the resource and context,
- a JSON document expected on stdout,
- human-readable diagnostics on stderr,
- action and resource metadata on environment variables.

The plugin is responsible for a single resource at a time. routerd picks
the right plugin by matching `spec.resource` in the manifest against the
resource being processed.

## Actions

Each action maps to a phase of the apply pipeline:

- `validate`: structural and semantic validation of the resource.
- `observe`: read host state related to the resource.
- `plan`: compute the difference between desired and observed state.
- `ensure`: bring the host to desired state.
- `delete`: remove host state owned by the resource.

A plugin manifest only needs to opt into the actions it implements.

## Environment variables

Action and resource metadata are passed as environment variables so the
plugin script does not need to parse them out of stdin:

- `ROUTERD_ACTION`
- `ROUTERD_RESOURCE_API_VERSION`
- `ROUTERD_RESOURCE_KIND`
- `ROUTERD_RESOURCE_NAME`
- `ROUTERD_GENERATION`
- `ROUTERD_RUN_DIR`
- `ROUTERD_STATE_DIR`
- `ROUTERD_DRY_RUN`

The detailed shape of the stdin / stdout JSON documents is being filled in
as the plugin runner solidifies, and will be added to this document along
with each new action that lands in core.

## Log sink plugins

`LogSink` resources with `spec.type: plugin` are a separate, simpler form
of plugin. They are one-way event sinks: routerd executes the configured
trusted local executable once per event.

- stdin: one JSON event object followed by a newline.
- stdout: ignored.
- stderr: human-readable diagnostics.
- Environment variables:
  - `ROUTERD_LOG_LEVEL`
  - `ROUTERD_LOG_ROUTER`
  - `ROUTERD_LOG_COMMAND`

The event payload looks like this:

```json
{
  "timestamp": "2026-04-26T00:00:00Z",
  "level": "info",
  "message": "routerd command completed",
  "router": "lab-router",
  "command": "apply",
  "fields": {
    "phase": "Healthy"
  }
}
```

routerd waits for the executable to exit before considering the event
delivered. The configured `spec.plugin.timeout` bounds that wait, so a
slow or stuck sink cannot hold up apply.
