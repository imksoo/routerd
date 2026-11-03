# 高可用性

![Diagram showing high availability with RouterdCluster file lease leader election gating routerd mutation while keepalived or CARP separately owns VIP address movement](/img/diagrams/operations-high-availability.png)

`RouterdCluster` は、軽量なファイルベースのリースで、レンダラーと適用処理の動作を制御します。VIP の所有権とは分離しています。VIP をどのルーターが持つかは keepalived または CARP が決め、routerd はリースによって、ホスト設定を変更してよいノードを決めます。

リーダーは `spec.leasePath` の排他ロックを保持し、`spec.leaseTTL` が切れる前にリースを更新します。スタンバイのノードは、観測のためにコントローラーチェーンを動かし続けますが、状態を変更するコントローラーは dry-run モードに強制されます。one-shot apply モードでは、通常どおり計画を作成し、クラスターの状態を記録したうえで apply を skip します。

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

同一ホスト上で routerd プロセスを 1 つだけにしたい場合は、ローカルパスで十分です。複数ホストの間で 1 つの適用ノードを選びたい場合は、共有ファイルシステム上のパスを使います。ファイルシステムは advisory lock が正しく動作する必要があります。動作しない場合は、このリソースはステータスの記録だけに使い、実際の apply サービスは 1 台のノードだけで有効にしてください。

ステータスのフィールド:

| フィールド | 意味 |
| --- | --- |
| `phase` | `Leader` または `Standby` |
| `identity` | ローカルの routerd 識別情報（デフォルトはホスト名） |
| `holder` | 現在のリース保持者 |
| `expiresAt` | リースの有効期限 |
| `leasePath` | リースファイルのパス |

最小構成は `examples/ha-2-node.yaml` にあります。

### Linux の起動時に保存済み VRRP 役割を扱う方法

Linux では起動時に `when` を評価する前に、管理している keepalived の設定と
現在の実状態から役割を確認します。以前の `master` という保存値だけで、
待機系として戻ったホストに共通アドレスや依存サービスを適用しません。
デーモンだけを再起動し、ゲートウェイを引き続き所有している場合は master と認識し、
依存サービスを維持します。役割の観測は最初のコントローラー起動より前に行い、
設定更新時の世代切り替え順序は維持します。HA 全体の実機試験の通過を意味するものではありません。

BGP 宣言をすべて削除した場合、daemon の停止・所有する FIB 経路の削除・管理対象 systemd
補助 unit の削除が完了してから、BGPRouter/BGPPeer の状態表示を退役させます。
削除失敗時は状態と再試行のための完了記録を保持します。リース同期の宣言を削除した場合は、
旧 controller 世代の停止後に状態表示を退役させ、送信元と受信先のリースファイルは保持します。
`when` で一時的に無効な宣言の表示は保持します。通常の世代停止を新たな同期障害として
表示しませんが、稼働中の転送タイムアウトは引き続き障害として表示します。

IPv4 の unicast VRRP では、選出用インターフェース上の無条件な `IPv4StaticAddress` が
一意に決まる場合、その機器固有アドレスを keepalived の `unicast_src_ip` と
`track_src_ip` に設定します。共有 VIP が先に存在し、BACKUP 通知で後から削除されても、
受信ソケットを共有 VIP に固定させません。指定した送信元がなければ fault として扱います。
同じインターフェースに旧ネットワークのアドレスもある場合は、すべての VRRP 通信相手を
含むネットワークに属する、唯一の送信元を選びます。
multicast、または静的な送信元が一意に決まらない構成は従来の選択に従い、
この静的送信元の検証結果には含めません。
