---
title: 定義防火牆區域
---

# 定義防火牆區域

## 適用情境

「WAN 無法到達 LAN、LAN 可以到達 WAN、管理路徑可以到達所有地方」，這是家庭與 SOHO 路由器的基本策略矩陣。
若以個別的 `accept` / `drop` 規則來撰寫，不僅重複性高，也容易出錯。

## routerd 的解決方式

使用 `FirewallZone` 將介面與**角色（role）**綁定。
routerd 會根據內建的角色矩陣，自動推導各方向的預設動作，因此在典型配置下，甚至不需要撰寫任何 `FirewallRule`。

| role | 用途 |
| --- | --- |
| `untrust` | WAN 側（上游線路、DSLite 隧道、PPPoE 虛擬介面） |
| `trust` | 一般 LAN 區段 |
| `mgmt` | 帶外管理網路 |

隱含的矩陣如下：

| from \ to | self | trust | mgmt | untrust |
| --- | --- | --- | --- | --- |
| `mgmt` | accept | accept | n/a | accept |
| `trust` | accept | accept | drop | accept |
| `untrust` | drop | drop | drop | n/a |
| `self` | accept | accept | accept | accept |

established/related 的連線一律允許。

## 範例

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: wan
  spec:
    role: untrust
    interfaces:
      - Interface/wan
      - DSLiteTunnel/ds-lite-primary

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: lan
  spec:
    role: trust
    interfaces:
      - Interface/lan

- apiVersion: firewall.routerd.net/v1alpha1
  kind: FirewallZone
  metadata:
    name: management
  spec:
    role: mgmt
    interfaces:
      - Interface/mgmt
```

對於典型的家庭路由器，這樣就已足夠。`FirewallRule` 只在需要表達例外時才新增。

## 相關項目

- [新增防火牆例外規則](./firewall-rule.md)
- [以 MAC 位址隔離訪客裝置](./guest-mode.md)
- [防火牆概念說明](../concepts/firewall.md)
