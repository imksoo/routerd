# ADR 0009: 可插拔 overlay underlay（ipip / gre，然後 fou / gue）

![ADR 0009 的示意圖。TunnelInterface、IPIP 或 GRE 投遞、可選的 WireGuard 加密 underlay、MTU 開銷推導、MSS clamp 的安全性](/img/diagrams/adr-0009-pluggable-underlay.png)

## 狀態

已提議。核准為實驗性實作 — 2026-06-01。

以 CloudEdge overlay/SAM 資料平面（[ADR 0006](../adr/0006-event-federation.md)、
[Selective Address Mobility](../reference/selective-address-mobility)）和
zone 無關的 PMTU/MSS clamp（#53/#68）為基礎。實驗性。

## 背景

CloudEdge overlay（`OverlayPeer`）目前使用 **WireGuard** 作為唯一已實作的 underlay。
在*可信的*私有 underlay — ExpressRoute、DirectConnect、FastConnect、VPC/VNet 對等 —
上，WireGuard 的加密是冗餘的，約 80 位元組的開銷純粹是代價。當 underlay 已經可信時，
我們希望操作員能選擇更輕量、低開銷的 L3 傳輸，而**不改變**位址的投遞方式。

Overlay 已在適當的接縫處抽象（程式碼中已確認）：

- **投遞與 underlay 無關。** `hybrid.RouteTarget(peer)` 將
  `OverlayPeer.Underlay.Type` 對映到 `(device, gateway)`，`/32` 投遞路由
  （`RemoteAddressClaim` / `HybridRoute`）指向該裝置。新增傳輸只需
  新增 `switch` 分支。
- **MTU / MSS clamp 已參數化。** `hybrid.EstimateMTU = underlayMTU(interface)
  − overheadFor(type)`。zone 無關的 clamp 遵循 `EstimateMTU`。新傳輸只需
  開銷值和介面 MTU，clamp 即自動跟隨。

唯一實質的缺口：**裝置建立是 WireGuard 專有的**（專用的
`WireGuardInterface` Kind + 控制器）。新的 L3 傳輸需要
「建立隧道裝置」的等價資源 + 控制器。

## 決策

### 新 Kind `TunnelInterface`（`hybrid.routerd.net/v1alpha1`）

`WireGuardInterface` 的鏡像：一個 OS 隧道裝置的 desired state 資源。
`OverlayPeer.Underlay` 保持為*投遞選擇*的參考。`TunnelInterface` 是
*裝置 desired state* — 清晰的分離（`OverlayPeer` 的內聯欄位會
為每個 peer 增殖裝置規格，使裝置的所有權/冪等性/刪除變得模糊）。

Phase 1 欄位：

- `mode`: `ipip | gre`。
- `local`、`remote`: underlay（實體）端點 IP（必需）。
- `address`: overlay 內側位址（可選。否則與 WireGuard 相同，由
  `ipv4-static-address` 控制器設定）。
- `mtu`（可選）、`ttl`（可選，預設 64）、`key`（僅 GRE。設定時
  +4 開銷）。
- `trustedUnderlay: true` — **必需**（參見安全性）。

Phase 2 在同一 Kind 上擴充 IPIP-over-UDP：

- `mode`: `fou | gue` 表示帶 Linux UDP 封裝（`encap fou` 或 `encap gue`）的
  `ipip` 隧道裝置。
- `encapSport`、`encapDport`: 本機 UDP 來源/監聽埠和 peer 目的埠。
  `fou`/`gue` 時兩者必需。

`OverlayPeer.Underlay.Type` 列舉增加 `ipip`、`gre`、`fou`、`gue`。
`.Interface` 按名稱參考 `TunnelInterface`。

### 新控制器 `tunnel`

reconcile `TunnelInterface` 的 `framework.FuncController`（Phase 1 僅 Linux。
其他平台報告 unsupported 狀態而非使鏈報錯）：

