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
| Version | **v20260914.0729** |
| Status | Current production-recommended stable release |
| Release page | [v20260914.0729](https://github.com/imksoo/routerd/releases/tag/v20260914.0729) |
| Track record | Release CI passed; official ISO rollout reported successful on 10 routers, plus two home routers updated from official archives. See validation scope below. |
| Binary | Statically linked (`CGO_ENABLED=0`), published as fixed-name and versioned archives |

## Validation scope

The operator promoted **v20260914.0729** after official-release rollout reports:

- Ten ISO routers: BGP, runtime doctor, service state and bidirectional SAM ICMP/TCP passed; API HTTP stayed successful during VRRP switching. Evidence: `docs/routerd-v20260914.0729-rollout-log.md`, Forgejo commit `317fb30`.
- Two home routers: VIP, DHCP, DNS, four DS-Lite paths and native nDPI checked; client HTTPS 360/360, doctor fail=0. Evidence: `evidence-20260914-release0729/result.md`.
- [Candidate fault-injection evidence](https://github.com/imksoo/routerd/pull/1252#issuecomment-5660295106) covers isolated Linux netns observation failures and publication history retention.

These are operator-reported results, not a new AWS/Azure/OCI qualification. Initial ISO-update packet loss recovered by the test 15 seconds later; an earlier candidate's single DNS timeout remains unexplained. Do not interpret the milestone as zero packet loss throughout every upgrade. Existing rollback ISOs were retained.

## Install the stable release

Use the fixed tag URL when you want the recommended stable build:

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Versioned archives are also published on the same release page, for example
`routerd-v20260914.0729-linux-amd64.tar.gz`.

## Previous stable milestone

Previous stable **v20260707.1514** passed the historical AWS/Azure/OCI/PVE redundant topology (matrix 56/56, provider convergence 4s, dataplane convergence 567s, cleanup state 0). Those results belong to that release, not this one.

v20260627.1533 was the prior production-recommended stable release. It passed a
cost-bounded AWS/Azure/OCI/PVE single-topology baseline after the PVE ISO
substrate was corrected: convergence 136s, matrix 12/12, all leaf
MobilityPools Ready, provider pending/failed 0, cleanup state 0. It remains a
valid rollback candidate for operators who need that exact milestone, but new
deployments should start with v20260914.0729.

## Known observations

- **The API is still v1alpha1.** A stable milestone means this build is
  production-quality; it does not promise backward-compatible resource schemas.
- **Upgrade configs against the new schema.** Do not rely on migration shims.
  Review the per-release deltas in the [changelog](./changelog.md).
- **`routerctl doctor mgmt` SKIPs when no `ManagementAccess` is declared.**
  This is a live-config choice, not a release defect.

## Install and upgrade

See [Install and upgrade](../install-and-upgrade.md) for the full procedure.
