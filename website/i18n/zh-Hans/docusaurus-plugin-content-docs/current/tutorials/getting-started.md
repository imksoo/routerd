---
title: 安全起步
---

# 安全起步：先检查，不先断网

![安全的 routerd 首次流程：发现接口、写小 YAML、验证、临时路径 dry-run、启动服务、读取状态](/img/diagrams/tutorial-getting-started.png)

本教程的目标不是马上把机器变成家庭网关，而是安全地完成第一轮：写一份很小的配置、检查它、再在不提交网络变更的条件下运行一次。请使用隔离的 Ubuntu Server VM 或备用电脑，并保留控制台。

## 1. 先找出接口名

```bash
ip -br link
```

示例把 `ens18` 当 WAN、`ens19` 当 LAN。你的名字可能是 `enp1s0`、`eth0` 或别的名称，必须按输出替换。不要从正要被 routerd 接管的唯一网络接口远程 SSH 进去做实验。

## 2. 写最小配置

把下面内容保存为 `first-router.yaml`。这一步只声明两个接口；它还没有 DHCP 服务、NAT 或互联网共享。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: false
        owner: external

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true
        owner: routerd
```

`metadata.name` 是配置内部使用的名字；`spec.ifname` 才是 Linux 实际看到的接口名。把 WAN 标为 `external` 是一种保守起点：先不要让 routerd 直接接管上游链接。

## 3. 离线验证文件

```bash
routerd validate --config first-router.yaml
```

这个命令独立工作，不要求 `routerd serve` 已启动，也不写入网络设置。出错时先改 YAML 或接口名，而不是尝试真实应用。

## 4. 用临时路径做一次 dry-run

```bash
workdir=$(mktemp -d)
routerd apply --once --dry-run --skip-service-manager --config first-router.yaml --status-file "$workdir/status.json" --state-file "$workdir/state.db" --ledger-file "$workdir/ledger.db" --netplan-file "$workdir/50-routerd.yaml" --dnsmasq-file "$workdir/dnsmasq.conf" --dnsmasq-service-file "$workdir/routerd-dnsmasq.service" --nftables-file "$workdir/routerd-nat.nft"
```

`--dry-run` 会计算依赖关系、计划和渲染意图，但不会提交主机网络变更。临时目录还避免状态报告和输出路径落入 `/run`、`/etc` 或 `/var/lib`。阅读输出，特别留意接口名、资源引用和管理路径警告。

不要把 dry-run 当作连通性或安全测试：它不会替你验证真实 ISP、交换机 VLAN 或完整的防火墙暴露面。

## 5. 准备好后再运行 daemon

真实应用由 `routerd apply --once`（不带 `--dry-run`）或 `routerd serve` 完成，两者都可能改变网络。确认有控制台或独立管理网络后，再把配置安装到默认位置并启动服务：

```bash
sudo install -m 0600 first-router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd
sudo routerctl get status
```

最后一条命令之所以放在这里，是因为此时 `routerd serve` 已由 systemd 运行。`routerctl` 通过这个运行中的 daemon 查询状态；服务没启动时不要用它替代 `routerd validate`。

## 下一步

- [第一台实验路由器](./first-router.md)会加入 WAN DHCP 和 LAN 网关地址。
- [WAN 侧服务](./wan-side-services.md)介绍更多上游连接方式。
- [LAN 侧服务](./lan-side-services.md)介绍 DHCP、DNS、RA 和 NTP。
