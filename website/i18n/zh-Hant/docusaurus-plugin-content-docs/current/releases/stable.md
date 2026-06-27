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
| 版本 | **v20260627.1533** |
| 定位 | 推薦穩定版（post-1107 SAM baseline 加上 operator 輸出整理） |
| 運行實績 | AWS/Azure/OCI/PVE single-topology baseline 136 秒收斂，matrix 12/12，全部 leaf MobilityPool Ready，provider pending/failed 0，cleanup state 0 |
| 二進位 | 靜態連結（`CGO_ENABLED=0`），通過 CI 和 Release 工作流程 |

## 推薦 v20260627.1533 的理由

v20260627.1533 在 v20260627.1107 的 SAM baseline 上整理了 `routerctl mobility explain` 與 `routerctl action list` 的 operator 輸出。PVE substrate 採用 qnap-backed live ISO/config media、DHCP/QGA 管理與 ens19 capture interface 後，AWS/Azure/OCI/PVE fresh single-topology baseline 已通過。

release manifest 記錄在 `docs/releases/manifests/v20260627.1533.yaml`。

### Pair-stable addressing（#330, #331）

`SAMTransportProfile` 新增 `spec.addressingMode: pair-stable`，使用 inner prefix 和 canonical peer key 的 fnv64a 雜湊實現確定性的 /31 slot 分配。

- **緊湊設定撰寫。** leaf 節點不再需要 `topologyNodeRefs`，消除了逐節點重複的拓撲宣告。svnet1 設定減少約 100 行。
- **拓撲變更穩定性。** 新增或刪除節點不會重新分配現有 peer 的位址（與依賴排序順序的 `edge-index` 不同）。
- **向後相容。** 現有的 `edge-index`（預設）設定不受影響。
- **碰撞偵測。** `routerd validate` / `routerctl validate` 在設定時偵測 /31 slot 雜湊碰撞。

### 從 v20260608.0642 繼承的事項

繼承 v20260608.0642 的全部特性：ADR 0014 CLI 重新設計、DNS VRRP VIP 支援、forcefrag prerouting 修復、BGP peer watch 穩定化及所有先前的生產安全修復。

## 已知觀測（非發布阻塞項）

- **`install.sh` 後 `routerd-bgp` 可能仍以舊 inode 運行。** 這是設計如此。`install.sh` 在升級時不自動重啟 `routerd-bgp`，以便已建立的 BGP 會話和 ECMP 在 routerd 二進位更新後存活。
- **未宣告 `ManagementAccess` 的設定中 `routerctl doctor mgmt` 顯示 SKIP。** 這是運行設定的選擇，非發布缺陷。

:::warning 升級注意
- **從 v20260528.2308 升級時：** ADR 0014 變更了 CLI verb 體系。`routerd apply` → `routerctl apply`、`routerd validate` → `routerctl validate` 等。如果服務單元或指令碼中使用了舊命令，請重寫。`install.sh` 會自動部署新的服務單元，因此 systemd 管理的單元會自動更新。
- **務必先 `cd` 到解壓後的發布目錄再執行 `install.sh`。**
- **從 v20260523.1542 及更早版本升級時：** `disabled:` 欄位（請用 `enabled: false`）和 `--controller-chain*` / `--observe-interval` 旗標已刪除。
- **DNS 解析器服務單元化：** 解析器以 `routerd-dns-resolver@<name>.service` 運行。首次升級時會有短暫的 DNS 中斷。
:::

## 「穩定版」的意義與注意

:::warning API 仍為 v1alpha1
「穩定版里程碑」表示**該版本的品質達到了生產可用的水準**，但**不承諾 API（資源 schema）的向後相容**。
:::

- routerd 的資源 API 目前為 **v1alpha1**。版本間**可能出現破壞性變更**。
- 升級時請勿依賴向後相容。請以**按照新 schema 重寫設定（YAML）**為前提進行。
- 策略上不提供遷移相容層。各版本的變更請查閱[變更日誌](./changelog.md)。

## 安裝與升級

安裝步驟請參閱[安裝與升級](../install-and-upgrade.md)。建議以推薦的里程碑版本為升級起點。
