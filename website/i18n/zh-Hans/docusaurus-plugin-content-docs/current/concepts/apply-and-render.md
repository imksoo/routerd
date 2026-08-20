---
title: 应用、渲染与调和
slug: /concepts/apply-and-render
sidebar_position: 4
---

# 应用、渲染与调和

![同一份资源图如何经过 routerd 验证、dry-run、运行中服务和 routerctl 客户端](/img/diagrams/concept-apply-and-render.png)

这几个词看起来相近，却对应不同风险等级。先分清它们，能避免把“检查一份文件”和“更改路由器”混为一谈。

| 操作 | 谁执行 | 是否需要运行中的 daemon | 会改变主机网络吗 |
| --- | --- | --- | --- |
| `routerd validate --config FILE` | 本地 `routerd` | 不需要 | 不会 |
| `routerd apply --once --dry-run …` | 本地 `routerd` | 不需要 | 不会提交网络变更 |
| `routerd apply --once …` | 本地 `routerd` | 不需要 | 会，可能改变网络 |
| `routerd serve --config FILE` | 常驻 `routerd` | 它本身就是 daemon | 会，可能改变网络 |
| `routerctl …` | 客户端 | 需要 `routerd serve` | 请求由 daemon 执行 |

## 验证：检查文件本身

`routerd validate` 会读取 YAML，检查资源种类、必填字段、值的格式和明显的引用错误。它是服务启动前应该使用的命令。

```bash
routerd validate --config first-router.yaml
```

它不会检查真实网线是否接好，也不会证明 ISP 能连通；它只回答“这份 routerd 配置是否有效”。

## Dry-run：把一次应用限制在临时位置

dry-run 会计算资源顺序、派生项和渲染意图，同时不提交网络变更。为避免结果文件落入默认系统目录，给它一个新建的临时目录：

```bash
workdir=$(mktemp -d)
routerd apply --once --dry-run --skip-service-manager --config first-router.yaml --status-file "$workdir/status.json" --state-file "$workdir/state.db" --ledger-file "$workdir/ledger.db" --netplan-file "$workdir/50-routerd.yaml" --dnsmasq-file "$workdir/dnsmasq.conf" --dnsmasq-service-file "$workdir/routerd-dnsmasq.service" --nftables-file "$workdir/routerd-nat.nft"
```

`--state-file`、`--ledger-file`、`--status-file` 和渲染文件参数都指向临时目录。dry-run 仍可能读取主机状态来形成计划，因此不要把它理解为完全脱离主机的模拟器。

## 应用与 serve：真正影响主机

不带 `--dry-run` 的 `routerd apply --once` 会执行一次真实应用；`routerd serve` 则持续运行、维护 Unix socket，并在需要时调和资源。两者都可能改变地址、路由、服务和 nftables 规则。

只有在你有控制台或独立管理网卡，并确认管理路径不会被移除时，才运行真实应用。正常 Ubuntu 安装通常由 systemd 启动 `routerd serve`。

## routerctl：给正在运行的 routerd 发请求

`routerctl` 不直接离线处理候选 YAML。它把请求交给运行中的 `routerd serve`：

```bash
sudo routerctl get status
sudo routerctl describe Interface/lan
sudo routerctl validate -f candidate.yaml --replace
sudo routerctl plan -f candidate.yaml --replace
```

前两条读取运行时状态；后两条让 daemon 验证或计划候选配置。若 socket 不可达，先启动或检查 `routerd serve`，不要把错误当作 YAML 验证失败。

`routerctl apply -f candidate.yaml --replace` 也只适用于运行中的服务：它请求 daemon 写入候选配置并调和。它不是比 `routerd apply --once` 更安全的本地替代品。

## 渲染与调和

**渲染** 是把资源转换成面向主机的内容，例如 dnsmasq 配置、nftables 规则集或 systemd 单元。渲染本身不等于一定已经修改主机；关键还在于当前操作是 validate、dry-run、真实 apply 还是 serve。

**调和（reconcile）** 是 serve 模式持续缩小“YAML 想要的状态”和“主机当前状态”差距的过程。例如 DHCPv6-PD 更新前缀后，相关 LAN 地址、RA 和路由会再次评估。

:::caution 防火墙边界
routerd 的防火墙资源仍处于预发布的基础实现阶段，不是通用规则语言或安全认证。dry-run 和渲染成功也不能代替针对真实接口、VLAN、暴露服务和回程路径的安全审查。
:::
