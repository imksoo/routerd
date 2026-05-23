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
| 版本 | **v20260523.1542** |
| 定位 | 推薦穩定版（取代 v20260522.1334） |
| 運行實績 | 已在正式環境路由器（homert02）上運行；維持 BGP 2-way ECMP，並可透過 graceful restart 以零中斷升級 |
| 二進位檔 | 靜態連結（`CGO_ENABLED=0`），通過 CI 與 Release workflow |

## 推薦 v20260523.1542 的理由

- **完整承襲 v20260522.1334 的 BGP 控制平面成果。** routerd 不使用 FRR，由自有的 `routerd-bgp` 常駐程式維護 eBGP peer；next-hop 改寫修正（#26）即使上游廣告第三方 next-hop，也能維持 2-way ECMP。
- **修正了 live ISO 的 BGP（#28）。** 在 Alpine/OpenRC 的 live ISO 上，受管理的 GoBGP 常駐程式（`routerd-bgp`）現在會在 OpenRC 下啟動，因此可從 live ISO 使用 BGP。v20260522.1334 此處有問題，因此不再推薦 1334，尤其是在 live ISO 上使用 BGP 時。
- **新增了內建 DPI classifier 與 NixOS renderer 修正。**
- **已在正式環境運行**，以靜態二進位檔發布，並通過 CI。

## 「穩定版」的定義與注意事項

:::warning API 仍為 v1alpha1
「穩定版里程碑」代表**此版本具備正式環境所需的品質**，並**不保證 API（資源 schema）的向下相容性**。
:::

- routerd 的資源 API 目前為 **v1alpha1**。**版本之間可能包含破壞性變更。**
- 升級時，請勿依賴向下相容性，應以**配合新 schema 重新撰寫設定（YAML）**為前提進行。
- 本專案不提供遷移相容層。各版本的變更內容請參閱[變更記錄（Changelog）](./changelog.md)。

## 安裝與升級

安裝程序請參閱[安裝與升級](../install-and-upgrade.md)。建議以推薦里程碑版本為起點進行升級。
