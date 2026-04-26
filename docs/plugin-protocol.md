# Plugin Protocol

routerd plugins are trusted local executables.

routerd invokes a plugin with:

- JSON input on stdin
- JSON output on stdout
- human-readable logs on stderr
- action metadata in environment variables

## Actions

- `validate`
- `observe`
- `plan`
- `ensure`
- `delete`

## Environment Variables

- `ROUTERD_ACTION`
- `ROUTERD_RESOURCE_API_VERSION`
- `ROUTERD_RESOURCE_KIND`
- `ROUTERD_RESOURCE_NAME`
- `ROUTERD_GENERATION`
- `ROUTERD_RUN_DIR`
- `ROUTERD_STATE_DIR`
- `ROUTERD_DRY_RUN`

The full protocol will be implemented and documented as the plugin runner is added.

## Log Sink Plugins

`LogSink` resources with `spec.type: plugin` are one-way event sinks. routerd
executes the configured trusted local executable once per event.

- stdin: one JSON event object followed by a newline
- stdout: ignored
- stderr: human-readable diagnostics
- environment variables:
  - `ROUTERD_LOG_LEVEL`
  - `ROUTERD_LOG_ROUTER`
  - `ROUTERD_LOG_COMMAND`

Event JSON:

```json
{
  "timestamp": "2026-04-26T00:00:00Z",
  "level": "info",
  "message": "routerd command completed",
  "router": "lab-router",
  "command": "reconcile",
  "fields": {
    "phase": "Healthy"
  }
}
```
