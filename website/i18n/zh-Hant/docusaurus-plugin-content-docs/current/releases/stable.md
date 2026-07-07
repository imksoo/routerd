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
| 版本 | **v20260707.1514** |
| 定位 | 目前推薦穩定版 |
| Release page | [v20260707.1514](https://github.com/imksoo/routerd/releases/tag/v20260707.1514) |
| 運行實績 | Release workflow 通過；產生的 config/control schema 與 website copy 一致；AWS/Azure/OCI/PVE 冗餘 full topology 實機測試通過：8 clients、8 leaves、2 個 AWS RR、matrix 56/56、provider 收斂 4s、dataplane 收斂 567s、cleanup state 0 |
| 二進位 | 靜態連結（`CGO_ENABLED=0`），同時發布固定名稱和帶版本號的 archive |

## 推薦 v20260707.1514 的理由

v20260707.1514 包含近期 steady-state runtime 修正，並通過了新的真實機器 CloudEdge SAM qualification，因此是目前穩定版里程碑。接受的 run 在 AWS、Azure、OCI 和 PVE 上使用冗餘 leaf 與兩個 AWS route reflector，directed client matrix 為 `56/56` PASS。測試後的 cleanup 銷毀了 53 個 OpenTofu resource，state 為 0。

schema 也已確認與發布網站一致：

- `make check-schema` PASS。
- `make check-website-schemas` PASS。
- `schemas/routerd-config-v1alpha1.schema.json` 與 `website/static/schemas/routerd-config-v1alpha1.schema.json` byte 一致。
- Control schema 與 Control OpenAPI 的 website copy 也與 canonical file byte 一致。

接受的 full run evidence summary 保存在 repository 外：

```text
/home/imksoo/routerd-labs-archive/evidence/rv202607071514-full-20260707T100026Z/e2e-baseline-awsprofile-retry1/summary.txt
```

## 安裝穩定版

使用推薦穩定版時，請使用固定 tag URL：

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

同一 release page 也發布帶版本號的 archive，例如 `routerd-v20260707.1514-linux-amd64.tar.gz`。

## 上一個穩定版

v20260627.1533 是上一個推薦穩定版。它在修正 PVE ISO substrate 後通過了 cost-bounded AWS/Azure/OCI/PVE single-topology baseline：136 秒收斂、matrix 12/12、全部 leaf MobilityPool Ready、provider pending/failed 0、cleanup state 0。需要固定到該里程碑的 operator 仍可將它作為 rollback 候選，但新部署應從 v20260707.1514 開始。

## 已知觀測

- **API 仍為 v1alpha1。** 穩定版里程碑表示該 build 達到生產可用品質，但不承諾 resource schema 向後相容。
- **請按新 schema 檢查設定。** 不要依賴 migration shim；請查看[變更記錄](./changelog.md)中的每個 release delta。
- **未宣告 `ManagementAccess` 的設定中 `routerctl doctor mgmt` 會顯示 SKIP。** 這是運行設定的選擇，不是 release defect。

## 安裝與升級

完整步驟請參閱[安裝與升級](../install-and-upgrade.md)。
