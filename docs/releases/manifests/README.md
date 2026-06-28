# Release manifests

Release manifests are machine-readable evidence for release gates. The first
manifest type is environment certification: it records that PVE/cloud substrate
was inspected, repaired if needed, and certified before routerd product
qualification started.

## Environment certification manifest

Schema:

```text
docs/releases/manifests/release-environment-certification.schema.json
```

The manifest is produced by routerd-labs certification scripts and consumed by
release preflight and qualification smoke scripts.

Minimum example:

```json
{
  "schemaVersion": "release-environment-certification/v1",
  "manifestId": "envcert-20260628T010000Z-routerd-full",
  "environment": "routerd-dev",
  "topology": "sam-full",
  "status": "pass",
  "issuedAt": "2026-06-28T01:00:00Z",
  "expiresAt": "2026-06-29T01:00:00Z",
  "routerdCommit": "0123456789abcdef0123456789abcdef01234567",
  "labsCommit": "abcdef0123456789abcdef0123456789abcdef01",
  "providers": ["pve", "aws", "azure", "oci"],
  "certifiers": [
    {
      "name": "certify-pve-substrate.sh",
      "version": "git:abcdef0",
      "result": "pass"
    },
    {
      "name": "certify-cloud-substrate.sh",
      "version": "git:abcdef0",
      "result": "pass"
    }
  ],
  "checks": [
    {
      "name": "pve-qga-ready",
      "component": "pve",
      "result": "pass",
      "checkedAt": "2026-06-28T00:50:00Z"
    },
    {
      "name": "cloud-bootstrap-ssh",
      "component": "cloud",
      "provider": "aws",
      "result": "pass",
      "checkedAt": "2026-06-28T00:55:00Z"
    }
  ],
  "repairs": [],
  "notes": "No substrate repair required."
}
```

## Policy

- A release qualification run must reference exactly one passing, unexpired
  certification manifest.
- `status: pass` means release qualification may start.
- `status: fail`, `blocked`, or `expired` means qualification must not start.
- Certification failure is an infra failure, not a routerd product failure.
- Product failure can only be assigned after preflight accepts the certification
  manifest.
- Do not commit secrets, raw provider debug logs, private keys, or provider
  resource IDs that are not necessary for release evidence.

See:

- `docs/operations/release-environment-certification.md`
- `docs/operations/release-qualification-policy.md`
