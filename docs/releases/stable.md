---
title: Stable milestone
sidebar_label: Stable milestone
sidebar_position: 0
---

# Stable milestone

routerd ships frequently using the `vYYYYMMDD.HHmm` scheme. From those builds we
pick a **production-recommended** release at each milestone. For a new
deployment, start with the version listed here and pin the release tag in
automation.

## Current recommended release

| Item | Value |
| --- | --- |
| Version | **v20260707.1514** |
| Status | Current production-recommended stable release |
| Release page | [v20260707.1514](https://github.com/imksoo/routerd/releases/tag/v20260707.1514) |
| Track record | Release workflow passed, generated config/control schemas match the website copies, and the tag passed a fresh AWS/Azure/OCI/PVE full topology with redundancy: 8 clients, 8 leaves, 2 AWS route reflectors, matrix 56/56, provider convergence 4s, dataplane convergence 567s, cleanup state 0. |
| Binary | Statically linked (`CGO_ENABLED=0`), published as fixed-name and versioned archives |

## Why v20260707.1514 is recommended

v20260707.1514 is the current stable milestone because it combines the recent
runtime steady-state fixes with a fresh real-machine CloudEdge SAM qualification
run. The accepted run used AWS, Azure, OCI, and PVE with redundant leaves and two
AWS route reflectors. The directed client matrix passed `56/56`, and the
post-run cleanup destroyed all 53 OpenTofu resources with no residual state.

The release is also consistent across repository and website schemas:

- `make check-schema` passed.
- `make check-website-schemas` passed.
- `schemas/routerd-config-v1alpha1.schema.json` is byte-identical to
  `website/static/schemas/routerd-config-v1alpha1.schema.json`.
- Control schema and Control OpenAPI website copies are also byte-identical to
  their canonical repository files.

The evidence archive for the accepted full run is stored outside the repository
at:

```text
/home/imksoo/routerd-labs-archive/evidence/rv202607071514-full-20260707T100026Z/e2e-baseline-awsprofile-retry1/summary.txt
```

## Install the stable release

Use the fixed tag URL when you want the recommended stable build:

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Versioned archives are also published on the same release page, for example
`routerd-v20260707.1514-linux-amd64.tar.gz`.

## Previous stable milestone

v20260627.1533 was the prior production-recommended stable release. It passed a
cost-bounded AWS/Azure/OCI/PVE single-topology baseline after the PVE ISO
substrate was corrected: convergence 136s, matrix 12/12, all leaf
MobilityPools Ready, provider pending/failed 0, cleanup state 0. It remains a
valid rollback candidate for operators who need that exact milestone, but new
deployments should start with v20260707.1514.

## Known observations

- **The API is still v1alpha1.** A stable milestone means this build is
  production-quality; it does not promise backward-compatible resource schemas.
- **Upgrade configs against the new schema.** Do not rely on migration shims.
  Review the per-release deltas in the [changelog](./changelog.md).
- **`routerctl doctor mgmt` SKIPs when no `ManagementAccess` is declared.**
  This is a live-config choice, not a release defect.

## Install and upgrade

See [Install and upgrade](../install-and-upgrade.md) for the full procedure.
