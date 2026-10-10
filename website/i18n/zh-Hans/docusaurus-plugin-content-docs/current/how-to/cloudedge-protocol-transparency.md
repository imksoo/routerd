---
title: CloudEdge 协议透明性验收检验
---

# CloudEdge 协议透明性验收检验

![CloudEdge 协议透明性探针的 FTP、NFS、大容量传输、PMTU、源 IP 保留、no-NAT 证据的流程](/img/diagrams/how-to-cloudedge-protocol-transparency.png)

这是一个不使用云端的线束计划，用于验证 CloudEdge mobility 对面向连接的协议（这些协议对 NAT、辅助 ALG、动态端口、MTU/PMTU 行为敏感）的透明性。实际的实时运行将由实验室操作员稍后执行。本文档和 `scripts/` 目录下的脚本仅准备契约和证据格式。

## 目标

对逻辑共享子网（演示中为 `10.77.60.0/24`）上的流量，证明以下几点：

- 无 NAT：服务器将客户端站点的 mobility `/32` 识别为对等地址。
- 客户端的默认网关未从本地站点变更。
- FTP 主动模式和被动模式均在无 NAT ALG 的情况下完成数据传输。
- 通过 `rpcbind` 的 RPC 端点发现和 NFSv3 的挂载/读写在站点间正常工作。
- 大容量传输在无 PMTU 黑洞的情况下完成。
- MSS/PMTU 证据记录了 overlay MTU、路由 MTU/advmss（如可用）、配置的 MSS clamp 值。

## 最小实时矩阵

在常规 D3 有向矩阵已全部通过后，运行协议探针。使用 2 个代表性对：

| 对 | 理由 |
| --- | --- |
| `aws -> azure` | 两端均使用云端提供商捕获的云间 overlay 路径 |
| `aws -> onprem` | on-prem 端使用 proxy-ARP/VRRP 权限的云-on-prem 路径 |

在场景目录中，这编码为 `examples/cloudedge-acceptance-scenarios.json` 的 `d11-protocol-transparency`。

如需扩大对等性验证，可添加 `azure -> oci`、`oci -> aws`、反向等，但最小验收检验应控制在单次 4 站点实验室窗口内可执行的规模。

## 线束

封装器如下：

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
  scripts/cloudedge-protocol-probe.sh \
    --pairs aws:azure,aws:onprem \
    --bytes 104857600 \
    --out evidence/protocol-probe.json
```

完整验收场景使用相同封装器：

```sh
PROTOCOL_PROBE_RUNNER=scripts/runners/cloudedge-protocol-runner.sh \
MATRIX_RUNNER=scripts/runners/cloudedge-matrix-runner.sh \
scripts/cloudedge-acceptance.sh run \
  --scenario d11-protocol-transparency \
  --out evidence/d11-protocol \
  --commit <routerd-commit>
