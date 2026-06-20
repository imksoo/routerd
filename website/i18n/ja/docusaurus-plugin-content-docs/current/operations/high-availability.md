# 高可用性

![Diagram showing high availability with RouterdCluster file lease leader election gating routerd mutation while keepalived or CARP separately owns VIP address movement](/img/diagrams/operations-high-availability.png)

`RouterdCluster` は、軽量なファイルベースのリースで、レンダラーと適用処理の動作を制御します。
VIP の所有権とは分離しています。
VIP をどのルーターが持つかは keepalived または CARP が決め、routerd はリースによって、ホスト設定を変更してよいノードを決めます。

リーダーは `spec.leasePath` の排他ロックを保持し、`spec.leaseTTL` が切れる前にリースを更新します。
スタンバイのノードは、観測のためにコントローラーチェーンを動かし続けますが、状態を変更するコントローラーは dry-run モードに強制されます。
one-shot apply モードでは、通常どおり計画を作成し、クラスターの状態を記録したうえで apply を skip します。

設定例:

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

同一ホスト上で routerd プロセスを 1 つだけにしたい場合は、ローカルパスで十分です。
複数ホストの間で 1 つの適用ノードを選びたい場合は、共有ファイルシステム上のパスを使います。
ファイルシステムは advisory lock が正しく動作する必要があります。
動作しない場合は、このリソースはステータスの記録だけに使い、実際の apply サービスは 1 台のノードだけで有効にしてください。

ステータスのフィールド:

| フィールド | 意味 |
| --- | --- |
| `phase` | `Leader` または `Standby` |
| `identity` | ローカルの routerd 識別情報（デフォルトはホスト名） |
| `holder` | 現在のリース保持者 |
| `expiresAt` | リースの有効期限 |
| `leasePath` | リースファイルのパス |

最小構成は `examples/ha-2-node.yaml` にあります。
