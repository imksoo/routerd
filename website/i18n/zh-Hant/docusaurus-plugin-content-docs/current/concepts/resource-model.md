---
title: 資源模型
slug: /concepts/resource-model
sidebar_position: 3
---

# 資源模型

routerd 的設定由最上層的 `Router` 以及其下並列的多個資源所構成。
每個資源的格式與 Kubernetes 相近。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DHCPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
```

## 共通欄位

- `apiVersion`：資源所屬的 API 群組與版本。
- `kind`：資源的種類（Kind）。
- `metadata.name`：在相同 `kind` 中唯一的名稱。
- `spec`：使用者宣告的意圖。
- `status`：routerd 或專屬常駐程式觀測到的狀態。

設定檔中主要撰寫的是 `spec`。
`status` 則透過控制 API、狀態資料庫，以及常駐程式的 `/v1/status` 來確認。

## API 群組

routerd 使用以下 API 群組。

| 群組 | 用途 |
| --- | --- |
| `routerd.net/v1alpha1` | 最上層的 `Router` |
| `net.routerd.net/v1alpha1` | 介面、DHCP、DNS、路由、通道、WAN 選擇、連線流量記錄 |
| `firewall.routerd.net/v1alpha1` | 防火牆區域、政策、規則、記錄 |
| `system.routerd.net/v1alpha1` | 主機名稱、套件、sysctl、網路接管、systemd 單元、NTP、記錄轉送、Web 管理介面 |
| `plugin.routerd.net/v1alpha1` | 受信任的本地插件 |

不使用 `routerd.io` 這類臨時群組名稱。

## 相依關係

資源以名稱互相參照。
例如 `IPv6DelegatedAddress` 參照 `DHCPv6PrefixDelegation`，`DSLiteTunnel` 參照 `DHCPv6Information` 和 `DNSResolver` 的結果。

當被參照的資源尚未就緒時，資源會維持在 `Pending` 狀態。
待參照資源就緒後，會依序進入 `Applied`、`Bound`、`Up`、`Installed`、`Healthy` 等階段。

## dependsOn

部分資源可透過 `dependsOn` 指定套用的前置條件。
`dependsOn` 中需明確指定所參照的資源及其狀態欄位。

```yaml
dependsOn:
  - resource: DHCPv6PrefixDelegation/wan-pd
    phase: Bound
  - resource: Interface/lan
    phase: Up
```

若要使用其他資源的狀態值，不在一般欄位中撰寫運算式，而是使用
`deviceFrom`、`gatewayFrom`、`addressFrom`、`ipv4From`、`ipv6From`、
`prefixFrom`、`rdnssFrom`、`addressFrom` 等專用欄位。

```yaml
deviceFrom:
  resource: DSLiteTunnel/ds-lite
  field: interface
```

## 擁有參照

`ownerRefs` 表示某個資源從屬於另一個資源。
當父資源尚未就緒時，子資源不會持續輸出過時的設定。
這是一個重要機制，用於防止 DHCPv6-PD 遺失時遺留舊有的 LAN IPv6 設定。
依賴委派前綴的 LAN IPv6 位址、RA、DNS 記錄、DS-Lite，在父資源尚未就緒期間均不會輸出過時狀態。
