# ADR 0015: hub/spoke bootstrap 的 WireGuard peer enrollment

## 状态

提议 — 2026-06-09。

相关 issue: #377。

## 背景

`WireGuardInterface.spec.peersFrom` 已经可以从共享的 `SAMNodeSet` 派生 WireGuard
peer。当每个 router 都已经持有可信的 node registry 时，这可以减少大部分静态 peer 重复。

但这还不能完全解决 hub/spoke bootstrap。在 Route Reflector 或 spine 部署中，leaf router
通常主动连接固定的 RR/spine endpoint。RR/spine 侧仍然必须在 kernel 接受 peer 前知道每个
leaf 的 public key、allowed IP，以及可选 endpoint。随着 `pve-rt` 这类 leaf 增加，RR/spine
source of truth 仍会产生维护负担。

初次接触不能使用目标 WireGuard tunnel。WireGuard 会在应用协议运行前丢弃未知 peer，因此
enrollment 必须使用独立 bootstrap transport，例如 management address、underlay listener
或另一个预先建立的 control channel。

## 决策

为 hub/spoke 部署增加一个可选的 WireGuard peer enrollment flow。

RR/spine router 可以在显式配置的 non-WireGuard listen address 和 port 上公开 enrollment
endpoint。leaf 提交 node identity 和 WireGuard peer material，RR/spine 根据 local policy
和期望 topology 验证后才激活该 peer。

enrollment record 应包含：

- `nodeRef` 与目标 WireGuard interface；
- WireGuard `publicKey`；
- leaf 有稳定 endpoint 时的 endpoint 或 listen port；
- 请求的 `allowedIPs` 和/或 `samEndpoint`；
- 用于幂等 retry 和 stale write 检测的 nonce 或 generation。

已批准的 registration 存为 dynamic config，而不是隐藏在 config graph 之外的临时 runtime
state。effective config path 再将这些记录转换为普通 WireGuard peer input，可以是生成的
`WireGuardPeer` resource，也可以是现有 `WireGuardInterface.spec.peersFrom` 可以消费的 entry。
静态 `WireGuardPeer` resource 继续按名称覆盖生成 peer，保留应急 override 能力。

leaf 的静态 bootstrap config 保持很小：自身 private key、RR/spine public key 和固定 endpoint、
以及 enrollment credential。批准和激活由 RR/spine 负责。

## 验证和安全

enrollment 必须 fail closed。只有所有配置的检查都通过时才接受请求。

- enrollment endpoint 默认关闭，并且只绑定到配置的 address。
- 使用 bearer token、mTLS client identity 或签名 registration payload 等显式机制认证请求。
- 请求的 `nodeRef` 必须被 policy 允许；配置时，还必须存在于期望的 `SAMNodeSet` 中。
- 请求的 `allowedIPs` 与 `samEndpoint` 必须匹配 node identity，且不得与现有 node 冲突。
- public key 必须唯一；同一 node 对同一 generation 的 retry 除外。
- re-registration、key rotation、reject、revoke 和 expire 必须能在 audit/status output 中看到。
- rate limiting 保护 bootstrap endpoint，避免无效 registration 连续冲击。

`routerctl` 应将 enrollment state 显示为 `Pending`、`Approved`、`Rejected`、`Revoked`
或 `Expired`，并在 request 未激活时显示验证原因。

## 非目标

- 不替换 WireGuard cryptokey routing。RR/spine 仍为每个已批准 leaf 安装一个 kernel peer。
- 不在没有显式 policy decision 的情况下接受任意 public key。
- 不通过目标 WireGuard interface 运行首次 enrollment。
- 不让 `SAMNodeSet` 分发依赖一个本身需要新 peer 才能建立的 tunnel。

## 实施计划

1. 定义 enrollment API resource 形状、status model 和 CLI/status output，并与 WireGuard runtime reconcile 分离。
2. 将 RR/spine enrollment storage 作为 dynamic config source 增加，并保留持久 audit 信息和 stale entry cleanup。
3. 增加基于 policy 和可选 `SAMNodeSet` membership 的 validation。
4. 在保留 static peer override 行为的同时，把已批准 registration 输入现有 effective WireGuard peer generation path。
5. 增加可在 boot 时安全运行的、幂等的 leaf-side submit/retry logic。
6. 增加 revoke 和 key rotation flow。

## 影响

如果部署具有 approved enrollment policy，RR/spine config 就不再需要随着每个 leaf 增加而手写一个
WireGuard peer block。kernel peer 数和 identity validation 仍然存在，但 operator workflow
会从逐个编辑 RR/spine peer material 转向批准或预授权 leaf registration。

该功能也明确了 bootstrap boundary：topology distribution 继续使用 `SAMNodeSet` 和 `peersFrom`，
first-contact trust 则由显式的 non-WireGuard enrollment surface 处理。
