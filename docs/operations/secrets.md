---
title: Secret sources
---

# Secret sources

routerd supports file and environment secret sources for BGP peer passwords and
VRRP/CARP authentication. Prefer these fields over inline `password` or
`authentication` values:

```yaml
passwordFrom:
  file: /usr/local/etc/routerd/secrets/bgp-password
  base64: false
```

```yaml
authenticationFrom:
  env: ROUTERD_VRRP_AUTH
```

Operational guidance:

- Keep secret files outside Git-managed config directories.
- Use root-owned files with mode `0600`, or an equivalent service-manager
  credential mechanism that exposes a file only to routerd.
- Do not publish rendered keepalived or CARP config from production
  hosts; rendered files contain the resolved secret value.
- `base64: true` is only an encoding convenience for file/env transport. It is
  not encryption.
- `routerd validate` warns when a referenced secret file does not exist yet.
  Render and apply require the source to be readable.
