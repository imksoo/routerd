---
title: Tailscale exit node 与 subnet router
---

# Tailscale exit node 与 subnet router

## 场景

当一台 routerd 主机要加入 tailnet,并广告下面其中之一(或同时)时,使用 `TailscaleNode`:

- exit node (`0.0.0.0/0` 与 `::/0`)
- 一条或多条 subnet route
- 同时两者

routerd 不会替代 `tailscaled`。它会生成并管理一个 systemd unit,以声明的选项执行 `tailscale up`。Tailscale 账号、控制平面、route 审批流程留在 Tailscale 一侧;routerd 只承担主机端的意图。

## 安装 tailscale

把 OS 包声明出来,让依赖在 router 配置中可见:

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

在 Ubuntu 上,Tailscale apt 仓库必须已可用,`Package` 才能安装 `tailscale`。请按平常的 bootstrap 流程准备该仓库。

## 认证但不在 Git 留下密钥

生产环境优先使用 `authKeyEnv` 搭配 `authKeyFile`:

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

环境文件放在 routerd YAML 之外:

```sh
sudo install -d -m 0700 /usr/local/etc/routerd/secrets
sudo sh -c 'printf "%s\n" "TS_AUTHKEY=REDACTED" > /usr/local/etc/routerd/secrets/tailscale.env'
sudo chmod 0600 /usr/local/etc/routerd/secrets/tailscale.env
```

若节点已登录,可同时省略 `authKey`、`authKeyEnv`、`authKeyFile`。routerd 会在不把密钥塞进 service unit 的前提下,只重新应用所声明的节点选项。

## 广告私网段

把整个 RFC 1918 私有地址空间广告出去,适用于「router 是 tailnet 回家或回站点网络的入口」这一类场景:

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

应用此配置之后,要在 Tailscale 管理控制台审批被广告的路由。审批前,`tailscale debug prefs` 能看到所请求的 route,但 `tailscale status --self --json` 的 `Self.AllowedIPs` 可能还不包含它们。

## 防火墙 zone 放置

把 `tailscale0` 声明为 `Interface`,让它出现在状态与 Web Console 中:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: tailscale
spec:
  ifname: tailscale0
  managed: false
```

家用 router 推荐把 `tailscale0` 放进 `trust` zone,而不是 `mgmt` zone:

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

这样,tailnet 上的客户端就能通过正常的 `trust -> self` 路径访问 router 上的服务,比如 routerd Web Console。只要防火墙策略仍然对 `trust -> mgmt` 转发拒绝,tailnet 也不会因此获得对管理 VLAN 的广泛访问。

只有当你打算把 tailnet 当作完整的管理网络时,才用 `mgmt`。

## 应用与验证

应用配置:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
systemctl restart routerd.service
```

查看生成的 unit:

```sh
systemctl cat routerd-tailscale-edge.service
```

查看 Tailscale 状态:

```sh
tailscale status --self --json | jq '.BackendState, .Self.AllowedIPs'
tailscale debug prefs | jq '.AdvertiseRoutes'
```

查看 routerd 状态:

```sh
routerctl status --json
routerctl get TailscaleNode/edge -o yaml
```

若希望 Web Console 从 Tailscale 一侧可达,就用 router 的 Tailscale 地址或已审批的路由地址测试:

```sh
curl -f http://100.64.0.1:8080/
```

请把上面的地址换成 router 实际的 Tailscale IP。

## 注意事项

- `acceptDNS: false` 可避免 Tailscale 替换 router 本地的 DNS 解析配置。
- `acceptRoutes: false` 可避免 router 引入其他 peer 广告的 route。对「向外广告 route」的 router 来说,这是常见配置。
- Exit node 和 subnet route 的审批在 Tailscale 一侧完成,不在 routerd 这边。
- 不要把 auth key 放进示例和 Git 历史。本地部署请使用 `authKeyFile`。
