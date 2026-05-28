---
title: routerctl doctor
sidebar_label: routerctl doctor
---

# routerctl doctor — 執行時健康診斷

`routerctl doctor` 執行一組唯讀檢查，回報目前 routerd 是否作為家庭閘道器正常運作。
不會變更主機狀態。面向維運人員、CI、監控代理與下游工具（Prometheus exporter、
Web Console、LLM 輔助診斷等）。

## 使用方式

```sh
# 全部 area（預設）
routerctl doctor

# 單一 area
routerctl doctor dns

# 不執行主機指令（只看資源 status）
routerctl doctor --no-host

# 機器可讀輸出
routerctl doctor -o json
routerctl doctor -o yaml
```

選項與 `diagnose` 一致：`--config`, `--state-file`,
`--no-host` / `--host`, `-o` / `--output`, `--timeout`。

## Areas

| Area | 檢查內容 |
| --- | --- |
| `wan` | `EgressRoutePolicy` 與 `HealthCheck` 的資源 status；IPv4 / IPv6 預設路由（`ip -4/-6 route show default`）。 |
| `dns` | `DNSResolver` 的資源 status；透過 `dig @127.0.0.1` 進行 A 紀錄探測。 |
| `dslite` | `DSLiteTunnel` 的資源 status；AFTR FQDN 的 AAAA 探測；tunnel device 存在性（`ip link show`）。 |
| `dhcpv6-pd` | `DHCPv6PrefixDelegation` 的 status（Bound、委派前綴）。PD 未取得時依設計為 **WARN**（不在 LAN 廣告壞掉的 IPv6）。 |
| `nat` | `NAT44Rule` 的資源 status；`nft list table ip routerd_nat` 存在。 |
| `firewall` | `FirewallZone` / `FirewallPolicy` 的 status；`nft list table inet routerd_filter` 存在且 input 鏈為 `policy drop`（否則視為 permissive）。 |
| `rollback` | 至少存在一個已儲存世代，讓 `routerctl rollback --to` 可使用。 |
| `disk` | `/var/lib/routerd` 與 `/run/routerd` 的容量。≥90% 或 `<256 MiB` 時 WARN，≥98% 或 `<64 MiB` 時 FAIL。 |
| `mgmt` | 管理介面的存在性（從 `ManagementAccess` 或 `FirewallZone role=mgmt` 推斷）；WebConsole 的 bind（`0.0.0.0` / `::` 為 WARN/FAIL）。 |
| `reconcile` | 從唯讀狀態 socket 讀取每個 controller 的 reconcile 失敗歷史。`--since <duration>` 限定時間視窗。視窗內 ≥1 筆為 WARN，≥10 筆為 FAIL；detail 中最多顯示 5 筆樣本。 |
| `runtime` | 從唯讀狀態 socket 讀取 routerd 自身的 heap / goroutine / fd：`heapAlloc`、`heapObjects`、`numGoroutine`、`numGC`、`openFds`/`maxFds`。當 `numGoroutine` 超過 10000，或開啟的 fd 達到 `RLIMIT_NOFILE` 的 80% 以上時為 WARN。僅作觀測，不會 FAIL。 |

每個檢查回傳 `pass`、`warn`、`fail`、`skip`（資源或訊號不存在）其一。

## JSON 輸出契約

`routerctl doctor -o json` 是**穩定的**機器可讀介面。形式：

```jsonc
{
  "summary": {
    "overall": "pass",      // "pass" | "warn" | "fail" | "skip"
    "pass": 7,
    "warn": 1,
    "fail": 0,
    "skip": 2
  },
  "checks": [
    {
      "area":   "dns",                          // 見上方 Areas 表
      "name":   "DNSResolver/lan-resolver",     // 人類可讀對象名稱
      "status": "warn",                         // "pass" | "warn" | "fail" | "skip"
      "detail": "phase=Degraded,waiting=...",   // 可選
      "remedy": "wait for or repair dependency wan-pd" // 可選
    }
    // ...
  ]
}
```

保證：

- `summary.overall` 取 `checks[].status` 的最差值（`fail` > `warn` > `unknown`/`skip` > `pass`）。
- `summary.pass/warn/fail/skip` 為整數計數，總和等於 `len(checks)`。
- `checks[].status` 僅為 `pass`、`warn`、`fail`、`skip`（不會出現其他值）。
- `checks[].area` 取自 Areas 表的穩定識別字集合。
- `checks[].name` 為人類可讀，請勿對其精確形式做樣式比對。
- `detail` / `remedy` 為可選的自由文字，面向維運人員。

## 結束碼

- `0` — 沒有 `fail` 檢查（`pass`、`warn`、`skip` 都不視為失敗）。
- 非 0 — 至少一個 `fail`。可寫成 `routerctl doctor || alert`。

`warn` 不會讓結束碼變為非 0（例如開機後 DHCPv6-PD 尚未 Bound 這類資訊性情況）。
若要更嚴格的閘門，請明確選擇 area（如 `routerctl doctor wan` 只在 `wan` fail 時非 0）。

## 穩定性

JSON 形式、area 識別字與 status 列舉是 v1alpha1 的維運契約。後續版本**可能新增 area 或可選欄位**，
但既有的 area 名稱與 status 取值在 v1alpha1 的 minor 版本之間不會改名或改變用途。

## 相關項目

- [調整（reconcile）與回滾](./reconcile.md)
- [`routerctl ledger` 維護](./reconcile.md#刪除)
