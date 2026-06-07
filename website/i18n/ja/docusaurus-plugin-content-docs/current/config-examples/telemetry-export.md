---
title: OTLP コレクターへのテレメトリ送出
sidebar_position: 110
---

# OTLP コレクターへのテレメトリ送出

![routerd の log、metric、trace、service attribute を Telemetry resource から OTLP collector へ export する構成](/img/diagrams/config-example-telemetry-export.png)

routerd のテレメトリを OpenTelemetry コレクターへ送る例です。
長時間運転、health check、DPI、apply のレイテンシーの観測に使えます。

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

| 番号 | 意味 | 主な resource |
| --- | --- | --- |
| [1] | logs、metrics、traces を出す routerd プロセス。 | `Telemetry/otlp` |
| [2] | OTLP コレクターのエンドポイント。 | `Telemetry.spec.otlp.endpoint` |
| [3] | コレクターが転送する先のバックエンド。 | routerd 管理外 |

## 要点

```yaml
# [1] routerd telemetry export を有効にする。
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: otlp
  spec:
    # [2] OTLP collector のエンドポイント。
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

## 確認

```bash
routerd validate --config examples/telemetry-export.yaml
routerctl describe Telemetry/otlp
```

コレクターやバックエンド側でデータが届いていることを確認します。
エンドポイントは信頼できる管理網または観測用ネットワークに置いてください。
