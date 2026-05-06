---
title: 把遥测数据送到 OTLP 收集器
slug: /how-to/opentelemetry
---

# 把遥测数据送到 OTLP 收集器

## 场景

你想把路由器的日志、指标和追踪发到任意 OpenTelemetry 兼容的后端 (Grafana Loki/Tempo/Mimir、Datadog、Honeycomb、自建 `otelcol-contrib` 等),而不必每次都去 `journalctl` 或 `routerctl events`。

routerd 的所有常驻 daemon 都能输出 OpenTelemetry。**routerd 不内置收集器**;你需要自己准备外部 OTLP 端点,routerd 会用 OTLP/gRPC 把数据发过去。

## routerd 会输出什么

| Daemon | service.name | 内容 |
| --- | --- | --- |
| `routerd` (控制面) | `routerd` | `controller.reconcile` 追踪、`routerd.controller.reconcile` 计数器、结构化 slog 日志 |
| `routerd-dhcpv6-client` | `routerd-dhcpv6-client` | DHCPv6 生命周期追踪与日志 (Solicit/Request/Renew、租约事件) |
| `routerd-dhcpv4-client` | `routerd-dhcpv4-client` | DHCPv4 生命周期追踪与日志 |
| `routerd-pppoe-client` | `routerd-pppoe-client` | PPPoE 会话生命周期 |
| `routerd-healthcheck` | `routerd-healthcheck` | 探测结果 (附目标属性的成功/失败) |

每个 daemon 都会把 `routerd.resource.name` 设成资源属性,所以你能按资源 (例如每条 WAN 上的 DHCPv6 client) 拆分信号。

输出方式是 OTLP/gRPC。默认 logs/metrics/traces 共用同一端点,如后端要求,也可分别指定。

## 配置输出

routerd 读取 OpenTelemetry 标准环境变量,没有 routerd 专属语法。OTLP/gRPC exporter 上游能解析的变量都能直接用。

主要变量:

| 变量 | 用途 |
| --- | --- |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 全信号共用端点 (例如 `http://collector.lan:4317`) |
| `OTEL_EXPORTER_OTLP_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` | 单信号覆盖 |
| `OTEL_EXPORTER_OTLP_INSECURE` | `true` 关闭 TLS (lab 用) |
| `OTEL_EXPORTER_OTLP_HEADERS` | 例如 managed 后端用 `Authorization=Bearer ...` |
| `OTEL_SERVICE_NAMESPACE` | 建议所有 daemon 都设为 `routerd` |
| `OTEL_RESOURCE_ATTRIBUTES` | 用 `key=value,...` 自由标注 site / host 属性 |

如果 `OTEL_EXPORTER_OTLP_ENDPOINT` / `_LOGS_ENDPOINT` / `_METRICS_ENDPOINT` / `_TRACES_ENDPOINT` 全部没设置,routerd 会完全跳过遥测初始化。daemon 没有专门的开关,**不设变量就是 OFF**。

### 应用到 systemd 管理的 routerd

Linux 安装把变量放进 systemd unit 的环境。为了不被上游 unit 更新覆盖,使用 drop-in 最干净:

```ini
# /etc/systemd/system/routerd.service.d/10-otel.conf
[Service]
Environment=OTEL_EXPORTER_OTLP_ENDPOINT=http://collector.lan:4317
Environment=OTEL_EXPORTER_OTLP_INSECURE=true
Environment=OTEL_SERVICE_NAMESPACE=routerd
Environment=OTEL_RESOURCE_ATTRIBUTES=deployment.environment=home,host.name=edge-router
```

要输出的每个受管 daemon 都加同样的 drop-in:

- `/etc/systemd/system/routerd-dhcpv6-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-dhcpv4-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-pppoe-client@.service.d/10-otel.conf`
- `/etc/systemd/system/routerd-healthcheck@.service.d/10-otel.conf`

然后:

```bash
sudo systemctl daemon-reload
sudo systemctl restart routerd.service \
                       'routerd-dhcpv6-client@*.service' \
                       'routerd-healthcheck@*.service'
```

### NixOS

在 routerd 的 NixOS module 生成的每个 systemd service 的 environment 里加变量:

```nix
systemd.services.routerd.environment = {
  OTEL_EXPORTER_OTLP_ENDPOINT = "http://collector.lan:4317";
  OTEL_EXPORTER_OTLP_INSECURE = "true";
  OTEL_SERVICE_NAMESPACE      = "routerd";
};
```

routerd 给你生成的 daemon service 也照搬。

### FreeBSD

把变量加到 routerd 渲染的 rc.d 包装脚本的 `command_args` 环境块 (若包装脚本支持,可改用 `routerd_envfile=...`)。

## 起一个接收端验证

任何 OTLP/gRPC 后端都行。冒烟测试最方便的是 `otelcol-contrib` 加 `debug` exporter:

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

重启 routerd 几秒后应能看到:

- `routerd.controller.reconcile` Sum 指标 (持续增加)
- `controller.reconcile` span (状态 OK)
- routerd 的 slog 以 `LogRecord` 形式发进来

如果只见到 `routerd` 本体的记录而 daemon 沉默,检查 daemon 的 drop-in 是否生效、`daemon-reload` 是否打过。

## 故障排查

**daemon journal 出现 `address family not supported by protocol`。** routerd 加固后的 systemd unit 限制了 address family。如果 collector 走 IPv4 (大多数如此),unit 必须允许 `AF_INET`。内置模板已经包含;若你有旧 drop-in 覆盖了 `RestrictAddressFamilies`,确认 `AF_INET AF_INET6` 两者都在。

**收集端没数据。** 确认端点是 routerd 能解到的主机/IP (`getent ahosts` 与 `nc -vz host port`),并且不走 TLS 时要设 `OTEL_EXPORTER_OTLP_INSECURE=true`。

**有数据但 service.name 不对。** 每个 daemon 设定自己的 `service.name`。后端归组请用 `OTEL_RESOURCE_ATTRIBUTES=service.namespace=routerd,...`。**不要覆盖 `service.name` 本身**。

## routerd 不会提供的东西

- 内置 OTLP collector。请在 routerd 旁边另起一份,或用 managed 后端。
- 内置存储后端。routerd 自己有 SQLite 日志 DB (`events.db`, `dns-queries.db`, `traffic-flows.db`, `firewall-logs.db`),Web Console 可直接查看;OTLP 导出用于把同一份数据「送到主机外」。

## 后续

计划增加声明式的 `Telemetry` 资源 (apiVersion `observability.routerd.net/v1alpha1`)。届时可以用 YAML 表达端点/信号/属性,不必手改 systemd drop-in。在此之前,上述环境变量就是受支持的配置面。
