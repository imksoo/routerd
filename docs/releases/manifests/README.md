# Release Manifests

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
