---
title: Tailscale exit node 與 subnet router
---

# Tailscale exit node 與 subnet router

## 情境

當一個 routerd 主機要加入 tailnet,並廣告以下其中之一(或同時)時,使用 `TailscaleNode`:

- exit node (`0.0.0.0/0` 與 `::/0`)
- 一個或多個 subnet route
- 兩者並行

routerd 不會取代 `tailscaled`。它會生成並管理一個 systemd unit,以宣告的選項執行 `tailscale up`。Tailscale 帳號、控制平面、route 核准流程留在 Tailscale 端;routerd 只負責主機端的意圖。

## 安裝 tailscale

把 OS 套件聲明出來,讓相依關係在 router 設定中可見:

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

在 Ubuntu 上,Tailscale apt 倉庫必須已可用,`Package` 才能安裝 `tailscale`。請依平常的 bootstrap 流程準備該倉庫。

## 認證但不在 Git 留下密鑰

正式環境請優先使用 `authKeyEnv` 搭配 `authKeyFile`:

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

環境檔放在 routerd YAML 之外:

```sh
sudo install -d -m 0700 /usr/local/etc/routerd/secrets
sudo sh -c 'printf "%s\n" "TS_AUTHKEY=REDACTED" > /usr/local/etc/routerd/secrets/tailscale.env'
sudo chmod 0600 /usr/local/etc/routerd/secrets/tailscale.env
```

若節點已登入,可同時省略 `authKey`、`authKeyEnv`、`authKeyFile`。routerd 會在不把密鑰塞進 service unit 的情況下,只重新套用所宣告的節點選項。

## 廣告私網段

把 RFC 1918 全部私有位址空間都廣告出去,適用於「router 是 tailnet 回到家或站點網路的入口」這種場景:

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

套用設定後,要在 Tailscale 管理主控台核准被廣告的路由。在核准之前,`tailscale debug prefs` 會看到所請求的 route,但 `tailscale status --self --json` 的 `Self.AllowedIPs` 可能還沒有它們。

## 防火牆 zone 配置

把 `tailscale0` 宣告成 `Interface`,讓它出現在狀態與 Web Console 中:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: tailscale
spec:
  ifname: tailscale0
  managed: false
```

家用 router 建議把 `tailscale0` 放在 `trust` zone,而不是 `mgmt` zone:

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

這樣,tailnet 端的客戶端就能透過正常的 `trust -> self` 路徑連到 router 上的服務,例如 routerd Web Console。只要防火牆策略仍維持 `trust -> mgmt` forwarding 拒絕,tailnet 就不會因此就拿到對管理 VLAN 的廣泛存取。

只有當你打算把 tailnet 視為完整的管理網路時,才用 `mgmt`。

## 套用與驗證

套用設定:

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
systemctl restart routerd.service
```

檢查產生的 unit:

```sh
systemctl cat routerd-tailscale-edge.service
```

檢查 Tailscale 狀態:

```sh
tailscale status --self --json | jq '.BackendState, .Self.AllowedIPs'
tailscale debug prefs | jq '.AdvertiseRoutes'
```

檢查 routerd 狀態:

```sh
routerctl status --json
routerctl get TailscaleNode/edge -o yaml
```

若希望 Web Console 從 Tailscale 端可達,就用 router 的 Tailscale 位址或被核准的路由位址測試:

```sh
curl -f http://100.64.0.1:8080/
```

請把上面的位址換成 router 實際的 Tailscale IP。

## 注意事項

- `acceptDNS: false` 可避免 Tailscale 取代 router 本機的 DNS 解析設定。
- `acceptRoutes: false` 可避免 router 匯入其他 peer 廣告的 route。對「往外廣告 route」的 router 來說,這是常見的設定。
- Exit node 和 subnet route 的核准在 Tailscale 端進行,不在 routerd 這邊。
- 不要把 auth key 放進範例與 Git 歷史。本地部署請使用 `authKeyFile`。
