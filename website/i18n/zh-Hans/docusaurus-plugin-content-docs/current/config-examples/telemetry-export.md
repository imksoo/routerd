---
title: 将遥测数据发送至 OTLP 收集器
sidebar_position: 110
---

# 将遥测数据发送至 OTLP 收集器

![routerd log、metric、trace 与 service attribute 通过 Telemetry resource export 到 OTLP collector 的构成](/img/diagrams/config-example-telemetry-export.png)

此示例演示如何将 routerd 的遥测数据发送至 OpenTelemetry 收集器。
可用于观测长时间运行状态、健康检查、DPI 以及应用操作的延迟。

完整的 YAML 位于 `examples/telemetry-export.yaml`。

## 架构图

```mermaid
flowchart LR
  router["[1] routerd host"]
  collector["[2] OTLP collector<br/>collector.example.internal:4317"]
  backend["[3] metrics / logs / traces backend"]

  router --> collector --> backend
```

## 图示对照表

| 编号 | 说明 | 主要资源 |
| --- | --- | --- |
| [1] | 输出 logs、metrics、traces 的 routerd 程序。 | `Telemetry/otlp` |
| [2] | OTLP 收集器的 endpoint。 | `Telemetry.spec.otlp.endpoint` |
| [3] | 收集器转发的目标后端。 | routerd 管理范围外 |

## 重点说明

```yaml
# [1] 启用 routerd 遥测数据导出。
- apiVersion: observability.routerd.net/v1alpha1
  kind: Telemetry
  metadata:
    name: otlp
  spec:
    # [2] OTLP collector 端点。
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

## 确认步骤

先在 daemon 未启动时进行独立检查。以下命令需要具有 `sudo` 权限的本地用户，但不会应用网络变更。

```bash
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config examples/telemetry-export.yaml
sudo routerd apply --config examples/telemetry-export.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
rm -rf "$LAB_DIR"
```

服务运行后，可在路由器上使用 `sudo routerctl describe Telemetry/otlp`；请同时确认收集器及后端均已正确接收数据。
endpoint 应置于可信任的管理网络或专用观测网络中。
