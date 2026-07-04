---
title: 架構概覽
---

# routerd 架構概覽

本文件針對運維者與貢獻者，概覽 routerd 的設計理念與內部結構。
個別功能的使用方式請參閱[教學](./tutorials/getting-started.md)與 [How-to](./how-to/multi-wan.md)，
資源定義請參閱 [API 參考文件](./api-v1alpha1.md)。

![routerd 架構圖：router YAML 與 routerctl 經過驗證、effective config、controller、SQLite state、renderer，最後形成擁有的 host artifact](/img/diagrams/routerd-architecture.png)

---

## 1. routerd 的定位

routerd 是宣告式的路由器框架。
目標是以相同的 primitive，建構家庭路由器、SOHO 路由器，以及小型資料中心的邊界路由器。

具體的取代對象有以下三種。

| 目標 | 覆蓋範圍 | 所需功能階段 |
| --- | --- | --- |
| 家庭路由器替換 | 1 台主機、1-2 條上行鏈路、1-3 個 LAN VLAN | H |
| 虛擬化環境的 SDN 路由器 | 叢集內的 VXLAN / EVPN / underlay routing | C |
| Kubernetes 叢集的邊界 | 以 BGP 公告 Pod CIDR / LoadBalancer IP，終結 ingress | S → C |

三者皆以相同的宣告式 primitive 表達，可依用途逐步啟用功能。

### 1.1 功能階段（capability tier）

| tier | 用途 | 主要功能 |
| --- | --- | --- |
| **H**（Home） | 家庭、小型辦公室 | WAN acquire（PD/RA/PPPoE/DHCPv4/DS-Lite）、LAN service（RA/DHCPv6/dnsmasq）、NAT44、防火牆、`EgressRoutePolicy` |
| **S**（SOHO/分支） | 多據點、以 VPN 為主 | + WireGuard / IPsec、VRF、VPN 上的動態路由、commit-confirmed |
| **C**（Campus / 小型 DC） | 數十節點 | + EVPN-VXLAN、iBGP RR、BFD、RouteMap DSL、更進階的路由策略 |
| **E**（Enterprise / SP） | 數百節點以上 | + 完整 BGP、MP-BGP L3VPN、segment routing、HA leader election |

primitive 從 H 到 E 共用，功能階段提升只是增加路由與策略的控制器。

---

## 2. 執行環境

### 2.1 部署形式

routerd 以虛擬機執行為主要設計對象，嵌入式實體設備的支援留待日後。

對虛擬化環境的需求如下。

- virtio NIC（vmxnet、ne2k 等不在支援範圍）
- 不依賴特權核心模組（DPDK / XDP 為選用，不需 host passthrough）
- 以 console 與 SSH 進行運維
- 驗證時建議善用快照與複製功能

### 2.2 OS 策略

routerd 以跨 OS 為前提設計，同一份 binary 與相同設定可支援多種 OS。

| OS | 強項 | 用途 |
| --- | --- | --- |
| **Linux（Ubuntu / Debian）** | systemd 標準、易取得、核心版本較新 | 開發與正式環境的主流 |
| **FreeBSD** | base 穩定、資源佔用小、jail 隔離 | 長期運轉與低資源環境 |

OS 之間的差異由 `pkg/platform` 層吸收。
nftables ↔ pf、systemd-networkd ↔ rc.conf、systemd unit ↔ rc.d 腳本等對應，由各 OS 的產生器（renderer）負責。

版本策略方面，routerd 採用 `vYYYYMMDD.HHmm` 格式的日期時刻型版號。舊有的 `0.x.y` 格式與 `yyyymmdd.N` 格式的預發行版號已停止使用。

---

## 3. 整體架構圖

```
┌─────────────────────────────────────────────────────────────────┐
│ 使用者                                                          │
│   /etc/routerd/*.yaml  +  routerctl CLI                          │
└─────────┬─────────────────────────────────────────┬───────────────┘
          │ inotify                          HTTP+JSON
          │ (僅偵測)                         (明確 apply)
          ▼                                         ▼
┌─────────────────────────────────────────────────────────────────┐
│ routerd (1 binary, multi-OS)                                      │
│                                                                   │
│   ConfigWatcher ──notify only──▶ Bus                              │
│   ConfigLoader ◀──explicit trigger───── routerctl apply           │
│                                                                   │
│   ┌──────────────────────────────────────────────────────────┐   │
│   │ Bus (in-process channel + SQLite events 持久層)           │   │
│   │  topics: routerd.<area>.<subject>.<verb>                  │   │
│   │  cursor: events.id (autoincrement)                        │   │
│   │  fanout: subscribe pattern match → controller channel     │   │
│   └─────┬─────────────────────────────────────────────────────┘   │
│         │                                                         │
│         ▼ 控制器（in-process reactor 群）                         │
│   PrefixDelegationCtrl / LANAddressCtrl / RAAnnouncerCtrl         │
│   DNSAnswerCtrl / DNSResolverCtrl / FirewallCtrl / RouteCtrl      │
│   EgressRouteCtrl / ServiceLifecycleCtrl / ConfigLoaderCtrl       │
│   EventRuleEngine / DerivedEventEngine                            │
│         │                                                         │
│         ▼ SQLite state DB (objects/events/artifacts/generations)  │
└─────────┬─────────────────────────────────────────────────────────┘
          │ Unix socket HTTP+JSON                fsnotify (lease/snapshot)
          ▼                                            ▲
┌─────────────────────────────────────────────────────────────────┐
│ Layer 1 source 常駐程式（各自為一個 process）                     │
│   routerd-dhcpv6-client / routerd-dhcpv4-client                   │
│   routerd-pppoe-client / routerd-dns-resolver                     │
│   routerd-healthcheck@<resource> / routerd-firewall-logger        │
└─────────────────────────────────────────────────────────────────┘
```

