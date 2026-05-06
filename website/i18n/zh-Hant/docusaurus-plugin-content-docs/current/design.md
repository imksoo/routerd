---
title: 架構概覽
---

# routerd 架構概覽

本文件針對運維者與貢獻者，介紹 routerd 的設計意圖與內部結構。
日常使用請從 [教學](./tutorials/getting-started.md) 與 [How-to](./how-to/multi-wan.md) 開始。
資源定義請參照 [API 參考](./api-v1alpha1.md)。

---

## 1. routerd 的定位

routerd 是宣告式的路由器框架。
目標是用同一組 primitive 建立家用路由器、SOHO 路由器，以及小型資料中心邊界路由器。

具體鎖定的三個取代對象：

| 目標 | 範圍 | 所需階段 |
| --- | --- | --- |
| 取代家用路由器 | 1 台主機、1-2 條上行、1-3 個 LAN VLAN | H |
| 虛擬化 SDN 路由器 | 叢集內的 VXLAN / EVPN / underlay routing | C |
| Kubernetes 邊界 | 以 BGP 公告 Pod CIDR / LoadBalancer IP，終結 ingress | S → C |

三者皆以同一組宣告式 primitive 表達，可依用途逐步啟用功能。

### 1.1 機能階段（capability tier）

| tier | 用途 | 主要功能 |
| --- | --- | --- |
| **H**（Home） | 家用、小型辦公室 | WAN acquire（PD/RA/PPPoE/DHCPv4/DS-Lite）、LAN service（RA/DHCPv6/dnsmasq）、NAT44、firewall、`EgressRoutePolicy` |
| **S**（SOHO/分支） | 數個據點，VPN 為主 | + WireGuard / IPsec、VRF、VPN 上的 dynamic routing、commit-confirmed |
| **C**（Campus / 小型 DC） | 數十節點 | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、FRR 包裝 |
| **E**（Enterprise / SP） | 數百節點以上 | + 完整 BGP、MP-BGP L3VPN、segment routing、HA leader election |

primitive 從 H 到 E 共用，階段提升只是增加包裝對象（如 FRR）。

---

## 2. 執行環境

### 2.1 部署形式

routerd 鎖定虛擬機環境執行；嵌入式 appliance 為未來工作。

對虛擬化平台的需求：

- virtio NIC（vmxnet、ne2k 等不在範圍）
- 不依賴特權 kernel 模組（DPDK / XDP 為選用，不需 host passthrough）
- 以 console 與 SSH 維運
- 實驗時建議善用快照與複製

### 2.2 OS 策略

routerd 設計為 cross-OS：同一份 binary 與設定可對應多種 OS。

| OS | 強項 | 用途 |
| --- | --- | --- |
| **Linux（Ubuntu / Debian）** | systemd 標準、易取得、kernel 較新 | 開發與正式環境的主流 |
| **NixOS** | 宣告式 OS 與 routerd 高度契合，可重現 | 宣告式運維的主力 |
| **FreeBSD** | base 穩定、佔資源小、jail 隔離 | 長期運轉與低資源環境 |
| **Alpine** | 最小體積、musl、apk | 未來的最小設定檔 |

OS 差異由 `pkg/platform` 層吸收。
nftables ↔ pf、systemd-networkd ↔ rc.conf、systemd unit ↔ rc.d 之類的對應，由各 OS 的渲染器負責。

---

