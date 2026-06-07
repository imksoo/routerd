---
title: 將遙測資料送到 OTLP 收集器
slug: /how-to/opentelemetry
---

# 將遙測資料送到 OTLP 收集器

![routerd daemon 的 log、metric、trace、resource attribute、OTLP environment variable，以及到 external OpenTelemetry collector 的 export](/img/diagrams/how-to-opentelemetry.png)

## 情境

當您想把路由器的日誌、指標與追蹤，送到 OpenTelemetry 相容的後端（Grafana Loki/Tempo/Mimir、Datadog、Honeycomb、自架的 `otelcol-contrib` 等），不必每次都執行 `journalctl` 或 `routerctl events`，而是從外部儀表板觀測狀態時，本指南即適用。

routerd 的所有常駐程式均可透過 OpenTelemetry 匯出資料。收集器本體並不內建於 routerd binary 中，請另行準備外部 OTLP 端點，routerd 會以 OTLP/gRPC 傳送資料。

## routerd 會匯出的內容

| 常駐程式 | service.name | 內容 |
| --- | --- | --- |
| `routerd`（控制平面） | `routerd` | `controller.reconcile` 追蹤、`routerd.controller.reconcile` 計數器、結構化 slog 日誌 |
| `routerd-dhcpv6-client` | `routerd-dhcpv6-client` | DHCPv6 生命週期的追蹤與結構化日誌（Solicit/Request/Renew、租約事件） |
| `routerd-dhcpv4-client` | `routerd-dhcpv4-client` | DHCPv4 生命週期的追蹤與日誌 |
| `routerd-pppoe-client` | `routerd-pppoe-client` | PPPoE 連線的生命週期 |
| `routerd-healthcheck` | `routerd-healthcheck` | 健康檢查結果（附 target 屬性的成功/失敗） |

每個常駐程式都會在資源屬性中附上 `routerd.resource.name`，因此可以依資源（例如每條 WAN 的 DHCPv6 客戶端）分別篩選訊號。

匯出方式為 OTLP/gRPC。logs / metrics / traces 預設共用同一個端點，若後端有需要也可分別指定。

## 匯出設定

routerd 讀取 OpenTelemetry 的標準環境變數，沒有 routerd 專屬的語法。OTLP/gRPC 匯出器的上游能解析的變數，可直接使用。

主要變數如下。

| 變數 | 用途 |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 所有訊號共用的端點（例如：`http://collector.lan:4317`） |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` | 各訊號的個別指定 |
| `OTEL_EXPORTER_OTLP_INSECURE` | 設為 `true` 可停用 TLS（實驗室用） |
| `OTEL_EXPORTER_OTLP_HEADERS` | 例如：managed 後端用的 `Authorization=Bearer ...` |
| `OTEL_SERVICE_NAMESPACE` | 建議：所有常駐程式共用 `routerd` |
| `OTEL_RESOURCE_ATTRIBUTES` | 以 `key=value,...` 格式自由指定站點、主機等屬性 |

若 `OTEL_EXPORTER_OTLP_ENDPOINT` / `_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` 均未設定，routerd 會完全略過遙測初始化。常駐程式端沒有個別的開關，**不設定變數即代表停用**。

### 套用至 systemd 管理的 routerd

Linux 安裝時，請將變數加入 systemd unit 的環境中。為避免上游 unit 更新後被覆蓋，建議使用 drop-in 方式。

```ini
# /etc/systemd/system/routerd.service.d/10-otel.conf
[Service]
Environment=OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.lan:4317
Environment=OTEL_EXPORTER_OTLP_INSECURE=true
Environment=OTEL_SERVICE_NAMESPACE=routerd
Environment=OTEL_RESOURCE_ATTRIBUTES=deployment.environment=home,host.name=edge-router
```

每個需要匯出的受管常駐程式也請加入相同的 drop-in。

- `/etc/systemd/system/routerd-dhcpv6-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-dhcpv4-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-pppoe-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-healthcheck@.service.d/10-otel.conf`

接著執行以下指令。

```bash
sudo systemctl daemon-reload
sudo systemctl restart routerd.service \
                       'routerd-dhcpv6-client@*.service' \
                       'routerd-healthcheck@*.service'
```

### NixOS

在 routerd 的 NixOS 模組產生的各 systemd service 的 environment 中加入變數。

```nix
systemd.services.routerd.environment = {
  OTEL_EXPORTER_OTLP_ENDPOINT = "http://collector.lan:4317";
  OTEL_EXPORTER_OTLP_INSECURE = "true";
  OTEL_SERVICE_NAMESPACE      = "routerd";
};
```

routerd 產生的常駐程式 service 也請同樣設定。

### FreeBSD

請將變數加入 routerd 產出的 rc.d 包裝程式的 `command_args` 環境區塊中（若包裝程式支援，也可使用 `routerd_envfile=...`）。

## 建立接收端進行驗證

任何 OTLP/gRPC 後端均可。快速冒煙測試最方便的方式是使用 `otelcol-contrib` 搭配 `debug` 匯出器。

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

重啟 routerd 後數秒內，應可看到以下內容。

- `routerd.controller.reconcile` 的 Sum 指標（持續遞增）
- `controller.reconcile` 的 span（狀態 OK）
- routerd 的 slog 輸出以 `LogRecord` 形式送達

若只收到 `routerd` 本體的記錄，而常駐程式端沉默，請確認常駐程式的 drop-in 是否已套用、以及是否有執行 `daemon-reload`。

## 疑難排解

**常駐程式的 journal 出現 `address family not supported by protocol`。** routerd 加固過的 systemd unit 限制了位址家族。若收集器透過 IPv4 連線（大多數情況如此），unit 必須允許 `AF_INET`。內建範本已包含此設定；若您有舊的 drop-in 覆寫了 `RestrictAddressFamilies`，請確認 `AF_INET AF_INET6` 兩者均包含在內。

**收集器未收到任何資料。** 請確認端點是 routerd 可解析的主機/IP（使用 `getent ahosts` 與 `nc -vz host port` 確認），以及不使用 TLS 時是否已設定 `OTEL_EXPORTER_OTLP_INSECURE=true`。

**資料送達但 service.name 不正確。** 每個常駐程式會自行設定 `service.name`。若要在後端進行群組化，請使用 `OTEL_RESOURCE_ATTRIBUTES=service.namespace=routerd,...`。請勿覆寫 `service.name` 本身。

## routerd 不提供的功能

- 內建 OTLP 收集器。請在 routerd 旁另行架設，或使用 managed 後端。
- 內建儲存後端。routerd 本身具備 SQLite 日誌 DB（`events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db`），可從 Web 管理介面查看。OTLP 匯出的用途是「將相同資料傳送至主機外部」。

## 宣告式 Telemetry 資源

使用 `Telemetry` 可以在路由器的 YAML 中指定 OTLP 端點。routerd 會將對應的 OpenTelemetry 環境變數注入至產生的 systemd、NixOS 及 FreeBSD rc.d unit 中。收集器需在外部另行準備，routerd 只負責設定匯出器。

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

若希望也將 routerd 內部的事件串流轉發至 stdout / syslog / Loki，請使用
`ObservabilityPipeline`。詳情請參閱
[Observability pipeline](../operations/observability.md)。
