---
title: 将遥测数据送到 OTLP 收集器
slug: /how-to/opentelemetry
---

# 将遥测数据送到 OTLP 收集器

## 场景

当您想把路由器的日志、指标与追踪，送到 OpenTelemetry 兼容的后端（Grafana Loki/Tempo/Mimir、Datadog、Honeycomb、自建的 `otelcol-contrib` 等），不必每次都执行 `journalctl` 或 `routerctl events`，而是从外部仪表板观测状态时，本指南即适用。

routerd 的所有守护进程均可通过 OpenTelemetry 导出数据。收集器本体并不内置于 routerd binary 中，请另行准备外部 OTLP 端点，routerd 会以 OTLP/gRPC 传送数据。

## routerd 会导出的内容

| 守护进程 | service.name | 内容 |
| --- | --- | --- |
| `routerd`（控制平面） | `routerd` | `controller.reconcile` 追踪、`routerd.controller.reconcile` 计数器、结构化 slog 日志 |
| `routerd-dhcpv6-client` | `routerd-dhcpv6-client` | DHCPv6 生命周期的追踪与结构化日志（Solicit/Request/Renew、租约事件） |
| `routerd-dhcpv4-client` | `routerd-dhcpv4-client` | DHCPv4 生命周期的追踪与日志 |
| `routerd-pppoe-client` | `routerd-pppoe-client` | PPPoE 连接的生命周期 |
| `routerd-healthcheck` | `routerd-healthcheck` | 健康检查结果（附 target 属性的成功/失败） |

每个守护进程都会在资源属性中附上 `routerd.resource.name`，因此可以依资源（例如每条 WAN 的 DHCPv6 客户端）分别筛选信号。

导出方式为 OTLP/gRPC。logs / metrics / traces 默认共用同一个端点，若后端有需要也可分别指定。

## 导出配置

routerd 读取 OpenTelemetry 的标准环境变量，没有 routerd 专属的语法。OTLP/gRPC 导出器的上游能解析的变量，可直接使用。

主要变量如下。

| 变量 | 用途 |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 所有信号共用的端点（例如：`http://collector.lan:4317`） |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` | 各信号的单独指定 |
| `OTEL_EXPORTER_OTLP_INSECURE` | 设为 `true` 可停用 TLS（实验室用） |
| `OTEL_EXPORTER_OTLP_HEADERS` | 例如：managed 后端用的 `Authorization=Bearer ...` |
| `OTEL_SERVICE_NAMESPACE` | 建议：所有守护进程共用 `routerd` |
| `OTEL_RESOURCE_ATTRIBUTES` | 以 `key=value,...` 格式自由指定站点、主机等属性 |

若 `OTEL_EXPORTER_OTLP_ENDPOINT` / `_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` 均未设置，routerd 会完全跳过遥测初始化。守护进程端没有单独的开关，**不设置变量即代表停用**。

### 应用至 systemd 管理的 routerd

Linux 安装时，请将变量加入 systemd unit 的环境中。为避免上游 unit 更新后被覆盖，建议使用 drop-in 方式。

```ini
# /etc/systemd/system/routerd.service.d/10-otel.conf
[Service]
Environment=OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.lan:4317
Environment=OTEL_EXPORTER_OTLP_INSECURE=true
Environment=OTEL_SERVICE_NAMESPACE=routerd
Environment=OTEL_RESOURCE_ATTRIBUTES=deployment.environment=home,host.name=edge-router
```

每个需要导出的受管守护进程也请加入相同的 drop-in。

- `/etc/systemd/system/routerd-dhcpv6-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-dhcpv4-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-pppoe-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-healthcheck@.service.d/10-otel.conf`

接着执行以下命令。

```bash
sudo systemctl daemon-reload
sudo systemctl restart routerd.service \
                       'routerd-dhcpv6-client@*.service' \
                       'routerd-healthcheck@*.service'
```

### NixOS

在 routerd 的 NixOS 模块生成的各 systemd service 的 environment 中加入变量。

```nix
systemd.services.routerd.environment = {
  OTEL_EXPORTER_OTLP_ENDPOINT = "http://collector.lan:4317";
  OTEL_EXPORTER_OTLP_INSECURE = "true";
  OTEL_SERVICE_NAMESPACE      = "routerd";
};
```

routerd 生成的守护进程 service 也请同样设置。

### FreeBSD

请将变量加入 routerd 生成的 rc.d 包装程序的 `command_args` 环境区块中（若包装程序支持，也可使用 `routerd_envfile=...`）。

## 建立接收端进行验证

任何 OTLP/gRPC 后端均可。快速冒烟测试最方便的方式是使用 `otelcol-contrib` 搭配 `debug` 导出器。

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

重启 routerd 后数秒内，应可看到以下内容。

- `routerd.controller.reconcile` 的 Sum 指标（持续递增）
- `controller.reconcile` 的 span（状态 OK）
- routerd 的 slog 输出以 `LogRecord` 形式送达

若只收到 `routerd` 本体的记录，而守护进程端沉默，请确认守护进程的 drop-in 是否已应用、以及是否有执行 `daemon-reload`。

## 故障排查

**守护进程的 journal 出现 `address family not supported by protocol`。** routerd 加固过的 systemd unit 限制了地址族。若收集器通过 IPv4 连接（大多数情况如此），unit 必须允许 `AF_INET`。内置模板已包含此设置；若您有旧的 drop-in 覆盖了 `RestrictAddressFamilies`，请确认 `AF_INET AF_INET6` 两者均包含在内。

**收集器未收到任何数据。** 请确认端点是 routerd 可解析的主机/IP（使用 `getent ahosts` 与 `nc -vz host port` 确认），以及不使用 TLS 时是否已设置 `OTEL_EXPORTER_OTLP_INSECURE=true`。

**数据送达但 service.name 不正确。** 每个守护进程会自行设置 `service.name`。若要在后端进行分组，请使用 `OTEL_RESOURCE_ATTRIBUTES=service.namespace=routerd,...`。请勿覆盖 `service.name` 本身。

## routerd 不提供的功能

- 内置 OTLP 收集器。请在 routerd 旁另行架设，或使用 managed 后端。
- 内置存储后端。routerd 本身具备 SQLite 日志 DB（`events.db`、`dns-queries.db`、`traffic-flows.db`、`firewall-logs.db`），可从 Web 管理界面查看。OTLP 导出的用途是「将相同数据传送至主机外部」。

## 声明式 Telemetry 资源

使用 `Telemetry` 可以在路由器的 YAML 中指定 OTLP 端点。routerd 会将对应的 OpenTelemetry 环境变量注入至生成的 systemd、NixOS 及 FreeBSD rc.d unit 中。收集器需在外部另行准备，routerd 只负责配置导出器。

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

若希望也将 routerd 内部的事件流转发至 stdout / syslog / Loki，请使用
`ObservabilityPipeline`。详情请参阅
[Observability pipeline](../operations/observability.md)。
