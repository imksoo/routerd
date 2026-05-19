# High availability

`RouterdCluster` は lightweight な file-based lease で renderer / applier work を
gate します。VIP ownership とは分離されています。VIP をどの router が持つかは
keepalived または CARP が決め、routerd は lease によって host configuration を
変更してよい node を決めます。

leader は `spec.leasePath` の exclusive lock を保持し、`spec.leaseTTL` が切れる前に
lease を更新します。standby node は観測のため controller chain を動かし続けますが、
mutating controller は dry-run mode に強制されます。one-shot apply mode では通常通り
plan を作成し、cluster status を記録して apply を skip します。

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

同一 host 上で routerd process を 1 つだけにしたい場合は local path で十分です。
複数 host 間で 1 つの applier を選びたい場合は advisory lock が正しく動作する
shared filesystem path を使います。

`examples/ha-2-node.yaml` に最小構成があります。
