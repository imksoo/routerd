---
title: Tailscale 的 exit node 与 subnet router
---

# Tailscale 的 exit node 与 subnet router

![TailscaleNode 处理 tailscaled、auth key file、advertised subnet、exit-node intent 与 tailnet approval flow 的流程](/img/diagrams/how-to-tailscale.png)

## 适用场景

当 routerd 主机需要加入 tailnet，并广告以下路由时，请使用 `TailscaleNode`。

- exit node（`0.0.0.0/0` 与 `::/0`）
- 一个或多个 subnet route
- exit node 与 subnet route 两者并行

routerd 不会取代 `tailscaled`。
routerd 会生成一个 systemd unit 来执行 `tailscale up`，并管理节点的广告配置。
Tailscale 的账号、控制平面及路由审批流程留在 Tailscale 端处理；
routerd 负责管理主机上的声明式配置。

## 安装 tailscale

以 `Package` 声明依赖软件包，让必要的软件包从 YAML 中即可一目了然。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Package
metadata:
  name: router-service-dependencies
spec:
  packages:
    - os: ubuntu
      manager: apt
      names:
        - tailscale
        - tailscale-archive-keyring
    - os: nixos
      manager: nix
      names:
        - tailscale
    - os: freebsd
      manager: pkg
      names:
        - tailscale
      optional: true
```

在 Ubuntu 上，`Package` 安装 `tailscale` 之前，需先确保 Tailscale 的 apt 仓库已可用。
请通过一般的初始构建程序完成仓库的添加。

## 不将密钥留在 Git 中

生产环境建议使用 `authKeyEnv` 搭配 `authKeyFile`。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: TailscaleNode
metadata:
  name: edge
spec:
  hostname: edge
  advertiseExitNode: true
  advertiseRoutes:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  acceptDNS: false
  acceptRoutes: false
  authKeyEnv: TS_AUTHKEY
  authKeyFile: /usr/local/etc/routerd/secrets/tailscale.env
```

环境变量文件置于 routerd YAML 之外。

```sh
sudo install -d -m 0700 /usr/local/etc/routerd/secrets
sudo sh -c 'printf "%s\n" "TS_AUTHKEY=REDACTED" > /usr/local/etc/routerd/secrets/tailscale.env'
sudo chmod 0600 /usr/local/etc/routerd/secrets/tailscale.env
```

节点已登录的情况下，可省略 `authKey`、`authKeyEnv`、`authKeyFile`。
此时 routerd 不会将密钥嵌入 systemd unit，只会重新应用广告配置。

Tailscale 默认使用 UDP/41641。
当配置中存在 `TailscaleNode` 时，routerd 会将此端口视为已保留。
若 `WireGuardInterface` 配置使用相同端口，验证时会予以拒绝。

## 广告所有私有地址

当路由器作为自宅或站点网络的 tailnet 入口时，可广告 RFC 1918 的全部私有地址空间。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: TailscaleNode
metadata:
  name: edge
spec:
  hostname: edge
  advertiseExitNode: true
  advertiseRoutes:
    - 10.0.0.0/8
    - 172.16.0.0/12
    - 192.168.0.0/16
  acceptDNS: false
  acceptRoutes: false
```

应用配置后，请在 Tailscale 管理控制台审批广告的路由。
审批前，`tailscale debug prefs` 可看到请求的路由；
但 `tailscale status --self --json` 的 `Self.AllowedIPs` 中可能尚未出现。

## 防火墙 zone 的配置

将 `tailscale0` 声明为 `Interface`，使其显示在状态与 Web 管理界面的接口列表中。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: tailscale
spec:
  ifname: tailscale0
  mtu: 1280
  managed: false
```

指定 `mtu: 1280` 可让派生的 TCP MSS clamp 在考量 Tailscale 路由的同时，
不会对无关的 LAN 至 WAN 路由降低 MTU。

家庭路由器建议将 `tailscale0` 放在 `trust` zone，而非 `mgmt` zone。

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallZone
metadata:
  name: lan
spec:
  role: trust
  interfaces:
    - Interface/lan
    - Interface/tailscale

---

apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallZone
metadata:
  name: management
spec:
  role: mgmt
  interfaces:
    - Interface/mgmt
```

此配置下，tailnet 的客户端可通过 `trust -> self` 的路径访问 routerd 的 Web 管理界面等服务。
只要防火墙拒绝 `trust -> mgmt` 的转发，tailnet 便无法广泛访问管理 VLAN。

仅在希望将整个 tailnet 视为管理网络时，才将 `tailscale0` 放入 `mgmt`。

## 应用与确认

确认配置后重启 routerd。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
systemctl restart routerd.service
```

确认生成的 systemd unit。

```sh
systemctl cat routerd-tailscale-edge.service
```

确认 Tailscale 端的状态。

```sh
tailscale status --self --json | jq '.BackendState, .Self.AllowedIPs'
tailscale debug prefs | jq '.AdvertiseRoutes'
```

确认 routerd 端的状态。

```sh
routerctl status --json
routerctl get TailscaleNode/edge -o yaml
routerctl tailscale peers
```

`routerctl tailscale peers -o json` 会读取 `tailscale status --json`，并以 routerd CLI 格式显示对等节点列表。Web 管理界面的 Resources 页面也会在 `TailscaleNode` 中显示对等节点的在线状态、relay、最后上线时间及允许的路由。

若要通过 Tailscale 访问 Web 管理界面，请使用路由器的 Tailscale 地址或已审批路由上的地址。

```sh
curl -f http://100.64.0.1:8080/
```

上述地址仅为示例，请替换为实际的路由器 Tailscale IP。

## 补充说明

- 设置 `acceptDNS: false` 可防止 Tailscale 覆盖路由器本身的 DNS 配置。routerd 的基本方针是优先使用 LAN 端的 DNS。`DNSResolver`、本地 zone、DHCP 派生记录及条件式转发均以 LAN 端为权威，不让 MagicDNS 接管主机的解析器。
- 设置 `acceptRoutes: false` 可防止路由器导入其他节点广告的路由。对于负责向外广告路由的路由器而言，此为合理的配置。
- routerd 会针对 Tailscale 对等节点导出 `routerd.tailscale.peer.count` 与 `routerd.tailscale.last_handshake.seconds` 指标。运维上判断握手经过时间时，请使用 Tailscale status 的 `LastSeen`。
- exit node 与 subnet route 的审批在 Tailscale 端进行。
- 请勿将 auth key 留在示例或 Git 记录中。实机部署请使用 `authKeyFile`。
