---
title: 安装与升级
---

# 安装与升级

![routerd 从发布归档安装、检查配置、dry-run，到启动服务的流程](/img/diagrams/install-and-upgrade.png)

本页采用 Ubuntu Server + systemd 的入门路径。请在隔离 VM 或有本地控制台的备用主机上完成第一次安装；它不适合在唯一的生产网关上远程试错。

## 1. 下载并安装发布归档

在 [GitHub Releases](https://github.com/imksoo/routerd/releases) 选择与 CPU 架构相符的归档。下面使用当前推荐的稳定里程碑 [v20260707.1514](https://github.com/imksoo/routerd/releases/tag/v20260707.1514)；[稳定版页面](./releases/stable.md)是这个推荐的唯一来源。示例为 Linux amd64；arm64 请把文件名改为 `linux-arm64`。

```bash
RELEASE=v20260707.1514
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

把示例复制成自己的配置并编辑接口名。`ens18`、`ens19` 只是常见 VM 名称，不一定是你的机器上的名字。服务尚未运行时，不要用 `routerctl`；先直接使用 `routerd` 检查文件。

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudoedit /usr/local/etc/routerd/router.yaml
sudo routerd validate --config /usr/local/etc/routerd/router.yaml
```

`routerd validate` 直接读取 YAML，不需要服务已经运行，也不会改动主机网络。

接着做一次隔离的 dry-run。临时路径让状态报告和所有可能的渲染目标都远离系统默认目录；`--skip-service-manager` 会略过服务管理器操作，`--dry-run` 本身不会提交网络变更。

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json" \
  --netplan-file "$LAB_DIR/50-routerd.yaml" \
  --dnsmasq-file "$LAB_DIR/dnsmasq.conf" \
  --dnsmasq-service-file "$LAB_DIR/routerd-dnsmasq.service" \
  --nftables-file "$LAB_DIR/routerd-nat.nft"
```

检查输出中的接口名、依赖和警告。dry-run 会观察部分主机信息，但不应把网络设置应用到主机；它也不能证明网线、ISP 或防火墙策略在真实流量下正确。

确认有控制台或独立管理路径后，才启动正常服务：

```bash
sudo systemctl enable --now routerd.service
sudo systemctl is-active routerd.service
sudo routerctl get status
```

现在 `routerd serve` 已通过 systemd 运行，`routerctl` 才有可连接的本机 socket。之后的 `routerctl validate`、`routerctl plan` 和 `routerctl apply` 都是向这个运行中的服务请求操作。

## 升级

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
