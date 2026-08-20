---
title: 安装
sidebar_position: 1
---

# 安装 routerd

![routerd 安装：发布归档、依赖和服务模板、保留的配置与状态，以及安装后的验证和 dry-run](/img/diagrams/tutorial-install.png)

最快的入门方式是在隔离的 Ubuntu Server VM 上使用发布归档。路由器主机不需要 Go、Makefile 或源码树。下面使用当前推荐的稳定里程碑 [v20260707.1514](../releases/stable.md)。

```bash
RELEASE=v20260707.1514
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz
curl -fLO https://github.com/imksoo/routerd/releases/download/${RELEASE}/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

arm64 主机请使用 `routerd-linux-arm64.tar.gz`。安装脚本会放置二进制文件和 systemd 模板，创建配置样本，但不会覆盖已有的 `/usr/local/etc/routerd/router.yaml`。

```bash
routerd --version
```

## 安装后先做这两件事

1. 编辑自己的配置。
2. 用 `routerd` 直接检查它，而不是先使用 `routerctl`。

```bash
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudoedit /usr/local/etc/routerd/router.yaml
sudo routerd validate --config /usr/local/etc/routerd/router.yaml
```

然后使用临时目录执行 dry-run：

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

`--skip-service-manager` 会跳过服务管理器操作；所有状态和渲染文件都留在临时目录中。

只有在你有控制台或独立管理路径，并已检查输出后，才启动服务：

```bash
sudo systemctl enable --now routerd
sudo routerctl get status
```

`routerctl` 现在可以工作，是因为 `routerd serve` 已作为 systemd 服务运行。它不是离线 YAML 验证器。

:::note 平台范围
本教程写给 Ubuntu Server + systemd。FreeBSD 和 NixOS 的支持处于不同阶段，不能直接照搬这里的服务和 nftables 步骤；请先阅读[支持的平台](../platforms.md)。
:::

有关升级、依赖列表、Live ISO 和卸载，请查看[安装与升级](../install-and-upgrade.md)。
