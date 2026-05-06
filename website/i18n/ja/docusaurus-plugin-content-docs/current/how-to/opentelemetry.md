---
title: OTLP コレクターへテレメトリを送る
slug: /how-to/opentelemetry
---

# OTLP コレクターへテレメトリを送る

## シナリオ

ルーターのログ・メトリクス・トレースを、OpenTelemetry 互換のバックエンド (Grafana Loki/Tempo/Mimir、Datadog、Honeycomb、自前の `otelcol-contrib` 等) に送りたい。`journalctl` や `routerctl events` を毎回叩かずに、外部のダッシュボードで観測したい状態です。

routerd は常駐デーモン全てから OpenTelemetry エクスポートを行えます。コレクター本体は routerd のバイナリには同梱しません。あなたが既に運用している外部の OTLP エンドポイントを指定してください。routerd は OTLP/gRPC でデータを送ります。

## routerd が出すもの

| デーモン | service.name | 内容 |
| --- | --- | --- |
| `routerd` (制御プレーン) | `routerd` | `controller.reconcile` トレース、`routerd.controller.reconcile` カウンタ、構造化 slog ログ |
| `routerd-dhcpv6-client` | `routerd-dhcpv6-client` | DHCPv6 ライフサイクルのトレースと構造化ログ (Solicit/Request/Renew、リースイベント) |
| `routerd-dhcpv4-client` | `routerd-dhcpv4-client` | DHCPv4 ライフサイクルのトレースと構造化ログ |
| `routerd-pppoe-client` | `routerd-pppoe-client` | PPPoE セッションのライフサイクル |
| `routerd-healthcheck` | `routerd-healthcheck` | ヘルスチェック結果 (target 属性付きの成功/失敗) |

各デーモンはリソース属性に `routerd.resource.name` を付けるので、リソース単位 (例: WAN ごとの DHCPv6 クライアント) でシグナルを分離できます。

エクスポートは OTLP/gRPC です。logs / metrics / traces は既定で同じエンドポイントを共有しますが、必要なら信号ごとに別エンドポイントを指定できます。

## エクスポートの設定

routerd は OpenTelemetry の標準環境変数を読みます。routerd 独自の構文はありません。OTLP/gRPC エクスポーター上流が解釈する変数はそのまま使えます。

主な変数:

| 変数 | 用途 |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 全シグナル共通のエンドポイント (例: `http://collector.lan:4317`) |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` | 信号ごとの個別指定 |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` で TLS を無効化 (ラボ用) |
| `OTEL_EXPORTER_OTLP_HEADERS` | 例: マネージドバックエンド向け `Authorization=Bearer ...` |
| `OTEL_SERVICE_NAMESPACE` | 推奨: 全デーモンで `routerd` を共有 |
| `OTEL_RESOURCE_ATTRIBUTES` | サイト/ホスト属性などを `key=value,...` で自由に |

`OTEL_EXPORTER_OTLP_ENDPOINT` / `_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` のいずれも未設定なら、routerd はテレメトリ初期化自体をスキップします。デーモン側に有効化フラグはありません。「変数を設定しない」ことが OFF です。

### systemd 管理の routerd へ反映

Linux インストールでは systemd unit の環境に変数を入れます。upstream unit が更新されても消えないように drop-in が無難です:

```ini
# /etc/systemd/system/routerd.service.d/10-otel.conf
[Service]
Environment=OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.lan:4317
Environment=OTEL_EXPORTER_OTLP_INSECURE=true
Environment=OTEL_SERVICE_NAMESPACE=routerd
Environment=OTEL_RESOURCE_ATTRIBUTES=deployment.environment=home,host.name=edge-router
```

エクスポートしたい管理対象デーモンにも同じ drop-in を入れてください:

- `/etc/systemd/system/routerd-dhcpv6-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-dhcpv4-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-pppoe-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-healthcheck@.service.d/10-otel.conf`

そして:

