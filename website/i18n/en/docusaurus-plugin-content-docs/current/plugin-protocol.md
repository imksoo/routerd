---
title: Plugin protocol
slug: /reference/plugin-protocol
---

# Plugin protocol

routerd plugins are trusted local executables. The plugin mechanism lets you add resource-specific behaviour as a small program on the same host, without modifying the routerd binary.

Remote plugin registration, remote installation, and a public plugin registry are intentionally out of scope.

## Layout

The default install path is:

```text
/usr/local/libexec/routerd/plugins/<name>/
```

Each plugin has a manifest and an executable:

```text
plugin.yaml
bin/<plugin>
```

## Responsibilities

A plugin can take part in:

- Resource validation
- Plan generation
- Host state observation
- Host state application

Operations that mutate network state should be split into testable units. As with the main code base, tests that touch real host networking should run inside isolated network namespaces (see `tests/netns`).

## Current status

The main router features are advanced inside the routerd binary and its managed daemons. The plugin protocol is the safe foundation for site-local extensions; the manifest format and the I/O contract may still change before the protocol is frozen as a stable public surface.
