---
title: DS-Lite 家用閘道器
sidebar_position: 30
---

# DS-Lite 家用閘道器

![IPv6 WAN、DS-Lite tunnel、LAN IPv4 與 delegated IPv6 service 的架構](/img/diagrams/config-example-dslite-home.png)

DS-Lite 用在 IPv6 為主的線路：IPv4 封包經 ISP 的 tunnel 出網。這不是第一台路由器的
教學，而是需要 ISP 資料的進階範例。WAN、tunnel 或 DNS 設錯會中斷連線，請只在有主控台或
獨立管理路徑的實驗環境操作。

完整且已驗證的 YAML 在
[`examples/example-dslite-home.yaml`](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)。
其中接近 Transix 的 AFTR 值只是範例，必須換成自己線路的資料。

## 此範例建立什麼

| 工作 | YAML 中實際的資源名 |
| --- | --- |
| 從 WAN 取得 IPv6 delegated prefix | `DHCPv6PrefixDelegation/wan-pd` |
| 導出 LAN 的 IPv6 位址 | `IPv6DelegatedAddress/lan-v6` |
| 在 LAN 回答 DNS | `DNSResolver/lan`、`DNSZone/home` |
| 建立 IPv4-over-IPv6 tunnel | `DSLiteTunnel/transix` |
| 對 LAN 配發 IPv4、DNS 與 IPv6 router 資訊 | `DHCPv4Server/lan`、`IPv6RouterAdvertisement/lan` |

`lan-v4`、`lan-v6`、`lan`、`transix` 是此檔案選的名稱，不是每個 routerd 設定都必須使用的名稱。

## 設定如何相連

WAN prefix delegation 與從中導出的 LAN IPv6 位址以名稱相連：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: wan-pd
  spec:
    interface: wan
    profile: ntt-hgw-lan-pd

- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: lan-v6
  spec:
    prefixDelegation: wan-pd
    interface: lan
    subnetID: "0"
    addressSuffix: "::1"
    announce: true
```

DS-Lite tunnel 使用同一個 `lan-v6` 作為本地端位址：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: transix
  spec:
    interface: wan
    tunnelName: ds-transix
    aftrFQDN: gw.transix.jp
    aftrDNSServers: [2404:1a8:7f01:a::3, 2404:1a8:7f01:b::3]
    localAddressSource: delegatedAddress
    localDelegatedAddress: lan-v6
    localAddressSuffix: "::100"
    defaultRoute: true
```

若你的 ISP 要求用 WAN Router Advertisement 位址當 tunnel source，請依 ISP 指定選擇
`localAddressSource`，不要直接複製此值。

DNS、DHCPv4、RA 也使用同一組本地資源名：

```yaml
- kind: DNSResolver
  metadata:
    name: lan
  # 在 IPv4StaticAddress/lan-v4 與 IPv6DelegatedAddress/lan-v6 監聽。

- kind: DHCPv4Server
  metadata:
    name: lan
  # 對用戶端公告 IPv4StaticAddress/lan-v4 為 gateway 與 DNS。

- kind: IPv6RouterAdvertisement
  metadata:
    name: lan
  # 公告 IPv6DelegatedAddress/lan-v6 與 DNSZone/home。
```

短摘錄只說明資源名的連結；必要的 `spec` 欄位請閱讀完整 YAML。

## daemon 尚未啟動時先檢查

複製檔案並更換所有 ISP 專屬的值後，以具有 `sudo` 權限的本機使用者執行獨立檢查。它們不會啟動服務或套用網路變更。

```sh
cp examples/example-dslite-home.yaml router.yaml
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config router.yaml
sudo routerd apply --config router.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

確認 WAN/LAN 名稱、AFTR FQDN、resolver 位址與管理路徑都是自己的資料。只要其中一項不對，
就不要進行下一步。

## 套用後觀察

只能從主控台或獨立管理路徑執行 live 操作。服務啟動後可檢查；首次安裝請保留 `sudo`。若管理員透過 `routerd` 群組授予本機 socket 存取權，加入後必須重新登入才會生效：

```sh
sudo routerctl get status
sudo routerctl describe DHCPv6PrefixDelegation/wan-pd
sudo routerctl describe IPv6DelegatedAddress/lan-v6
sudo routerctl describe DSLiteTunnel/transix
sudo routerctl describe FirewallZone/wan
sudo ip -6 tunnel show
sudo ip route show default
```

LAN 用戶端上檢查 IPv6、路由和本地 DNS：

```sh
ip -6 addr
ip route
curl https://1.1.1.1/
dig router.home.example
```

## 相關頁面

- [WAN 側服務](../tutorials/wan-side-services.md)
- [LAN 側服務](../tutorials/lan-side-services.md)
- [基本 IPv4 NAT 閘道器](./basic-ipv4-nat.md)
