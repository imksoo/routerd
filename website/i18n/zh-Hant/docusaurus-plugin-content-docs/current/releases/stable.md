---
title: 穩定版里程碑
sidebar_label: 穩定版里程碑
sidebar_position: 0
---

# 穩定版里程碑

routerd 以 `vYYYYMMDD.HHmm` 格式頻繁發布版本。其中經過評估**可供正式環境使用**的版本，會在每個里程碑時選定為穩定版里程碑。新部署請使用本頁所列版本，並在自動化中固定 release tag。

## 目前推薦版本

| 項目 | 內容 |
| --- | --- |
| 版本 | **v20260914.0729** |
| 定位 | 目前推薦穩定版 |
| Release page | [v20260914.0729](https://github.com/imksoo/routerd/releases/tag/v20260914.0729) |
| 運行實績 | Release CI 成功；已回報完成 10 台官方 ISO 路由器及兩台使用官方封存檔的家用路由器升級驗證。範圍見下文。 |
| 二進位 | 靜態連結（`CGO_ENABLED=0`），同時發布固定名稱和帶版本號的 archive |

## 驗證範圍

根據維運人員提交的官方版本部署報告，將 **v20260914.0729** 指定為推薦穩定版。

- 10 台 ISO 路由器：BGP、runtime doctor、服務、SAM 雙向 ICMP/TCP 正常；VRRP 切換期間 API HTTP 正常。紀錄：`docs/routerd-v20260914.0729-rollout-log.md`，Forgejo commit `317fb30`。
- 兩台家用路由器：VIP、DHCP、DNS、四條 DS-Lite 路徑、native nDPI 正常；HTTPS 360/360，doctor fail=0。紀錄：`evidence-20260914-release0729/result.md`。
- [候選版本故障注入證據](https://github.com/imksoo/routerd/pull/1252#issuecomment-5660295106)：隔離 Linux netns 內驗證觀測錯誤不會遺失發布歷史。

這是維運人員回報的結果，不是新的 AWS/Azure/OCI 實機認證。ISO 更新後的初始丟包在 15 秒後的複測中已收斂；較早候選版本的一次 DNS 逾時仍未查明原因。不能據此聲稱整個升級過程零丟包。舊 ISO 已保留用於回復。

## 安裝穩定版

使用推薦穩定版時，請使用固定 tag URL：

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

同一 release page 也發布帶版本號的 archive，例如 `routerd-v20260914.0729-linux-amd64.tar.gz`。

## 上一個穩定版

前一穩定版 **v20260707.1514** 的歷史 AWS/Azure/OCI/PVE 冗餘測試為 matrix 56/56、provider 收斂 4s、dataplane 收斂 567s、cleanup state 0。這些結果屬於舊版，不是本版的新測試。

v20260627.1533 是上一個推薦穩定版。它在修正 PVE ISO substrate 後通過了 cost-bounded AWS/Azure/OCI/PVE single-topology baseline：136 秒收斂、matrix 12/12、全部 leaf MobilityPool Ready、provider pending/failed 0、cleanup state 0。需要固定到該里程碑的 operator 仍可將它作為 rollback 候選，但新部署應從 v20260914.0729 開始。

## 已知觀測

- **API 仍為 v1alpha1。** 穩定版里程碑表示該 build 達到生產可用品質，但不承諾 resource schema 向後相容。
- **請按新 schema 檢查設定。** 不要依賴 migration shim；請查看[變更記錄](./changelog.md)中的每個 release delta。
- **未宣告 `ManagementAccess` 的設定中 `routerctl doctor mgmt` 會顯示 SKIP。** 這是運行設定的選擇，不是 release defect。

## 安裝與升級

完整步驟請參閱[安裝與升級](../install-and-upgrade.md)。
