---
title: 安装与升级
---

# 安装与升级

![routerd 从发布归档安装、检查配置、dry-run，到启动服务的流程](/img/diagrams/install-and-upgrade.png)

本页采用 Ubuntu Server + systemd 的入门路径。请在隔离 VM 或有本地控制台的备用主机上完成第一次安装；它不适合在唯一的生产网关上远程试错。

## 1. 下载并安装发布归档

在 [GitHub Releases](https://github.com/imksoo/routerd/releases) 选择与 CPU 架构相符的归档。下面使用当前推荐的稳定里程碑 [v20260914.0729](https://github.com/imksoo/routerd/releases/tag/v20260914.0729)；[稳定版页面](./releases/stable.md)是这个推荐的唯一来源。示例为 Linux amd64；arm64 请把文件名改为 `linux-arm64`。

```bash
RELEASE=v20260914.0729
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

归档带有程序、服务模板、示例配置和安装脚本；路由器主机不需要 Go 或 Makefile。安装脚本会安装或检查常用运行时依赖，并把程序放在 `/usr/local/sbin`。它会写入 `/usr/local/etc/routerd/router.yaml.sample`，但不会覆盖已有的 `/usr/local/etc/routerd/router.yaml`。全新安装没有配置时，服务不会自动启动。

确认安装：

```bash
routerd --version
routerd --help
```

## 2. 先准备配置，再启动服务

按[安全起步](./tutorials/getting-started.md)完成离线检查，再按
[第一台实验路由器](./tutorials/first-router.md)准备完整 DHCP/NAT 配置、启动服务、
检查状态并测试客户端。`router.yaml.sample` 是包含 ISP 特定设置的较大示例，
不能只改接口名就当成通用默认值。普通全新 `./install.sh` 只安装文件，不会启动服务。

## 升级

升级会保留当前 routerd 生成的 `routerd.service`，包括配置中的 Capability 和 Environment。
`Managed by routerd` 注释本身不表示旧格式；含已移除的 `--controller-chain` 参数的 unit 会被替换。
运行时配置代际切换会保留配置未变的受管理 DHCP 客户端。
VRRP `gracefulActivation` 控制 VIP 的初次发布；同一地址、接口已有 Ready/advertised MASTER
记录时，暂时的 readiness 下降不会撤销 VIP。只有地址存在而没有发布记录时仍需通过初始检查。
降为 BACKUP/FAULT 时仍会撤销 VIP。
成功发布历史与当前观测状态分开保存，观测错误不会清除历史。
降级、确认 VIP 不存在或发布失败会使历史失效；不同地址或接口不能沿用历史。

下载新版本、校验哈希、解压后再次运行同一个安装脚本即可：

```bash
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

安装程序会保留已有配置和状态。升级前先备份自己的 `router.yaml`；如果 `routerd.service` 已在运行，安装程序可能会重启它以换用新程序。因此请先安排维护窗口并确认管理路径；升级后检查服务状态和本机控制接口：

```bash
sudo systemctl is-active routerd.service
sudo routerctl get status
```

涉及 BGP 等对短暂重启敏感的生产功能时，请先阅读[变更记录](./releases/changelog.md)和自己的运维流程。

## 平台范围

Ubuntu Server 是当前主要目标。FreeBSD 和 NixOS 的安装布局、服务管理和网络渲染仍是各自的平台工作，不能把本页的 systemd/nftables 步骤当成它们的等价操作说明。请先读[支持的平台](./platforms.md)。

## 继续学习

- [安全起步](./tutorials/getting-started.md)
- [第一台实验路由器](./tutorials/first-router.md)
- [发布版和稳定版](./releases/stable.md)
