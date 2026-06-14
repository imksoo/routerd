---
title: 入门指南
---

# 入门指南

![从 interface discovery 与小型 YAML config 到 validate、plan、dry-run、serve、routerctl status 的安全 first routerd loop](/img/diagrams/tutorial-getting-started.png)

本教程首先确认安全的操作流程。

1. 编写小型的路由器资源文件。
2. 验证。
3. 确认计划。
4. 预演执行。
5. 确认安全后启动守护进程。

第一次确认时，不会变更主机的网络配置。
请先通过 release 归档文件与 `install.sh` 安装 routerd。
各 OS 的安装步骤请参阅[安装与升级](../install-and-upgrade.md)。

## 1. 确认接口名称

```bash
ip link
```

本教程以 WAN 为 `ens18`、LAN 为 `ens19`、管理用为 `ens20` 为例。
在实际主机上请务必根据自身环境替换。

请将管理路径与要变更的接口分开。
若只对 routerd 将接管的接口进行初次验证，风险较高。

## 2. 描述接口与主机准备

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Package
      metadata:
        name: router-host-tools
      spec:
        packages:
          - os: ubuntu
            names: [dnsmasq, nftables, conntrack, iproute2]

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true
```

路由器功能所需的主机端运行时配置，routerd 会从声明的资源中自动推导。
`Package`、`Sysctl`、`SysctlProfile` 仅作为补充尚无法自动推导的软件包或内核配置的有限逃生口，请仅在必要时使用。

## 3. 验证

```bash
routerctl validate -f first-router.yaml --replace
```

验证步骤在 routerd 接触主机之前，先确认资源的格式是否正确。

## 4. 确认计划

```bash
routerctl plan -f first-router.yaml --replace
```

计划步骤可确认接口名称错误、缺少依赖关系，以及将生成的主机产物。

## 5. 预演执行

```bash
routerctl plan -f first-router.yaml --replace
```

预演执行可确认资源加载、依赖顺序及生成内容。
不会确认网络变更。

## 6. 计划安全后启动守护进程

```bash
sudo routerd serve --config first-router.yaml
```

在生产环境中，请使用生成的服务产物资源或 systemd unit 文件。
这样便能在系统启动时自动执行 `routerd serve`。

## 7. 确认状态

```bash
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

下一篇教程将添加 LAN 的 DHCP、RA、DNS、路由策略、NAT44 与 DS-Lite。
