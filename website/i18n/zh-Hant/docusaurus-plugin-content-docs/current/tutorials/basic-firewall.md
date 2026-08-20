---
title: 基本 NAT 與防火牆
sidebar_position: 6
---

# 基本 NAT 與防火牆：先了解責任邊界

![WAN、LAN、NAT44Rule、FirewallZone 與 FirewallPolicy 的 routerd 入門關係圖](/img/diagrams/tutorial-basic-firewall.png)

NAT 和防火牆不是同一件事：

- **NAT44** 讓私有 LAN 位址借用 WAN 的 IPv4 位址與外部網路通訊。
- **防火牆** 決定哪些流量可通過或到達路由器。

本頁說明資源的寫法，不應視為可直接暴露到網際網路的生產安全方案。目前著重 Linux 的 nftables 路徑，Ubuntu Server 是主要入門平台。

## 一條現代的 NAT44Rule

假設已宣告名為 `wan` 和 `lan` 的 `Interface` 資源，LAN 網段為 `192.168.10.0/24`。現行欄位如下：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: lan-to-wan
  spec:
    type: masquerade
    egressInterface: wan
    sourceRanges:
      - 192.168.10.0/24
    excludeDestinationCIDRs:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
```

`type: masquerade` 是採用出站介面目前 IPv4 位址的方式，適合位址可能變動的 DHCP WAN。`egressInterface` 指的是邏輯介面資源名稱，不是任意猜測的網卡名稱。`excludeDestinationCIDRs` 讓通往常見私有網段的流量不做偽裝，避免把原本應走內部路由的流量錯當成網際網路流量。

## 防火牆資源表達什麼

以下為 WAN/LAN 區域與一個策略資源的最小結構：

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - wan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallPolicy
  metadata:
    name: home
  spec:
    logDeny: true
```

`FirewallZone` 以 `untrust`、`trust`、`mgmt` 描述網路位置；`FirewallPolicy` 放置全域行為，例如拒絕記錄。NAT44 與過濾使用不同 nftables 表，所以「已經有 NAT」不代表「所有入站流量都已審查並保護」。

:::caution 防火牆仍在基礎實作階段
routerd 是預發布軟體。這裡的區域與策略資源不是通用防火牆語言，也不是安全認證。不要只憑這段 YAML 就宣稱 WAN 一定無法連到 LAN，或把主機暴露到公網。真實部署前需要針對介面、管理入口、VLAN、連接埠轉送與回程流量做獨立檢查與測試。
:::

## 安全檢查設定

先讓 `routerd` 直接驗證完整檔案，再使用暫存路徑做 dry-run：

```bash
routerd validate --config router.yaml
workdir=$(mktemp -d)
routerd apply --once --dry-run --skip-service-manager --config router.yaml --status-file "$workdir/status.json" --state-file "$workdir/state.db" --ledger-file "$workdir/ledger.db" --netplan-file "$workdir/50-routerd.yaml" --dnsmasq-file "$workdir/dnsmasq.conf" --dnsmasq-service-file "$workdir/routerd-dnsmasq.service" --nftables-file "$workdir/routerd-nat.nft"
```

只有在 `routerd serve` 已藉由主控台或獨立管理路徑啟動後，才以 `routerctl` 檢查運行結果：

```bash
sudo routerctl get status
sudo routerctl describe NAT44Rule/lan-to-wan
sudo nft list table ip routerd_nat
```

## 繼續閱讀

- [基本 IPv4 NAT 閘道](../config-examples/basic-ipv4-nat.md)
- [防火牆概念](../concepts/firewall.md)
- [支援的平台](../platforms.md)
