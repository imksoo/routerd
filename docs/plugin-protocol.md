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
