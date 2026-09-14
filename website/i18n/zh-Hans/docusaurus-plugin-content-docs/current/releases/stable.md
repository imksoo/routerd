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
| 版本 | **v20260914.0729** |
| 定位 | 当前推荐稳定版 |
| Release page | [v20260914.0729](https://github.com/imksoo/routerd/releases/tag/v20260914.0729) |
| 运行实绩 | Release CI 成功；已报告完成 10 台官方 ISO 路由器及两台使用官方归档的家庭路由器升级验证。范围见下文。 |
| 二进制 | 静态链接（`CGO_ENABLED=0`），同时发布固定名称和带版本号的 archive |

## 验证范围

根据运维人员提交的官方版本部署报告，将 **v20260914.0729** 指定为推荐稳定版。

- 10 台 ISO 路由器：BGP、runtime doctor、服务、SAM 双向 ICMP/TCP 正常；VRRP 切换期间 API HTTP 正常。记录：`docs/routerd-v20260914.0729-rollout-log.md`，Forgejo commit `317fb30`。
- 两台家庭路由器：VIP、DHCP、DNS、四条 DS-Lite 路径、native nDPI 正常；HTTPS 360/360，doctor fail=0。记录：`evidence-20260914-release0729/result.md`。
- [候选版本故障注入证据](https://github.com/imksoo/routerd/pull/1252#issuecomment-5660295106)：隔离 Linux netns 内验证观测错误不会丢失发布历史。

这是运维人员报告的结果，不是新的 AWS/Azure/OCI 实机认证。ISO 更新后的初始丢包在 15 秒后的复测中已收敛；较早候选版本的一次 DNS 超时仍未查明原因。不能据此声称整个升级过程零丢包。旧 ISO 已保留用于回退。

## 安装稳定版

使用推荐稳定版时，请使用固定 tag URL：

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

同一 release page 也发布带版本号的 archive，例如 `routerd-v20260914.0729-linux-amd64.tar.gz`。

## 上一个稳定版

前一稳定版 **v20260707.1514** 的历史 AWS/Azure/OCI/PVE 冗余测试为 matrix 56/56、provider 收敛 4s、dataplane 收敛 567s、cleanup state 0。这些结果属于旧版，不是本版的新测试。

v20260627.1533 是上一个推荐稳定版。它在修正 PVE ISO substrate 后通过了 cost-bounded AWS/Azure/OCI/PVE single-topology baseline：136 秒收敛、matrix 12/12、全部 leaf MobilityPool Ready、provider pending/failed 0、cleanup state 0。需要固定到该里程碑的 operator 仍可将它作为 rollback 候选，但新部署应从 v20260914.0729 开始。

## 已知观测

- **API 仍为 v1alpha1。** 稳定版里程碑表示该 build 达到生产可用品质，但不承诺 resource schema 向后兼容。
- **请按新 schema 检查配置。** 不要依赖 migration shim；请查看[变更记录](./changelog.md)中的每个 release delta。
- **未声明 `ManagementAccess` 的配置中 `routerctl doctor mgmt` 会显示 SKIP。** 这是运行配置的选择，不是 release defect。

## 安装与升级

完整步骤请参阅[安装与升级](../install-and-upgrade.md)。
