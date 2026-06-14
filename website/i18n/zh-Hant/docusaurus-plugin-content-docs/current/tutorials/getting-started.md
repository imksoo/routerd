---
title: 入門指南
---

# 入門指南

![從 interface discovery 與小型 YAML config 到 validate、plan、dry-run、serve、routerctl status 的安全 first routerd loop](/img/diagrams/tutorial-getting-started.png)

本教學首先確認安全的操作流程。

1. 撰寫小型的路由器資源檔。
2. 驗證。
3. 確認計畫。
4. 預演執行。
5. 確認安全後啟動常駐程式。

第一次確認時，不會變更主機的網路設定。
請先透過 release 封存檔與 `install.sh` 安裝 routerd。
各 OS 的安裝步驟請參閱[安裝與升級](../install-and-upgrade.md)。

## 1. 確認介面名稱

```bash
ip link
```

本教學以 WAN 為 `ens18`、LAN 為 `ens19`、管理用為 `ens20` 為例。
在實際主機上請務必根據自身環境替換。

請將管理路徑與要變更的介面分開。
若只對 routerd 將接管的介面進行初次驗證，風險較高。

## 2. 描述介面與主機準備

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: first-router
spec:
  resources:
    - apiVersion: system.routerd.net/v1alpha1
      kind: Package
      metadata:
        name: router-host-tools
      spec:
        packages:
          - os: ubuntu
            names: [dnsmasq, nftables, conntrack, iproute2]

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: wan
      spec:
        ifname: ens18
        adminUp: true
        managed: true

    - apiVersion: net.routerd.net/v1alpha1
      kind: Interface
      metadata:
        name: lan
      spec:
        ifname: ens19
        adminUp: true
        managed: true
```

路由器功能所需的主機端執行時期設定，routerd 會從宣告的資源中自動推導。
`Package`、`Sysctl`、`SysctlProfile` 僅作為補充尚無法自動推導的套件或核心設定的有限逃生口，請僅在必要時使用。

## 3. 驗證

```bash
routerctl validate -f first-router.yaml --replace
```

驗證步驟在 routerd 接觸主機之前，先確認資源的格式是否正確。

## 4. 確認計畫

```bash
routerctl plan -f first-router.yaml --replace
```

計畫步驟可確認介面名稱錯誤、缺少相依關係，以及將產生的主機成品。

## 5. 預演執行

```bash
routerctl plan -f first-router.yaml --replace
```

預演執行可確認資源載入、相依順序及產生內容。
不會確認網路變更。

## 6. 計畫安全後啟動常駐程式

```bash
sudo routerd serve --config first-router.yaml
```

在生產環境中，請使用產生的服務成品資源或 systemd unit 檔案。
這樣便能在系統啟動時自動執行 `routerd serve`。

## 7. 確認狀態

```bash
routerctl status
routerctl events --limit 20
routerctl connections --limit 50
```

下一篇教學將新增 LAN 的 DHCP、RA、DNS、路由政策、NAT44 與 DS-Lite。
