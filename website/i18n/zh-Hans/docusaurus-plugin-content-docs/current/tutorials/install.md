---
title: 安装
sidebar_position: 1
---

# 安装

![从 release archive install routerd，安装 dependency 与 service template，preserve config/state，并执行 validate-plan-dry-run 的流程](/img/diagrams/tutorial-install.png)

routerd 从 release 归档文件安装。
路由器主机上不需要 Go 或 Makefile。

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

Linux arm64 主机请使用 `routerd-linux-arm64.tar.gz`。

FreeBSD 请获取 `routerd-freebsd-amd64.tar.gz`，并执行相同的
`./install.sh`。
FreeBSD arm64 主机请使用 `routerd-freebsd-arm64.tar.gz`。
若要固定至特定版本，请使用 release 页面上附有版本号的归档文件。

Linux 归档文件包含静态链接的 routerd 可执行文件
（`CGO_ENABLED=0`）。
不依赖路由器主机的 glibc 版本。

安装程序将执行以下步骤：

- 通过对应的软件包管理器安装运行时软件包。
- 将可执行文件放置至 `/usr/local/sbin`。
- 放置 systemd 或 rc.d 的服务模板。
- 创建 `/usr/local/etc/routerd/router.yaml.sample`。
- 保留现有的 `/usr/local/etc/routerd/router.yaml`。
- 保留 `/var/lib/routerd` 或 `/var/db/routerd` 的状态。
- 若有只读状态 socket，则执行 `routerctl get status`。

常用选项：

```sh
./install.sh --list-deps
sudo ./install.sh --no-install-deps
sudo ./install.sh --deps-only
sudo ./install.sh --with-tailscale
sudo ./install.sh --dry-run
```

安装后，创建配置文件并进行验证。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 /usr/local/etc/routerd/router.yaml.sample /usr/local/etc/routerd/router.yaml
sudo vi /usr/local/etc/routerd/router.yaml

routerctl validate -f /usr/local/etc/routerd/router.yaml --replace
routerctl plan -f /usr/local/etc/routerd/router.yaml --replace
```

确认管理路径仍可访问后再正式应用。

```sh
sudo routerctl apply -f /usr/local/etc/routerd/router.yaml --replace
```

各 OS 的软件包列表、升级、卸载，以及开发者适用的
release 步骤，请参阅[安装与升级](../install-and-upgrade.md)。

若要在不写入磁盘的情况下试用，请启动 `routerd-live.iso`。
以 root 登录后，相同的 `install.sh configure` 向导将会启动。
亦支持 Proxmox VE 的 `qm terminal` 串口控制台。
在向导中选择 USB 持久化保存，即可将 Live ISO 作为无磁盘的永久路由器使用。
若不选择 USB 持久化保存，ISO 将作为临时演示运行，重启后配置将消失。
