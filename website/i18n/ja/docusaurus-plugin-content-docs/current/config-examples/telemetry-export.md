---
title: OTLP コレクターへのテレメトリ送出
sidebar_position: 110
---

# OTLP コレクターへのテレメトリ送出

![routerd のログ、メトリクス、トレース、サービス属性を Telemetry リソースから OTLP コレクターへ送出する構成](/img/diagrams/config-example-telemetry-export.png)

routerd のテレメトリを OpenTelemetry コレクターへ送る例です。
長時間稼働、ヘルスチェック、DPI、apply のレイテンシーの観測に使えます。

完全な YAML は `examples/telemetry-export.yaml` にあります。

## 構成図

```mermaid
flowchart LR
  router["[1] routerd host"]
  collector["[2] OTLP collector<br/>collector.example.internal:4317"]
  backend["[3] metrics / logs / traces backend"]

  router --> collector --> backend
```

## 図の対応表

| 番号 | 意味 | 主なリソース |
| --- | --- | --- |
| [1] | logs、metrics、traces を出す routerd プロセス。 | `Telemetry/otlp` |
| [2] | OTLP コレクターのエンドポイント。 | `Telemetry.spec.otlp.endpoint` |
| [3] | コレクターが転送する先のバックエンド。 | routerd 管理外 |

## この例で管理するもの

| 領域 | routerd リソース |
| --- | --- |
| テレメトリの送出先 | `Telemetry/otlp` |
| サービスの識別情報 | `serviceNamespace`, `attributes` |
| シグナル | `logs`, `metrics`, `traces` |

## 設定の要点

```yaml
# [1] routerd のテレメトリ送出を有効にする。
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: otlp
  spec:
    # [2] OTLP コレクターのエンドポイント。
    otlp:
      endpoint: http://collector.example.internal:4317
      insecure: true
    serviceNamespace: routerd
    attributes:
      deployment.environment: lab
      site: example
    signals:
      - logs
      - metrics
      - traces
```

## daemon の前に確認する

以下は `sudo` を実行できる通常ユーザーを想定します。まだサービスを起動していないため、
`routerctl` ではなく `routerd` を使い、dry-run の状態ファイルは一時ディレクトリへ隔離します。

```sh
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/telemetry-export.yaml
sudo routerd apply --config examples/telemetry-export.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sudo sed -n "1,160p" "$LAB_DIR/status.json"
```

レビュー済みの設定を適用して `routerd.service` を起動した**後で**、
`sudo routerctl describe Telemetry/otlp` で routerd 側の状態を確認します。
コレクターやバックエンド側でデータが届いていることを確認します。
エンドポイントは信頼できる管理網または観測用ネットワークに置いてください。
