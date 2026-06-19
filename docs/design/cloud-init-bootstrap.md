---
title: Cloud-init bootstrap design
---

# Cloud-init bootstrap design

This note proposes the shared bootstrap contract for routerd nodes on Proxmox
VE, AWS, Azure, and OCI. It supersedes the Alpine/OpenRC shape in PR #546 for
the live ISO path: the current target is the Ubuntu `debootstrap` live ISO with
systemd first boot units.

## Goals

- Keep VM images and the live ISO shared across nodes and providers.
- Put only node identity and bootstrap pointers in user-data.
- Fetch the full `router.yaml` or config bundle from HTTP or object storage.
- Verify fetched config content before installing it.
- Preserve the existing `ROUTERD_CONFIG` config disk flow as the first choice
  for offline or removable-media deployments.
- Avoid putting transport secrets or cloud credentials in cleartext user-data.

## User-data schema

Use a top-level `routerd` object for routerd-specific fields. `hostname` remains
top-level because it is already a common cloud-init convention and is useful even
before routerd starts.

```yaml
#cloud-config
hostname: pve-rt07
routerd:
  node_role: onprem-router
  config_url: https://config.example.net/routerd/pve-rt07/bundle.tar.zst
  config_sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
  transport_secret_ref: vault://routerd/pve-rt07/wireguard
```

Fields:

| Field | Required | Meaning |
| --- | --- | --- |
| `hostname` | Recommended | Node identity applied before routerd starts. |
| `routerd.node_role` | Optional | Role hint such as `onprem-router`, `spine`, `rr`, or `edge`. |
| `routerd.config_url` | Optional | URL for the full routerd config or config bundle. |
| `routerd.config_sha256` | Required when `config_url` is used outside a trusted local network | SHA256 digest of the fetched object. |
| `routerd.transport_secret_ref` | Optional | Pointer to a secret in Vault, cloud secret storage, or an operator-managed location. The secret value itself must not be placed in user-data. |

Compatibility aliases from PR #546 (`config_url`, `config-url`, `configUrl`,
`routerd_config_url`, and matching `config_sha256` spellings) can be accepted by
the reader during migration, but new examples should use the `routerd.*` shape.

## Provider sources

The bootstrap reader should normalize provider-specific data sources into the
same local user-data document:

| Provider | Source | Notes |
| --- | --- | --- |
| PVE | NoCloud config drive with `CIDATA` or `cidata` label | Read `/user-data` first, with OpenStack-style paths as fallback. Works with `qm set --cicustom user=...`. |
| AWS | IMDSv2 `http://169.254.169.254/latest/user-data` | Acquire a session token before reading user-data. |
| Azure | IMDS and wireserver | Use the metadata service for instance identity. If user-data is needed, read the Azure provisioned user-data/custom-data path supported by the image flow. |
| OCI | IMDSv2 `http://169.254.169.254/opc/v2/instance/metadata/` | Use OCI metadata headers and read the user-data/custom metadata value. |

The first implementation for the live ISO should stay lightweight and should not
install the full `cloud-init` package unless a later implementation needs module
compatibility. The live ISO already owns a small systemd first boot path, so a
small reader keeps ISO size and behavior predictable.

## Precedence

At boot, config discovery should be deterministic:

1. Existing `ROUTERD_CONFIG` config disk or USB media.
2. Cloud-init user-data from the current provider.
3. Built-in sample/default config.

Hostname can be applied earlier than full config restore because it is needed for
SSH identity and host-specific config disk paths. A NoCloud `hostname` from
user-data should set `/etc/hostname` and call `hostnamectl set-hostname` before
routerd services start.

When both config disk and cloud-init provide a config URL, config disk wins. The
cloud-init source can still provide hostname if the config disk does not.

## Config bundle

The downloaded object may be either a single `router.yaml` or a bundle archive.
A bundle layout should be explicit and stable:

```text
router.yaml
secrets/
  README.txt
metadata.json
```

`metadata.json` can later carry version, created time, intended node, and
signature metadata. The first implementation only needs a SHA256 check over the
downloaded object before it is installed.

Failure behavior:

- If `config_sha256` is present and does not match, refuse to install the config.
- If fetch fails and no previous config exists, continue with the default config
  and leave a clear boot log message.
- If a previous validated config exists on persistent storage, keep using it.

## Security

- Do not store WireGuard keys, provider credentials, or federation transport
  secrets directly in user-data.
- Treat user-data as node-identifying but not secret.
- Use `config_sha256` for integrity immediately.
- Add signature verification later if config bundles become multi-file release
  artifacts or are fetched over untrusted networks.
- Keep remote plugin registry and remote plugin install out of scope.

## Staged implementation

1. Done: PVE NoCloud `hostname` on the Ubuntu debootstrap ISO.
2. Done for PVE NoCloud: parse user-data and fetch `routerd.config_url` with
   optional `routerd.config_sha256`.
3. Done for the systemd first boot path: config disk precedence, single
   `router.yaml` install, and `.tar.zst` / `.tar.gz` / `.tar` bundle extraction.
4. Add provider readers for AWS, Azure, and OCI IMDS behind the same interface.
5. Add optional persistent validated-config cache and SSH host key bootstrap.
6. Add signature verification and richer status reporting once the bundle format
   stabilizes.

PR #546's useful part is the config pointer and checksum idea. The Alpine
OpenRC-specific implementation should not be carried forward into the current
live ISO; the debootstrap ISO should use the systemd first boot flow.
