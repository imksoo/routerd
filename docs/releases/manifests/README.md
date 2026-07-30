# Release Manifests

Release manifests are machine-readable evidence for release gates.

## Stable release tag manifest

Release tags that are intended to become a stable rollback point should have a
small manifest under this directory.

The manifest is deliberately lightweight. It records the release tag, commit,
artifact identity, real-machine evidence, and known follow-up issues that were
accepted at the time of tagging.

Required fields:

```yaml
release:
  version: vYYYYMMDD.HHMM
  commit: <tag commit sha>
  baseline: <previous stable tag or sha>
  artifact:
    path: <artifact path or release asset name>
    sha256: <sha256 when available>
  checks:
    unitTests: PASS|FAIL|SKIPPED
    schemaChecks: PASS|FAIL|SKIPPED
  fullTopology:
    status: PASS|FAIL|SKIPPED
    convergenceSeconds: <seconds when available>
    matrix: <passed>/<total>
    evidence: <evidence path or URL>
  knownIssues:
    - issue: <number>
      title: <short title>
      severity: P1|P2|P3
```

Keep unrelated release changes in separate commits. In particular, protocol or
demo configuration enablement, such as turning on BFD for CloudEdge SAM demo
peers, should not be mixed with status semantics fixes such as changing
hold-down reporting.

## Environment certification manifest

Environment certification manifests record that PVE/cloud substrate was
inspected, repaired if needed, and certified before routerd product
qualification started.

Schema:

```text
docs/releases/manifests/release-environment-certification.schema.json
```

The manifest is produced by the generic certification scripts in this
repository together with private routerd-labs drivers, then consumed by release
preflight and qualification smoke scripts.

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
      "provider": "pve",
      "result": "pass",
      "checkedAt": "2026-06-28T00:50:00Z"
    },
    {
      "name": "cloud-bootstrap-ssh",
      "component": "cloud",
      "provider": "aws",
      "result": "pass",
      "checkedAt": "2026-06-28T00:55:00Z"
    },
    {
      "name": "azure-bootstrap-ssh",
      "component": "cloud",
      "provider": "azure",
      "result": "pass",
      "checkedAt": "2026-06-28T00:56:00Z"
    },
    {
      "name": "oci-bootstrap-ssh",
      "component": "cloud",
      "provider": "oci",
      "result": "pass",
      "checkedAt": "2026-06-28T00:57:00Z"
    }
  ],
  "repairs": [],
  "run": {
    "schemaVersion": "release-environment-contract/v1",
    "runId": "envcert-20260628T010000Z-routerd-full",
    "environment": "routerd-dev",
    "topology": "sam-full",
    "stateMode": "fresh-fabric-fresh-state",
    "routerdArtifact": {
      "version": "v20260628.0100",
      "commit": "0123456789abcdef0123456789abcdef01234567",
      "path": "/release-evidence/routerd-v20260628.0100-linux-amd64.tar.gz",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
      "target": "linux-amd64"
    },
    "labsCommit": "abcdef0123456789abcdef0123456789abcdef01",
    "providers": [
      {"name": "pve", "profile": "release-lab", "region": "local", "authMode": "ssh-key"},
      {"name": "aws", "profile": "release-lab", "region": "example-1", "authMode": "profile"},
      {"name": "azure", "profile": "release-lab", "region": "example-2", "authMode": "service-principal"},
      {"name": "oci", "profile": "release-lab", "region": "example-3", "authMode": "profile"}
    ],
    "tofu": {
      "workingDirectory": "/release-lab/terraform/envs/default",
      "statePath": "/release-lab/terraform/envs/default/terraform.tfstate",
      "variablesPath": "/release-lab/terraform/envs/default/terraform.tfvars",
      "lockPath": "/release-lab/terraform/envs/default/.terraform.lock.hcl",
      "outputPath": "/release-evidence/tofu-output.json"
    },
    "pve": {
      "node": "release-pve",
      "datastore": "release-store",
      "bootSource": "release-image",
      "underlayBridge": "release-underlay",
      "captureBridge": "release-capture",
      "managementAddressSource": "qga-dhcp",
      "vmids": [1001, 1002, 1003, 1004]
    },
    "lifecycle": {
      "ttl": "75m",
      "heartbeatStale": "5m",
      "cleanupScope": "run-id"
    }
  },
  "notes": "No substrate repair required."
}
```

The paths, profiles, regions, node names, VMIDs, and checksum above are
illustrative. A live manifest is private release evidence unless it has been
sanitized; preflight also verifies that the artifact path exists and its
checksum matches.

Policy:

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
