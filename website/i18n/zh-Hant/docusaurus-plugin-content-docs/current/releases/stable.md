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
| 版本 | **v20260525.0112** |
| 定位 | 推薦穩定版（取代 v20260523.1542） |
| 運行實績 | 已在正式環境路由器（homert02）上運行；維持 BGP 2-way ECMP，並可透過 graceful restart 以零中斷升級 |
| 二進位檔 | 靜態連結（`CGO_ENABLED=0`），通過 CI 與 Release workflow |

## 推薦 v20260525.0112 的理由

- **啟動初期不再出現 DNS 中斷。** `DNSResolver` 現在不再等待所有相依解析完成才啟動，而是部分啟動：先以已解析的 listen 位址與 forward 來源開始回應，其餘仍在等待時回報 `phase: Degraded` 與 `waiting` 清單，待 DHCPv6 前綴委派抵達後收斂為 `Applied`。舊版本會在等待 PD 的啟動視窗期間拒絕 DNS。
- **完整具備 BGP 控制平面成果。** routerd 不使用 FRR，由自有的 `routerd-bgp` 常駐程式維護 eBGP peer；next-hop 改寫修正（#26）即使上游廣告第三方 next-hop 也能維持 2-way ECMP，且 Alpine/OpenRC 的 live ISO 會在 OpenRC 下啟動 `routerd-bgp`（#28）。
- **升級不再擾動 BGP。** `install.sh` 在二進位升級時不再自動重啟 `routerd-bgp`，因此 eBGP 工作階段與 ECMP 可跨 routerd 更新保持。
- **維運更輕鬆。** `routerd rollback --list` / `--to <generation>` 可重新套用已儲存的設定世代，`routerctl set-log-level` 可在執行時變更日誌詳細度，`routerctl describe` 會顯示 Phase、Reason、Message 及修復提示。
- **非 root 取得 status。** 唯讀 status socket 以 `root:routerd`、模式 `0o660` 建立，因此屬於 `routerd` 群組的維運人員無需 sudo 即可執行 `routerctl status`。
- **已在正式環境（homert02）運行**，以靜態二進位檔（`CGO_ENABLED=0`）發布，並通過 CI 與 Release workflow。

:::warning 從 v20260523.1542 或更早版本升級
此里程碑移除了 `disabled:` 欄位（請改用 `enabled: false`）以及無作用的 `--controller-chain*` / `--observe-interval` 旗標。請重寫使用了 `disabled:` 的設定，並在升級前更新仍傳入已移除旗標的主機 service unit。
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
