---
title: 穩定版里程碑
sidebar_label: 穩定版里程碑
sidebar_position: 0
---

# 穩定版里程碑

routerd 以 `vYYYYMMDD.HHmm` 格式頻繁發布版本，其中經過評估**可供正式環境使用**的版本，會在每個里程碑時選定為「穩定版里程碑」。初次部署時，請使用本頁所列的版本。

## 目前推薦版本

| 項目 | 內容 |
| --- | --- |
| 版本 | **v20260525.1631** |
| 定位 | 推薦穩定版（取代 v20260525.0112） |
| 運行實績 | 已在正式環境路由器（homert02）上運行；維持 BGP 2-way ECMP，DNS 解析器可跨 routerd 重新啟動持續回應，並可透過 graceful restart 以零中斷升級 |
| 二進位檔 | 靜態連結（`CGO_ENABLED=0`），通過 CI 與 Release workflow |

## 推薦 v20260525.1631 的理由

- **DNS 可跨 routerd 重新啟動持續回應。** `DNSResolver` 現在作為獨立的長壽命服務單元（`routerd-dns-resolver@<name>.service`）運行：重新啟動或升級 routerd 不再中斷 DNS，設定變更（包括 DHCPv6-PD 收斂）透過守護程序的 reload 端點就地生效而無需重新啟動程序，`routerctl restart-dns-resolver` 可明確復原。它在啟動時也會部分拉起：先以已解析的 listen 位址與 source 回應（`phase: Degraded` 與 `waiting` 清單）並收斂為 `Applied`，因此不存在等待前綴委派時拒絕 DNS 的啟動視窗。
- **完整具備 BGP 控制平面成果。** routerd 不使用 FRR，由自有的 `routerd-bgp` 常駐程式維護 eBGP peer；next-hop 改寫修正（#26）即使上游廣告第三方 next-hop 也能維持 2-way ECMP，且 Alpine/OpenRC 的 live ISO 會在 OpenRC 下啟動 `routerd-bgp`（#28）。
- **升級不再擾動 BGP 與 DNS。** `install.sh` 在二進位升級時不再自動重啟 `routerd-bgp` 或 DNS 解析器，因此 eBGP 工作階段、ECMP 與 DNS 可跨 routerd 更新保持。
- **維運更輕鬆。** `routerd rollback --list` / `--to <generation>` 可重新套用已儲存的設定世代，`routerctl set-log-level` 可在執行時變更日誌詳細度，`routerctl describe` 會顯示 Phase、Reason、Message 及修復提示。
- **非 root 取得 status。** 唯讀 status socket 以 `root:routerd`、模式 `0o660` 建立，因此屬於 `routerd` 群組的維運人員無需 sudo 即可執行 `routerctl status`。
- **已在正式環境（homert02）運行**，以靜態二進位檔（`CGO_ENABLED=0`）發布，並通過 CI 與 Release workflow。

:::warning 升級注意事項
- **從 v20260523.1542 或更早版本升級：** 已移除 `disabled:` 欄位（請改用 `enabled: false`）以及無作用的 `--controller-chain*` / `--observe-interval` 旗標。請在升級前重寫相關設定與主機 service unit。
- **DNS 解析器服務單元化：** 解析器現在作為 `routerd-dns-resolver@<name>.service` 運行。首次升級到此模式時會進行一次「子程序 → 單元」切換，期間有一次短暫的 DNS 中斷；此後 routerd 的重新啟動與升級不再中斷 DNS。
:::

## 「穩定版」的定義與注意事項

:::warning API 仍為 v1alpha1
「穩定版里程碑」代表**此版本具備正式環境所需的品質**，並**不保證 API（資源 schema）的向下相容性**。
:::

- routerd 的資源 API 目前為 **v1alpha1**。**版本之間可能包含破壞性變更。**
- 升級時，請勿依賴向下相容性，應以**配合新 schema 重新撰寫設定（YAML）**為前提進行。
- 本專案不提供遷移相容層。各版本的變更內容請參閱[變更記錄（Changelog）](./changelog.md)。

## 安裝與升級

安裝程序請參閱[安裝與升級](../install-and-upgrade.md)。建議以推薦里程碑版本為起點進行升級。