- **基於 argv 的 `ip` 呼叫**（非字串拼接 shell）。`ip link show` →
  add/modify/`ip link del` 實現冪等：
  - `ip link add <dev> type ipip|gre local <L> remote <R> ttl <t> [key <k>]`
  - `fou`/`gue` 時：`ip fou add port <sport> ipproto 4|gue`，然後
    `ip link add <dev> type ipip local <L> remote <R> ttl <t> encap fou|gue
    encap-sport <sport> encap-dport <dport>`
  - `ip link set <dev> mtu <m> up`
- 位址由現有 `ipv4-static-address` 控制器處理（與 WireGuard 相同）。
- 狀態: phase、device、mode、local、remote、mtu。

### 開銷、投遞、MTU

- `overheadFor`: `ipip = 20`、`gre = 24`（外層 IPv4 20 + GRE base 4）、`fou = 28`
  （外層 IPv4 + UDP）、`gue = 32`（外層 IPv4 + UDP + 最小 4 位元組 GUE 標頭）。
  GRE `key` 時 +4。
- `RouteTarget`: `ipip`、`gre`、`fou`、`gue` → `(device, "")`（`/32` 路由
  與 WireGuard 相同指向隧道裝置）。
- `EstimateMTU` 和 PMTU/MSS clamp 自動跟隨。`pathMTUResourceMTU` 回退中
  增加 `TunnelInterface` 預設值（或 `spec.mtu` 生效）。

### 驗證

- `OverlayPeer.Underlay.Type` 列舉 += `ipip`、`gre`、`fou`、`gue`。
- `TunnelInterface`: `mode ∈ {ipip, gre, fou, gue}`。`local`/`remote` 必需，有效 IP。
  `trustedUnderlay == true` 必需（否則以清晰訊息拒絕）。
  MTU/TTL/key/encap 埠的範圍檢查。

## 安全性（硬性不變量）

`ipip`、`gre`、`fou`、`gue` **既不加密也不驗證** — 與 WireGuard 根本不同。
僅在已可信的 underlay 上才安全。

- **WireGuard 保持為預設。**
- `TunnelInterface` 除非設定 **`trustedUnderlay: true`** 否則被拒絕 —
  操作員對 underlay 為明文的明確確認。僅靠文件/doctor 的
  警告太弱。這是驗證閘門。

## 階段劃分

- **Phase 1**: `TunnelInterface` Kind + `tunnel` 控制器
  （Linux `ipip`/`gre`）+ `trustedUnderlay` 閘門 + `RouteTarget`/開銷/MTU +
  驗證 + 單元/fixture 測試 + 範例組態。測試包含
  **刪除順序**不變量：`OverlayPeer`/claim 刪除使 `/32` 路由下線，
  `TunnelInterface` 刪除輸出裝置刪除計畫。路由安裝
  需要容許裝置不存在的情況。
- **Phase 2（已實作）**: `fou` / `gue`（IPIP-over-UDP）。GRE-over-FOU/GUE
  有意不公開。需要 inner-mode 欄位或複合型別字串。
  增加 `ip fou add` 的 encap-port 設定。最小標頭開銷假設
  連同現有的顯式 `mtu` 逃脫艙口一起記錄。
- **Phase 3**: FreeBSD（`gif` for ipip、`gre`）— 組態/狀態介面不同，
  不塞入 Linux 控制器。
- **Phase 4**: 防火牆自動打洞（raw `ipip` = IP proto 4、`gre` = IP proto 47、
  `fou`/`gue` = UDP）+ `doctor hybrid` 檢查。

## 結論

- 操作員為可信 underlay 取得輕量的 overlay 傳輸。
  投遞和 MSS clamp 無需更改，自動跟隨新的開銷。
- 加密的權衡是明確的且有閘門控制（`trustedUnderlay: true`），
  不會在不可信路徑上誤選輕量傳輸。
- `TunnelInterface` 是通用的裝置 desired state 資源，
  Phase 2-3 可擴充（encap、FreeBSD）而無需觸及投遞/MTU 接縫。
- WireGuard 的行為和現有部署不受影響（預設不變）。