```bash
sudo systemctl daemon-reload
sudo systemctl restart routerd.service \
                       'routerd-dhcpv6-client@*.service' \
                       'routerd-healthcheck@*.service'
```

### NixOS

routerd の NixOS モジュールが生成した各 systemd service の environment に変数を加えます:

```nix
systemd.services.routerd.environment = {
  OTEL_EXPORTER_OTLP_ENDPOINT = "http://collector.lan:4317";
  OTEL_EXPORTER_OTLP_INSECURE = "true";
  OTEL_SERVICE_NAMESPACE      = "routerd";
};
```

routerd が生成したデーモン用 service にも同じブロックを並べます。

### FreeBSD

routerd が出力する rc.d ラッパーの `command_args` 環境ブロックに変数を追加します (ラッパーが対応していれば `routerd_envfile=...` でも構いません)。

## 受信側を立てて検証する

OTLP/gRPC バックエンドなら何でも構いません。スモークテストには `otelcol-contrib` の `debug` exporter が一番手軽です:

```yaml
# /tmp/otel-test.yaml
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317

exporters:
  debug:
    verbosity: detailed

service:
  pipelines:
    logs:    { receivers: [otlp], exporters: [debug] }
    metrics: { receivers: [otlp], exporters: [debug] }
    traces:  { receivers: [otlp], exporters: [debug] }
```

```bash
otelcol-contrib --config /tmp/otel-test.yaml
```

routerd を再起動すると数秒以内に次が見えるはずです:

- `routerd.controller.reconcile` Sum メトリック (増加方向)
- `controller.reconcile` スパン (status OK)
- routerd の slog 出力が `LogRecord` として届く

`routerd` 本体の記録だけ届いてデーモン側が黙っているなら、デーモン用 drop-in が反映されているか、`daemon-reload` を打ったかを確認してください。

## トラブルシューティング

**デーモンの journal に `address family not supported by protocol` が出る。** routerd のハードン済み systemd unit はアドレスファミリを制限しています。コレクターが IPv4 経由なら (大抵そう) unit に `AF_INET` 許可が必要です。同梱テンプレートには既に入っています。古い drop-in で `RestrictAddressFamilies` を上書きしている場合は、`AF_INET AF_INET6` 両方が含まれていることを確認してください。

**コレクターに何も届かない。** routerd から到達できるホスト/IP か (`getent ahosts` と `nc -vz host port` で確認)、TLS 無しなら `OTEL_EXPORTER_OTLP_INSECURE=true` を入れたかを確認します。

**届いているが service.name がおかしい。** 各デーモンが自分自身の `service.name` を設定します。バックエンドでまとめたいなら `OTEL_RESOURCE_ATTRIBUTES=service.namespace=routerd,...` でグループ化してください。`service.name` 自体は上書きしないこと。

## routerd が出さないもの

- 同梱の OTLP コレクター。routerd の隣で別途立てるか、マネージドバックエンドを使ってください。
- 組み込みのストレージバックエンド。routerd はローカル可視化用に SQLite ログ DB (`events.db`, `dns-queries.db`, `traffic-flows.db`, `firewall-logs.db`) を持っており、Web Console から確認できます。OTLP エクスポートは「同じデータをホストの外へ送る」用途です。

## 宣言的な Telemetry リソース

`Telemetry` を使うと、OTLP エンドポイントを router YAML に書けます。routerd は対応する OpenTelemetry 環境変数を生成済みの systemd、NixOS、FreeBSD rc.d unit に入れます。コレクターは外部で用意します。routerd はエクスポーター設定だけを準備します。

```yaml
apiVersion: observability.routerd.net/v1alpha1
kind: Telemetry
metadata:
  name: otlp
spec:
  otlp:
    endpoint: http://collector.example.internal:4317
    insecure: true
  serviceNamespace: routerd
  attributes:
    deployment.environment: home
    site: edge
  signals: [logs, metrics, traces]
```
