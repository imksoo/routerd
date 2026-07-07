---
title: 安定版マイルストーン
sidebar_label: 安定版マイルストーン
sidebar_position: 0
---

# 安定版マイルストーン

routerd は `vYYYYMMDD.HHmm` 形式で頻繁にリリースします。その中から、節目ごとに**本番運用に推奨できる版**を安定版マイルストーンとして選びます。新規導入では、このページの版を使い、automation では release tag を固定してください。

## 現在の推奨版

| 項目 | 内容 |
| --- | --- |
| バージョン | **v20260707.1514** |
| 位置づけ | 現在の推奨安定版 |
| Release page | [v20260707.1514](https://github.com/imksoo/routerd/releases/tag/v20260707.1514) |
| 稼働実績 | Release workflow 通過、生成 config/control schema と website copy の一致確認、AWS/Azure/OCI/PVE の冗長化込み full topology 実機試験で 8 clients、8 leaves、AWS RR 2、matrix 56/56、provider 収束 4s、dataplane 収束 567s、cleanup state 0 |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）。固定名 archive と版番号付き archive を公開 |

## v20260707.1514 を推奨する理由

v20260707.1514 は、直近の steady-state runtime 修正を含み、実機 CloudEdge SAM qualification を通過したため、現在の安定版マイルストーンです。受理した run は AWS、Azure、OCI、PVE に冗長 leaf と AWS route reflector 2 台を置いた構成で、directed client matrix は `56/56` PASS でした。試験後の cleanup では OpenTofu resource 53 個を destroy し、state は 0 になっています。

schema についても repository と website の整合を確認済みです。

- `make check-schema` PASS。
- `make check-website-schemas` PASS。
- `schemas/routerd-config-v1alpha1.schema.json` と `website/static/schemas/routerd-config-v1alpha1.schema.json` は byte 一致。
- Control schema と Control OpenAPI の website copy も canonical file と byte 一致。

受理した full run の evidence summary は repository 外の次の場所に保存しています。

```text
/home/imksoo/routerd-labs-archive/evidence/rv202607071514-full-20260707T100026Z/e2e-baseline-awsprofile-retry1/summary.txt
```

## 安定版をインストールする

推奨安定版を使う場合は、固定 tag の URL を使います。

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

同じ release page には `routerd-v20260707.1514-linux-amd64.tar.gz` のような版番号付き archive もあります。

## 以前の安定版

v20260627.1533 は以前の推奨安定版です。PVE ISO substrate 修正後の cost-bounded AWS/Azure/OCI/PVE single-topology baseline で、136 秒収束、matrix 12/12、全 leaf MobilityPool Ready、provider pending/failed 0、cleanup state 0 を確認しています。その時点に固定したい operator の rollback 候補としては有効ですが、新規導入は v20260707.1514 から始めてください。

## 既知の観測

- **API はまだ v1alpha1 です。** 安定版マイルストーンは、この build が本番運用品質であることを示しますが、resource schema の後方互換は約束しません。
- **設定は新しい schema に合わせて確認してください。** migration shim に頼らず、[変更履歴](./changelog.md) の各 release delta を確認してください。
- **`ManagementAccess` 未宣言の構成では `routerctl doctor mgmt` が SKIP になります。** これは稼働中 config の選択であり、release defect ではありません。

## インストールとアップグレード

手順は [インストールとアップグレード](../install-and-upgrade.md) を参照してください。
