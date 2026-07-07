---
title: 稳定版里程碑
sidebar_label: 稳定版里程碑
sidebar_position: 0
---

# 稳定版里程碑

routerd 以 `vYYYYMMDD.HHmm` 格式频繁发布版本。其中经过评估**可供正式环境使用**的版本，会在每个里程碑时选定为稳定版里程碑。新部署请使用本页所列版本，并在自动化中固定 release tag。

## 当前推荐版本

| 项目 | 内容 |
| --- | --- |
| 版本 | **v20260707.1514** |
| 定位 | 当前推荐稳定版 |
| Release page | [v20260707.1514](https://github.com/imksoo/routerd/releases/tag/v20260707.1514) |
| 运行实绩 | Release workflow 通过；生成的 config/control schema 与 website copy 一致；AWS/Azure/OCI/PVE 冗余 full topology 实机测试通过：8 clients、8 leaves、2 个 AWS RR、matrix 56/56、provider 收敛 4s、dataplane 收敛 567s、cleanup state 0 |
| 二进制 | 静态链接（`CGO_ENABLED=0`），同时发布固定名称和带版本号的 archive |

## 推荐 v20260707.1514 的理由

v20260707.1514 包含近期 steady-state runtime 修复，并通过了新的真实机器 CloudEdge SAM qualification，因此是当前稳定版里程碑。接受的 run 在 AWS、Azure、OCI 和 PVE 上使用冗余 leaf 与两个 AWS route reflector，directed client matrix 为 `56/56` PASS。测试后的 cleanup 销毁了 53 个 OpenTofu resource，state 为 0。

schema 也已确认与发布网站一致：

- `make check-schema` PASS。
- `make check-website-schemas` PASS。
- `schemas/routerd-config-v1alpha1.schema.json` 与 `website/static/schemas/routerd-config-v1alpha1.schema.json` byte 一致。
- Control schema 与 Control OpenAPI 的 website copy 也与 canonical file byte 一致。

接受的 full run evidence summary 保存在 repository 外：

```text
/home/imksoo/routerd-labs-archive/evidence/rv202607071514-full-20260707T100026Z/e2e-baseline-awsprofile-retry1/summary.txt
```

## 安装稳定版

使用推荐稳定版时，请使用固定 tag URL：

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

同一 release page 也发布带版本号的 archive，例如 `routerd-v20260707.1514-linux-amd64.tar.gz`。

## 上一个稳定版

v20260627.1533 是上一个推荐稳定版。它在修正 PVE ISO substrate 后通过了 cost-bounded AWS/Azure/OCI/PVE single-topology baseline：136 秒收敛、matrix 12/12、全部 leaf MobilityPool Ready、provider pending/failed 0、cleanup state 0。需要固定到该里程碑的 operator 仍可将它作为 rollback 候选，但新部署应从 v20260707.1514 开始。

## 已知观测

- **API 仍为 v1alpha1。** 稳定版里程碑表示该 build 达到生产可用品质，但不承诺 resource schema 向后兼容。
- **请按新 schema 检查配置。** 不要依赖 migration shim；请查看[变更记录](./changelog.md)中的每个 release delta。
- **未声明 `ManagementAccess` 的配置中 `routerctl doctor mgmt` 会显示 SKIP。** 这是运行配置的选择，不是 release defect。

## 安装与升级

完整步骤请参阅[安装与升级](../install-and-upgrade.md)。
