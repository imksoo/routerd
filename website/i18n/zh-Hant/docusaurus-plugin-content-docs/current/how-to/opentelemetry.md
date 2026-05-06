---
title: 將遙測資料送到 OTLP 收集器
slug: /how-to/opentelemetry
---

# 將遙測資料送到 OTLP 收集器

## 情境

你想把路由器的日誌、指標與追蹤,送到任一 OpenTelemetry 相容的後端 (Grafana Loki/Tempo/Mimir、Datadog、Honeycomb、自架的 `otelcol-contrib` …),不必每次都去 `journalctl` 或 `routerctl events`。

routerd 的所有常駐 daemon 都能輸出 OpenTelemetry。**routerd 不內建收集器**;你要自己準備外部 OTLP 端點,routerd 會用 OTLP/gRPC 把資料送過去。

## routerd 會輸出什麼

| Daemon | service.name | 內容 |
| --- | --- | --- |
| `routerd` (控制平面) | `routerd` | `controller.reconcile` 追蹤、`routerd.controller.reconcile` 計數器、結構化 slog 日誌 |
| `routerd-dhcpv6-client` | `routerd-dhcpv6-client` | DHCPv6 生命週期追蹤與結構化日誌 (Solicit/Request/Renew、租約事件) |
| `routerd-dhcpv4-client` | `routerd-dhcpv4-client` | DHCPv4 生命週期追蹤與日誌 |
| `routerd-pppoe-client` | `routerd-pppoe-client` | PPPoE 連線生命週期 |
| `routerd-healthcheck` | `routerd-healthcheck` | 探測結果 (附目標屬性的成功/失敗) |

每個 daemon 都會把 `routerd.resource.name` 設成資源屬性,所以你可以依資源 (例如每條 WAN 上的 DHCPv6 client) 拆分訊號。

輸出方式為 OTLP/gRPC。預設 logs/metrics/traces 共用同一個端點;若後端偏好分流,也能個別指定。

## 設定輸出

routerd 讀取 OpenTelemetry 標準環境變數,沒有 routerd 專屬語法。OTLP/gRPC exporter 上游能解的變數都能用。

主要變數:

| 變數 | 用途 |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 全訊號共用的端點 (例:`http://collector.lan:4317`) |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` | 個別訊號覆寫 |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` 關閉 TLS (lab 用) |
| `OTEL_EXPORTER_OTLP_HEADERS` | 例如 managed 後端用 `Authorization=Bearer ...` |
| `OTEL_SERVICE_NAMESPACE` | 建議所有 daemon 都設成 `routerd` |
| `OTEL_RESOURCE_ATTRIBUTES` | `key=value,...` 自由標註 site / host 屬性 |

若 `OTEL_EXPORTER_OTLP_ENDPOINT` / `_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` 全部沒設,routerd 完全跳過遙測初始化。沒有 daemon 端的 on/off 開關,**沒設變數就是 OFF**。

### 套用到 systemd 管理的 routerd

Linux 安裝是把變數放進 systemd unit 的環境。為了不被上游 unit 更新蓋掉,放成 drop-in 最乾淨:

```ini
# /etc/systemd/system/routerd.service.d/10-otel.conf
[Service]
Environment=OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.lan:4317
Environment=OTEL_EXPORTER_OTLP_INSECURE=true
Environment=OTEL_SERVICE_NAMESPACE=routerd
Environment=OTEL_RESOURCE_ATTRIBUTES=deployment.environment=home,host.name=edge-router
```

每個你要輸出的受管 daemon 都重複同樣的 drop-in:

- `/etc/systemd/system/routerd-dhcpv6-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-dhcpv4-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-pppoe-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-healthcheck@.service.d/10-otel.conf`

接著:

```bash
sudo systemctl daemon-reload
sudo systemctl restart routerd.service \
                       'routerd-dhcpv6-client@*.service' \
                       'routerd-healthcheck@*.service'
```

### NixOS

在 routerd 的 NixOS module 產生的每個 systemd service 的 environment 加上變數:

```nix
systemd.services.routerd.environment = {
  OTEL_EXPORTER_OTLP_ENDPOINT = "http://collector.lan:4317";
  OTEL_EXPORTER_OTLP_INSECURE = "true";
  OTEL_SERVICE_NAMESPACE      = "routerd";
};
```

routerd 為你產生的 daemon service 也比照辦理。

### FreeBSD

把變數加到 routerd 渲染出來的 rc.d 包裝程式的 `command_args` 環境區塊 (若包裝程式支援,可改用 `routerd_envfile=...`)。

## 起一個接收端來驗證

任何 OTLP/gRPC 後端都行。冒煙測試最方便的是 `otelcol-contrib` 加 `debug` exporter:

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

重啟 routerd 後幾秒內應該會看到:

- `routerd.controller.reconcile` Sum 指標 (持續增加)
- `controller.reconcile` span (狀態 OK)
- routerd 的 slog 以 `LogRecord` 形式送進來

如果只看到 `routerd` 本體的紀錄,而 daemon 沉默,確認 daemon 端的 drop-in 是否套用、`daemon-reload` 是否打過。

## 疑難排解

**daemon journal 出現 `address family not supported by protocol`。** routerd 加固過的 systemd unit 限制了 address family。如果 collector 走 IPv4 (大多數情況),unit 必須允許 `AF_INET`。內建模板已經包含;若你有舊 drop-in 覆寫 `RestrictAddressFamilies`,確認 `AF_INET AF_INET6` 兩者都在。

**收集端沒資料。** 確認端點是 routerd 能解到的主機/IP (`getent ahosts` 與 `nc -vz host port`),且不走 TLS 時要設 `OTEL_EXPORTER_OTLP_INSECURE=true`。

**送到了但 service.name 怪怪的。** 各 daemon 設定自己的 `service.name`,要在後端歸群請用 `OTEL_RESOURCE_ATTRIBUTES=service.namespace=routerd,...`。**不要覆寫 `service.name` 本身**。

## routerd 不會給你的東西

- 內建 OTLP collector。請在 routerd 旁邊另起一份,或用 managed 後端。
- 內建儲存後端。routerd 自己有 SQLite 日誌 DB (`events.db`, `dns-queries.db`, `traffic-flows.db`, `firewall-logs.db`),Web Console 直接看;OTLP 輸出是把同一份資料「往主機外送」。

## 之後

預計加入宣告式的 `Telemetry` 資源 (apiVersion `observability.routerd.net/v1alpha1`)。屆時就能用 YAML 表達端點/訊號/屬性,不必再手改 systemd drop-in。在那之前,上面的環境變數就是支援的設定介面。
