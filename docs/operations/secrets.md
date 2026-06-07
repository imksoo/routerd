---
title: Secret sources
---

# Secret sources

![Diagram showing secret sources referenced from YAML through file or environment providers, root-owned host storage or USB persistence, validation warnings, and render or apply requiring readable secrets](/img/diagrams/operations-secrets.png)

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
- The default release path for host-local secret files is
  `/usr/local/etc/routerd/secrets/`.
- Use root-owned files with mode `0600`, or an equivalent service-manager
  credential mechanism that exposes a file only to routerd.
- Do not publish rendered keepalived or CARP config from production
  hosts; rendered files contain the resolved secret value.
- `base64: true` is only an encoding convenience for file/env transport. It is
  not encryption.
- `routerd validate` warns when a referenced secret file does not exist yet.
  Render and apply require the source to be readable.

On the live ISO with USB persistence, files under
`/usr/local/etc/routerd/secrets` are copied to `routerd/secrets/` on the
persistence device by `live-persistence.sh save-config` and `flush`, then
restored at boot before routerd applies `router.yaml`. Host-specific
`routerd/hosts/<hostname>/secrets/` and `routerd/hosts/<mac>/secrets/` directories
take precedence over the generic directory.
