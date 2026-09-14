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
| バージョン | **v20260914.0729** |
| 位置づけ | 現在の推奨安定版 |
| Release page | [v20260914.0729](https://github.com/imksoo/routerd/releases/tag/v20260914.0729) |
| 稼働実績 | Release CI成功。公式ISOの10台展開と、公式アーカイブによる家庭用ルーター2台の更新・検証完了報告。検証範囲は下記参照。 |
| バイナリ | 静的リンク（`CGO_ENABLED=0`）。固定名 archive と版番号付き archive を公開 |

## 検証範囲

運用者の公式版展開完了報告に基づき、**v20260914.0729**を推奨安定版に指定しました。

- ISOの10台：BGP、runtime doctor、サービス状態、SAM双方向ICMP/TCP正常。VRRP切替中もAPI HTTP正常。記録：`docs/routerd-v20260914.0729-rollout-log.md`、Forgejo commit `317fb30`。
- 家庭用ルーター2台：VIP、DHCP、DNS、DS-Lite 4経路、native nDPIを確認。HTTPS 360/360成功、doctor fail=0。記録：`evidence-20260914-release0729/result.md`。
- [候補版の障害注入記録](https://github.com/imksoo/routerd/pull/1252#issuecomment-5660295106)：隔離Linux netnsで観測エラー時の公開履歴保持を確認。

これは運用者から報告された検証結果であり、新たなAWS/Azure/OCI実機認証ではありません。ISO更新直後のパケット損失は15秒後の再試験で収束しました。以前の候補版のDNSタイムアウト1件は原因未確定です。更新中を含む全期間無損失を意味しません。切り戻し用旧ISOは保持されています。

## 安定版をインストールする

推奨安定版を使う場合は、固定 tag の URL を使います。

```sh
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/download/v20260914.0729/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

同じ release page には `routerd-v20260914.0729-linux-amd64.tar.gz` のような版番号付き archive もあります。

## 以前の安定版

直前の安定版 **v20260707.1514** はAWS/Azure/OCI/PVE冗長構成の過去試験でmatrix 56/56、provider収束4s、dataplane収束567s、cleanup state 0でした。この実績を今回の版の試験結果に読み替えません。

v20260627.1533 は以前の推奨安定版です。PVE ISO substrate 修正後の cost-bounded AWS/Azure/OCI/PVE single-topology baseline で、136 秒収束、matrix 12/12、全 leaf MobilityPool Ready、provider pending/failed 0、cleanup state 0 を確認しています。その時点に固定したい operator の rollback 候補としては有効ですが、新規導入は v20260914.0729 から始めてください。

## 既知の観測

- **API はまだ v1alpha1 です。** 安定版マイルストーンは、この build が本番運用品質であることを示しますが、resource schema の後方互換は約束しません。
- **設定は新しい schema に合わせて確認してください。** migration shim に頼らず、[変更履歴](./changelog.md) の各 release delta を確認してください。
- **`ManagementAccess` 未宣言の構成では `routerctl doctor mgmt` が SKIP になります。** これは稼働中 config の選択であり、release defect ではありません。

## インストールとアップグレード

手順は [インストールとアップグレード](../install-and-upgrade.md) を参照してください。