```

输出通过 `scripts/cloudedge-protocol-result-schema.json` 验证，并导入到 `result.json` 的 `protocols` 对象下。

## 运行器契约

`scripts/runners/cloudedge-protocol-runner.sh` 实现 `PROTOCOL_PROBE_RUNNER`。有意通过环境变量参数化，不包含提供商账户 ID、资源 ID、secret。

每个站点所需的设置：

```sh
export CE_AWS_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AWS_CLIENT_IP=10.77.60.11
export CE_AZURE_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export AZURE_CLIENT_IP=10.77.60.12
export CE_ONPREM_CLIENT_SSH_HOST=<ssh-host-or-user@host>
export ONPREM_CLIENT_IP=10.77.60.10
export SSH_KEY_FILE=<private-key>
export SSH_USER=ubuntu
export CLIENT_SSH_USER=ubuntu
```

协议相关变量：

```sh
export CE_PROTOCOL_INSTALL=1
export CE_PROTOCOL_CONFIGURE_SERVICES=1
export CE_PROTOCOL_FTP_PASSIVE_PORTS=40000:40100
export CE_PROTOCOL_BULK_BYTES=104857600
export CE_PROTOCOL_PMTU_SIZE=1300
export CE_PROTOCOL_OVERLAY_IFACE=wg-hybrid
export CE_PROTOCOL_MSS_CLAMP=1340
```

每个操作均可在不编辑运行器的情况下覆盖：

```sh
export CE_PROTOCOL_FTP_ACTIVE_COMMAND='...'
export CE_PROTOCOL_NFS_COMMAND='...'
```

封装器对每个对调用以下操作：

| 操作 | 断言 |
| --- | --- |
| `setup` | 启用时安装/配置 `vsftpd`、`rpcbind`、NFS 服务器/客户端工具、`iperf3` |
| `ftp-active` | FTP `PORT` 模式数据通道完成 |
| `ftp-passive` | FTP 被动模式数据通道完成 |
| `nfs` | NFSv3 挂载 + 要求字节数的写入/读取完成 |
| `rpc` | `rpcinfo -p` 发现 `rpcbind` 和至少 1 个动态 RPC/NFS 端口 |
| `bulk` | `iperf3 -n <bytes>` 完成，记录吞吐量/重传 |
| `pmtu` | DF ping 成功，记录 overlay MTU、路由 MTU/advmss、MSS clamp |
| `source-preserved` | 服务器端 SSH 将客户端的 mobility `/32` 识别为对等 IP |
| `no-nat` | 相同的对等 IP 检查，作为显式 no-NAT 断言记录 |

## Forcefrag / MSS 比较

常规运行中，routerd 导出的 MSS clamp 应能正常通过。如果需要在实验室中验证 P2-b 的强制分片行为，请对同一 D11 对集运行 2 次：

1. `forceFragmentIPv4: false`（默认）：TCP 传输应通过 MSS clamp 通过。超大的 DF 非 TCP 可能因底层网络 PMTU 而失败。
2. 在相关 `OverlayPeer` 或 `TunnelInterface` 上 `forceFragmentIPv4: true`：相同的 DF 探针应通过，路由器证据中应显示 `routerd_forcefrag`。

不要全局启用 force fragmentation。限定在路径范围内，并在证据包中记录 before/after 的配置摘要。

## 证据审查清单

对 `protocol-probe.json` 中的每个对：

- `checks.ftpActive`、`ftpPassive`、`nfs`、`rpc`、`bulkTransfer`、`pmtu`、`sourceIpPreserved`、`noNat` 均为 `pass`。
- `details.sourceIpPreserved.peer_ip` 与客户端站点的 mobility `/32` 一致。
- `details.rpc.dynamic_port` 存在且不是 `111`。
- 如果 `iperf3` 可用，`details.bulkTransfer.retransmits` 已记录。
- `details.pmtu.overlay_mtu`、`route_mtu` 或 `route_advmss`、`mss_clamp` 已记录。

外层 `result.json` 应包含以下通过断言：

- `protocol_transparency`
- `ftp_active_passive`
- `nfs_rpc`
- `bulk_transfer_pmtu`
- `protocol_source_ip_preserved`
- `protocol_no_nat`

SAM E2E工具逐字节核对FTP、NFS和CIFS的读取与写入内容。故障切换传输使用已知的64 MiB数据，下载后同时核对大小和SHA-256。注入故障前，必须确认本次传输任务仍在运行且已收到部分数据；无法确认进度时，以未判定状态停止，不注入故障或重试。保留启动、结果和失败尝试的证据。可选的传输观测和性能值与正常故障后matrix判定分开。部署使用本次专用目录，传播每个必要复制或配置步骤的失败，并在重启服务前比较已安装的binary和配置与本次数据。

L2 qualification需要同一provider和阈值配置的before与after观测以及抑制机制记录。before采集成功可进入下一操作，但保存的汇总在after采集前仍为未判定；after的退出码反映汇总结果。故障切换时长使用单调时钟，journal关联用UTC另行记录。provider的VM停止实现处理`stop-active`；`drain`需要显式配置`CE_<PROVIDER>_DRAIN_INJECT_COMMAND`或`CE_DRAIN_INJECT_COMMAND`，on-premises停止也需要明确的node-stop command。仅停止keepalived不能证明node停止qualification。

## Federation qualification 证据

`scripts/cloudedge-federation-qualification.sh` 的 release qualification 要求正数 cycles 和全部 8 个场景。`--allow-skip` 允许开发用子集，其结果不会认定 release。每次尝试使用新的证据目录和 event ID，保留 command 的 stdout/stderr/退出码；worker 在现有 `--duration` 预算内停止，预算包含 cleanup。失败、观测不足或 cleanup 未确认时，不重试，并停止后续故障和 cycle。遗留故障 marker 需要操作员在下一次尝试前检查。

TTL refresh 将当前 receiver event 的 `ExpiresAt` 与 sender 的延长值对照，旧 delivered 行或缺失的 `staleTTL` 不能作为证明。subscription 失败和恢复绑定本次 event ID 与 receiver 侧注入后的时间。partition 隔离记录正常 A/B baseline、A 故障期间本次 B 配送，以及限定 group/peer 的 SLO 违规、remediation 和配置阈值对应关系；相同阈值也是有效配置。security 使用正确签名的损坏 body 和带错误签名的有效 body，要求预期 HTTP 400/401、receiver 未接收和本次有效 event 配送。transport error 和 5xx 不证明拒绝成功。

OTel 可用性、sample 和 label 属于诊断，缺失保持未判定，不替换成数值零或产品失败。保存的 binary SHA-256 和产品 source commit 与 QA repository commit 分开记录。已部署 source identity 不明或不匹配时，在 scenario 故障前停止。退出码 1 表示观测到的 assertion 失败，2 表示无效输入/setup，3 表示观测或 provenance 不完整。这些修改通过 offline 边界验证，不代表 provider 或 release qualification 已完成。

开发用 partition 子集未选择 multi-group 时，不要求 B group。实机的 Go 是可选工具。`CE_BINARY_PROVENANCE_FILE` 可指定现有 prepared/release contract，将 `routerdArtifact.commit` 和 `execution.candidate_binary_hashes` 与本次 binary SHA-256 对照。runtime commit 显示冲突或 hash 不匹配时保留未确认状态，不会仅凭 commit 参数确认 identity。

eventd digest 来自目标 systemd unit 的 MainPID 执行文件（`/proc/<pid>/exe`），取得后再次核对进程和执行文件。进程缺失或变化时保持未确认，PATH 中的 binary 不能证明运行中服务的 identity。成功响应中的空 OTel label 数组仍为诊断未判定。

现有 offline assertion 按 WireGuard peer block 和完整 resource snapshot、每个 leaf 恢复的准确七个 peer、注入的 transition event identity/时间以及 transport peer/address 对应关系核对。SSE request deadline 可中断阻塞的正文读取。active-stable guard 要求指定文件存在，并逐个检查 version token。PoC bundle 将 schema 验证记录为 validated、unavailable 或 invalid，不新增必须安装 validator 的 gate。capture stop 检查现有四个 typed point record；缺失或损坏的 state 保持 PARTIAL，不执行 stop/copy。AWS fabric 核对请求的 secondary address 及其最长匹配 route，保留 raw facts 和 PARTIAL/NOT-RUN。labctl 只构建匹配的 clean 当前 checkout 并记录 commit/tree identity；dry run 和 prebuilt 输入不声称完成 build 或使用请求的 source。RR stage 拒绝必需 row 的重复或冲突，report gate 从已确认 outcome 导出。这些是 offline 检查，不代表实机 qualification。
