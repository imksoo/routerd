# High availability

![Diagram showing high availability with RouterdCluster file lease leader election gating routerd mutation while keepalived or CARP separately owns VIP address movement](/img/diagrams/operations-high-availability.png)

`RouterdCluster` 通过轻量的文件式租约，控制生成器与应用处理的行为，与 VIP 的拥有权相互独立。VIP 由哪台路由器持有由 keepalived 或 CARP 决定，routerd 则通过租约决定哪个节点可以变更主机配置。

领导节点持有 `spec.leasePath` 的排他锁，并在 `spec.leaseTTL` 到期前更新租约。待机节点持续执行控制器链以供观测，但会强制让变更状态的控制器以 dry-run 模式运作。在单次应用模式下，照常创建计划并记录集群状态，但跳过 apply。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: RouterdCluster
metadata:
  name: edge-ha
spec:
  peers:
    - routerd-01.lain.local
    - routerd-02.lain.local
  leaseTTL: 30s
  leasePath: /var/lib/routerd/ha-lease
```

若要在同一主机上只保留一个 routerd 进程，本地路径即已足够。
若要在多台主机间选出单一应用进程，请使用 advisory lock 能正确运作的共用文件系统路径。

最小配置示例请参阅 `examples/ha-2-node.yaml`。