---

## 4. 資源模型

routerd 的設定以資源集合來描述。概念上類似 Kubernetes，但 apiVersion 的層次與控制器結構更為簡潔。

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
| `net.routerd.net/v1alpha1` | 網路功能（Interface、IPv4Static、DSLite、PPPoE、EgressRoute、HealthCheck 等） |
| `dns.routerd.net/v1alpha1` | DNS（DNSZone、DNSResolver、DHCPv4Reservation 等） |
| `firewall.routerd.net/v1alpha1` | 防火牆（FirewallZone、FirewallPolicy、FirewallRule、NAT44Rule 等） |
| `system.routerd.net/v1alpha1` | OS 啟動設定意圖與覆寫（Package、SysctlProfile、WebConsole 等）。主機執行期成果物由資源自動推導。 |
| `control.routerd.net/v1alpha1` | 控制器鏈與 routerctl 的控制 API |

完整清單請參閱 [API 參考文件](./api-v1alpha1.md)。

### 4.2 資源間的參照

當某資源需要參照另一資源的 status 時，請使用型別化的 `*From` 欄位，而非直接寫入字面值。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: WebConsole
  spec:
    listenAddressFrom:
      resource: Interface/mgmt
      field: ipv4Addresses
    port: 8080
```

常見的參照格式包括：`addressFrom`、`ipv4From`、`ipv6From`、`prefixFrom`、`rdnssFrom`、`gatewayFrom` 等。
相依關係（`dependsOn`）也以相同機制宣告。

詳情請參閱[資源模型](./concepts/resource-model.md)與[狀態與擁有權](./concepts/state-and-ownership.md)。

---

## 5. Event bus 與控制器鏈

routerd 透過 in-process event bus 與多個控制器的組合，將系統收斂至宣告的期望狀態。

### 5.1 Event bus

- 以 in-process channel 加上 SQLite 事件日誌實現持久化
- topics 採 `routerd.<area>.<subject>.<verb>` 格式（例如：`routerd.dhcpv6.bind.changed`）
- 訂閱者以 pattern match 接收事件
- 所有事件均以 `events.id` 作為 cursor，重啟後仍可重新評估

### 5.2 控制器鏈

所有控制器共用 `framework.FuncController` 模式。

- `Subscriptions`：關注的 topic
- `Bootstrap`：啟動時執行一次的初始化
- `PeriodicFunc`：定期的冪等再評估
- `ReconcileFunc`：收到事件時的狀態收斂

`eventedStore` 包裝確保狀態保存時必然發出 `routerd.resource.status.changed`。
下游控制器因此能連鎖地再評估，完成跨資源的相依解析。

### 5.3 常駐程式契約

長時間執行的 OS process（DHCPv6 客戶端、DNS 解析器、健康檢查等）以**常駐程式**形式運行，而非控制器。
常駐程式透過 Unix domain socket 上的 JSON 與控制器鏈通訊，並將自身狀態持久化至 `lease.json` 等檔案。

詳情請參閱 [reconcile loop 的行為](./operations/reconcile)。

---

## 6. 設定檔運維

routerd 的設定檔（預設為 `/usr/local/etc/routerd/router.yaml`）以下列流程套用。

```
編輯 → routerctl validate → routerctl apply（或自動重新載入）
                              │
                              └─ 控制器鏈更新狀態 DB
                                 → 常駐程式重啟 / reload
                                 → OS 狀態（nftables / netlink / systemd）反映
```

強烈建議將設定檔納入 git 管理。
對正式主機的變更請一律透過 routerd 以宣告方式進行，勿直接在主機上執行 `nft add rule`、`ip route add`、`sysctl -w` 等臨時指令。
臨時變更會在下次 reconcile 時被還原，或更糟地在 routerd 狀態 DB 與 OS 實際狀態之間造成偏移（drift）。

發現偏移時，正確做法是在設定檔中表達後再 apply。
如此才能保持設定檔 ↔ 狀態 DB ↔ OS 實際狀態三者始終一致。

---

## 7. 可觀測性與除錯

routerd 提供以下方式觀測運轉狀態。

- `routerctl get status`：所有資源的 phase 一覽
- `routerctl describe <kind>/<name>`：單一資源的 spec、status 及近期事件
- `routerctl get events --topic <pattern> --resource <kind>/<name>`：tail bus event
- `routerctl plan --diff`：apply 前的差異預覽
- Web 管理介面（預設為 `http://<mgmt-ip>:8080/`）：在瀏覽器中查看 summary、events、connections、clients、firewall、config
- `journalctl -u routerd.service -f | grep "routerd event"`：以 systemd journal 追蹤 bus event

日誌依用途分為四個 SQLite 檔案持久保存：`events.db`（控制器產生）、`dns-queries.db`（DNS 解析器產生）、`traffic-flows.db`（conntrack/pf 產生）、`firewall-logs.db`（NFLOG/pflog 產生）。
詳情請參閱[日誌儲存](./concepts/log-storage.md)。

---

## 8. 相關文件

- [routerd 是什麼](./concepts/what-is-routerd.md)
- [資源模型](./concepts/resource-model.md)
- [設計理念](./concepts/design-philosophy.md)
- [套用與產生](./concepts/apply-and-render.md)
- [狀態與擁有權](./concepts/state-and-ownership.md)
- [reconcile loop](./operations/reconcile)
- [狀態 DB 運維](./operations/state-database.md)
- [API 參考文件 v1alpha1](./api-v1alpha1.md)
- [外掛程式通訊協定](./plugin-protocol.md)
- [支援平台](./platforms.md)
