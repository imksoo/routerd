---
title: Tailscale 的 exit node 與 subnet router
---

# Tailscale 的 exit node 與 subnet router

## 適用情境

當 routerd 主機需要加入 tailnet，並廣告以下路由時，請使用 `TailscaleNode`。

- exit node（`0.0.0.0/0` 與 `::/0`）
- 一個或多個 subnet route
- exit node 與 subnet route 兩者並行

routerd 不會取代 `tailscaled`。
routerd 會產生一個 systemd unit 來執行 `tailscale up`，並管理節點的廣告設定。
Tailscale 的帳號、控制平面及路由核准流程留在 Tailscale 端處理；
routerd 負責管理主機上的宣告式設定。

## 安裝 tailscale

以 `Package` 宣告相依套件，讓必要的套件從 YAML 中即可一目了然。

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

在 Ubuntu 上，`Package` 安裝 `tailscale` 之前，需先確保 Tailscale 的 apt 儲存庫已可用。
請透過一般的初始建置程序完成儲存庫的新增。

## 不將密鑰留在 Git 中

正式環境建議使用 `authKeyEnv` 搭配 `authKeyFile`。

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

環境變數檔案置於 routerd YAML 之外。

```sh
sudo install -d -m 0700 /usr/local/etc/routerd/secrets
sudo sh -c 'printf "%s\n" "TS_AUTHKEY=REDACTED" > /usr/local/etc/routerd/secrets/tailscale.env'
sudo chmod 0600 /usr/local/etc/routerd/secrets/tailscale.env
```

節點已登入的情況下，可省略 `authKey`、`authKeyEnv`、`authKeyFile`。
此時 routerd 不會將密鑰嵌入 systemd unit，只會重新套用廣告設定。

Tailscale 預設使用 UDP/41641。
當設定中存在 `TailscaleNode` 時，routerd 會將此埠視為已保留。
若 `WireGuardInterface` 設定使用相同埠，驗證時會予以拒絕。

## 廣告所有私有位址

當路由器作為自宅或據點網路的 tailnet 入口時，可廣告 RFC 1918 的全部私有位址空間。

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

套用設定後，請在 Tailscale 管理主控台核准廣告的路由。
核准前，`tailscale debug prefs` 可看到請求的路由；
但 `tailscale status --self --json` 的 `Self.AllowedIPs` 中可能尚未出現。

## 防火牆 zone 的配置

將 `tailscale0` 宣告為 `Interface`，使其顯示在狀態與 Web 管理介面的介面清單中。

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

指定 `mtu: 1280` 可讓衍生的 TCP MSS clamp 在考量 Tailscale 路由的同時，
不會對無關的 LAN 至 WAN 路由降低 MTU。

家庭路由器建議將 `tailscale0` 放在 `trust` zone，而非 `mgmt` zone。

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

此設定下，tailnet 的客戶端可透過 `trust -> self` 的路徑存取 routerd 的 Web 管理介面等服務。
只要防火牆拒絕 `trust -> mgmt` 的轉發，tailnet 便無法廣泛存取管理 VLAN。

僅在希望將整個 tailnet 視為管理網路時，才將 `tailscale0` 放入 `mgmt`。

## 套用與確認

確認設定後重啟 routerd。

```sh
routerd validate --config /usr/local/etc/routerd/router.yaml
systemctl restart routerd.service
```

確認產生的 systemd unit。

```sh
systemctl cat routerd-tailscale-edge.service
```

確認 Tailscale 端的狀態。

```sh
tailscale status --self --json | jq '.BackendState, .Self.AllowedIPs'
tailscale debug prefs | jq '.AdvertiseRoutes'
```

確認 routerd 端的狀態。

```sh
routerctl status --json
routerctl get TailscaleNode/edge -o yaml
routerctl tailscale peers
```

`routerctl tailscale peers -o json` 會讀取 `tailscale status --json`，並以 routerd CLI 格式顯示對等節點清單。Web 管理介面的 Resources 頁面也會在 `TailscaleNode` 中顯示對等節點的線上狀態、relay、最後上線時間及允許的路由。

若要透過 Tailscale 存取 Web 管理介面，請使用路由器的 Tailscale 位址或已核准路由上的位址。

```sh
curl -f http://100.64.0.1:8080/
```

上述位址僅為範例，請替換為實際的路由器 Tailscale IP。

## 補充說明

- 設定 `acceptDNS: false` 可防止 Tailscale 覆蓋路由器本身的 DNS 設定。routerd 的基本方針是優先使用 LAN 端的 DNS。`DNSResolver`、本地 zone、DHCP 衍生記錄及條件式轉發均以 LAN 端為權威，不讓 MagicDNS 接管主機的解析器。
- 設定 `acceptRoutes: false` 可防止路由器匯入其他節點廣告的路由。對於負責向外廣告路由的路由器而言，此為合理的設定。
- routerd 會針對 Tailscale 對等節點匯出 `routerd.tailscale.peer.count` 與 `routerd.tailscale.last_handshake.seconds` 指標。運維上判斷握手經過時間時，請使用 Tailscale status 的 `LastSeen`。
- exit node 與 subnet route 的核准在 Tailscale 端進行。
- 請勿將 auth key 留在範例或 Git 記錄中。實機部署請使用 `authKeyFile`。