## 3. 整體架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│ 使用者                                                            │
│   /etc/routerd/*.yaml  +  routerctl CLI                          │
└─────────┬─────────────────────────────────────────┬───────────────┘
          │ inotify                          HTTP+JSON
          │ (僅通知)                          (顯式 apply)
          ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ routerd（單一 binary、跨 OS）                                       │
│                                                                   │
│   ConfigWatcher ──notify only──▶ Bus                              │
│   ConfigLoader ◀──explicit trigger───── routerctl apply           │
│                                                                   │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ Bus（in-process channel + SQLite event 永續層）            │   │
│   │  topics: routerd.<area>.<subject>.<verb>                  │   │
│   │  cursor: events.id (autoincrement)                        │   │
│   │  fanout: subscribe pattern match → controller channel     │   │
│   └─────┬─────────────────────────────────────────────────────┘   │
│         │                                                         │
│         ▼ Controllers（in-process reactor 群）                     │
│   PrefixDelegationCtrl / LANAddressCtrl / RAAnnouncerCtrl         │
│   DNSAnswerCtrl / DNSResolverCtrl / FirewallCtrl / RouteCtrl      │
│   EgressRouteCtrl / ServiceLifecycleCtrl / ConfigLoaderCtrl       │
│   EventRuleEngine / DerivedEventEngine                            │
│         │                                                         │
│         ▼ SQLite state DB（objects/events/artifacts/generations） │
└─────────┬─────────────────────────────────────────────────────────┘
          │ Unix socket HTTP+JSON                fsnotify (lease/snapshot)
          ▼                                            ▲
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1 source daemons（各為一個 process）                         │
│   routerd-dhcpv6-client / routerd-dhcpv4-client                   │
│   routerd-pppoe-client / routerd-dns-resolver                     │
│   routerd-healthcheck@<resource> / routerd-firewall-logger        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. 資源模型

routerd 的設定以資源集合表達。型態類似 Kubernetes，但 apiVersion 階層與 controller 結構更為簡潔。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: ds-lite-primary
  spec:
    aftrFQDN: gw.transix.jp
```

### 4.1 主要 apiVersion

| apiVersion | 職責 |
| --- | --- |
| `net.routerd.net/v1alpha1` | 網路功能（Link、IPv4Static、DSLite、PPPoE、EgressRoute、HealthCheck 等） |
| `dns.routerd.net/v1alpha1` | DNS（DNSZone、DNSResolver、DHCPv4Reservation 等） |
| `firewall.routerd.net/v1alpha1` | Firewall（FirewallZone、FirewallPolicy、FirewallRule、NAT44Rule 等） |
| `system.routerd.net/v1alpha1` | OS bootstrap（Package、SysctlProfile、SystemdUnit、NetworkAdoption、WebConsole 等） |
| `control.routerd.net/v1alpha1` | controller chain 與 routerctl 控制 API |

完整清單於 [API 參考](./api-v1alpha1.md)。

### 4.2 資源間的參照

當某資源要參照另一資源的 status 時，請使用型別化的 `*From` 欄位，避免字面值。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

`addressFrom`、`ipv4From`、`ipv6From`、`upstreamFrom`、`prefixFrom`、`rdnssFrom`、`gatewayFrom` 皆採同形。
依存關係（`dependsOn`）也同樣透過此機制宣告。

詳見 [資源模型](./concepts/resource-model.md) 與 [狀態與所有權](./concepts/state-and-ownership.md)。

---

## 5. Event bus 與 controller chain

routerd 在 in-process event bus 與多個 controller 之間搭配，將設定收斂為宣告的目標狀態。

### 5.1 Event bus

- in-process channel + SQLite 事件紀錄做永續
- topics 採 `routerd.<area>.<subject>.<verb>` 格式（例如 `routerd.dhcpv6.bind.changed`）
- subscribers 以 pattern match 接收
- 所有事件皆有 `events.id` cursor，重啟後可重新評估

### 5.2 Controller chain

每個 controller 共用 `framework.FuncController` 結構：

- `Subscriptions`：關注的 topic
- `Bootstrap`：啟動時的一次性初始化
- `PeriodicFunc`：冪等的定期再評估
- `ReconcileFunc`：事件到達時的狀態收斂

`eventedStore` 包裝確保狀態保存時必發 `routerd.resource.status.changed`，
下游 controller 因此能完成跨資源的依存解析。

### 5.3 Daemon contract

長時間執行的 OS process（DHCPv6 client、DNS resolver、healthcheck 等）以 **daemon** 而非 controller 形式運行。
daemon 與 controller chain 透過 Unix domain socket + JSON 通訊，並將自身狀態落入 `lease.json` 等檔案。

詳見 [reconcile loop 行為](./operations/reconcile.md)。

---

## 6. 設定檔運維

routerd 設定檔（預設 `/usr/local/etc/routerd/router.yaml`）以下列流程套用：

```
編輯 → routerctl validate → routerctl apply（或自動重新載入）
                              │
                              └─ controller chain 更新狀態 DB
                                 → daemon 重啟 / reload
                                 → OS 狀態（nftables / netlink / systemd）反映
```

設定檔強烈建議納入 git 管理。
變更請一律透過 routerd 套用，勿在主機直接執行 `nft add rule`、`ip route add`、`sysctl -w` 等臨時指令。
臨時變更會被下次 reconcile 還原，或更糟地造成 routerd 狀態 DB 與 OS 實狀態的偏移（drift）。

發現 drift 時，正確做法是於設定檔中表達後再 apply。
這樣才能讓設定檔 ↔ 狀態 DB ↔ OS 實狀態三者保持一致。

---

## 7. 觀測性與除錯

routerd 透過下列介面提供運轉狀態：

- `routerctl status`：各資源的 phase 一覽
- `routerctl describe <kind>/<name>`：單一資源的 spec、status、近期事件
- `routerctl events --topic <pattern> --resource <kind>/<name>`：tail bus event
- `routerctl plan --diff`：apply 前差異預覽
- Web Console（預設 `http://<mgmt-ip>:8080/`）：summary、events、connections、clients、firewall、config 之瀏覽
- `journalctl -u routerd.service -f | grep "routerd event"`：以 systemd journal 追蹤 bus event

紀錄依用途分為四個 SQLite 檔案：`events.db`（controller）、`dns-queries.db`（DNS resolver）、`traffic-flows.db`（conntrack/pf）、`firewall-logs.db`（NFLOG/pflog）。
詳見 [日誌儲存](./concepts/log-storage.md)。

---

## 8. 相關文件

- [何謂 routerd](./concepts/what-is-routerd.md)
- [資源模型](./concepts/resource-model.md)
- [設計理念](./concepts/design-philosophy.md)
- [apply 與 render](./concepts/apply-and-render.md)
- [狀態與所有權](./concepts/state-and-ownership.md)
- [reconcile loop](./operations/reconcile.md)
- [狀態 DB 運維](./operations/state-database.md)
- [API 參考 v1alpha1](./api-v1alpha1.md)
- [Plugin 協定](./plugin-protocol.md)
- [支援平台](./platforms.md)
