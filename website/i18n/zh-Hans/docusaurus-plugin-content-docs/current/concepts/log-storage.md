# 日志存储

routerd 将长期状态与运行日志分开存储。

Linux 的默认配置如下：

| 文件 | 用途 | 标准保存期限 |
| --- | --- | --- |
| `/var/lib/routerd/routerd.db` | 资源状态与事件表 | 事件 30 天 |
| `/var/lib/routerd/dns-queries.db` | `routerd-dns-resolver` 的 DNS 查询历史 | 30 天 |
| `/var/lib/routerd/traffic-flows.db` | 从 conntrack 建立的流量历史 | 30 天 |
| `/var/lib/routerd/firewall-logs.db` | accept、drop、reject 的防火墙日志 | 90 天 |

FreeBSD 上，相同的数据库名称存放于 `/var/db/routerd` 之下。

日志表的字段名称以方便转换为 OpenTelemetry 日志属性的方式命名。
`traffic-flows.db` 中为 nDPI 与 TLS SNI 预留了字段，
但当前尚未实现向这些字段写入的处理，将在后续实现中添加。

`LogRetention` 依信号单位删除旧有数据行，
也可执行 SQLite 的 incremental vacuum。DB 文件路径不出现在配置中，
routerd 从生成事件、DNS 查询、流量、防火墙事件的资源中导出。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: default
spec:
  retention: 30d
  schedule: daily
  vacuum: true
  signals:
    - events
    - dnsQueries
    - trafficFlows
  sinks:
    - LogSink/local-syslog
---
apiVersion: system.routerd.net/v1alpha1
kind: LogRetention
metadata:
  name: firewall-events
spec:
  retention: 90d
  schedule: daily
  vacuum: true
  signals:
    - firewallEvents
```

确认时使用以下命令：

```sh
routerctl dns-queries --since 1h
routerctl traffic-flows --since 1h
routerctl firewall-logs --since 24h --action drop
```
