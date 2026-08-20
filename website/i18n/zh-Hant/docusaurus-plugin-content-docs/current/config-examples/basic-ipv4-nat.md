---
title: 基本 IPv4 NAT 閘道
sidebar_position: 10
---

# 基本 IPv4 NAT 閘道

![DHCP WAN、routerd 管理的 LAN 閘道、DHCPv4、NAT44 與區域資源組成的 IPv4 閘道](/img/diagrams/config-example-basic-ipv4-nat.png)

這是適合實驗室閱讀的家用 IPv4 閘道形狀：WAN 以 DHCPv4 取得位址，LAN 使用 `192.168.10.1/24`，用戶端經 NAT44 連往外部網路。完整 YAML 位於 `examples/example-basic-ipv4-nat.yaml`。

請先在隔離的 Ubuntu Server VM 進行測試，並保留主控台。這不是將正在承載生產網路的設備直接改成路由器的捷徑。

## 拓撲

```text
網際網路／上游路由器
          |
        wan（DHCPv4）
          |
      routerd 主機
          |
  lan（192.168.10.1/24）
          |
      LAN 用戶端
```

| 工作 | 主要資源 |
| --- | --- |
| 宣告 WAN 和 LAN | `Interface/wan`、`Interface/lan` |
| 取得 WAN IPv4 | `DHCPv4Client/wan-dhcpv4` |
| 持有 LAN 閘道位址 | `IPv4StaticAddress/lan-base` |
| 分配 LAN 位址 | `DHCPv4Server/lan-dhcpv4` |
| 讓 LAN IPv4 對外連線 | `NAT44Rule/lan-to-wan` |

## 重要設定：現代 NAT 欄位

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

這是目前 `NAT44Rule` 的結構：使用 `type`、`egressInterface` 和 `sourceRanges`。`masquerade` 適合 WAN 位址會改變的 DHCP 上游。排除的私有目的網段不會做 NAT，避免損壞內部、VPN 或管理路由。

下列資源將 LAN 位址與 DHCP 位址池連起來：

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4StaticAddress
  metadata:
    name: lan-base
  spec:
    interface: lan
    address: 192.168.10.1/24

- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: lan-dhcpv4
  spec:
    interface: lan
    addressPool:
      start: 192.168.10.100
      end: 192.168.10.199
      leaseTime: 12h
    gatewayFrom:
      resource: IPv4StaticAddress/lan-base
      field: address
    dnsServers:
      - 1.1.1.1
      - 1.0.0.1
```

這個第一輪實驗直接對用戶端公告外部 DNS 解析器 `1.1.1.1` 和 `1.0.0.1`，因此路由器本身暫時不需要承擔 DNS 服務。請先確認學校、家庭、公司或上游網路允許使用這些公共解析器；若網路政策指定 DNS，或你要讓路由器提供本地名稱、過濾或條件式轉送，請改用獲准的 DNS 位址，或先設定 `DNSResolver`，再公告路由器的 LAN 位址。

## 先用本機命令檢查

以具有 `sudo` 權限的本機使用者執行下列獨立檢查；它們不需要 daemon，也不會套用網路變更。

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/example-basic-ipv4-nat.yaml
sudo routerd apply --config examples/example-basic-ipv4-nat.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

服務未啟動時，不要用 `routerctl validate` 或 `routerctl plan` 代替。

真實服務運行後，才可查詢資源。首次安裝請保留 `sudo`；若管理員透過 `routerd` 群組授予本機 socket 存取權，加入後必須重新登入才會生效：

```bash
sudo routerctl get status
sudo routerctl describe DHCPv4Client/wan-dhcpv4
sudo routerctl describe NAT44Rule/lan-to-wan
```

## 關於防火牆

完整範例也包含 `FirewallZone` 和 `FirewallPolicy`，用來展示 NAT 與區域策略如何搭配。不過防火牆功能仍在預發布的基礎實作階段，不是完整安全邊界。NAT 成功、dry-run 成功，或看到 nftables 表存在，都不能證明對外曝露已安全。

## 常見調整

- 將 `ens18`、`ens19` 換成真實介面名稱。
- 若 `192.168.10.0/24` 與上游、VPN 或管理網路重疊，改用未使用的私有網段。
- 若需要可信的訪客隔離，請採用 VLAN、獨立 SSID 或獨立實體埠；不要只依賴共用二層 LAN 上的 MAC 分類。
