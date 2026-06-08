# Event Federation リファレンス

![観測されたローカルファクトから EventGroup、routerd-eventd push 配信、EventSubscription マッチング、plugin 由来の DynamicConfigPart 出力までを示す Event Federation の図](/img/diagrams/reference-event-federation.png)

> 実験的（CloudEdge）。設計と不変条件については [ADR 0006: CloudEdge Event Federation](../adr/0006-event-federation.md) を、
> 実践的な例については how-to の
> [Event Federation subscription](../how-to/event-federation-subscription.md) を参照してください。

Event Federation は、routerd ノード間で **型付きの観測ファクト**（例: 「このクライアント
IPv4 が観測された」「このアドレスが期限切れになった」）を overlay 経由で交換し、
サブスクライバーがマッチしたイベントを plugin 経由で導出設定に変換する仕組みです。
[選択的アドレス移動性](./selective-address-mobility)の下にある制御プレーン基盤であり、
あるノードで観測されたアドレスが別のノードの `RemoteAddressClaim`（capture）になります。

モデルは **冪等な観測ファクトイベントの at-least-once 配信**です。
イベントは世界についての不変の記述（「observed」）であり、命令的コマンドではありません。
同じイベントから同じ状態を再導出する受信者にとっては no-op です。

## Kind

### `EventGroup`

ノードが参加するバス。1 ノードはグループごとに 1 つの identity を持ちます。

| フィールド | 意味 |
|---|---|
| `nodeName` | グループ内でのこのノードの identity。発行イベントに `sourceNode` として刻印される。 |
| `retention` | ローカルストアがイベントを保持する件数/期間の上限。空/ゼロ = 無制限。 |
| `auth` | ピア配信（push）用の HMAC シークレットマテリアル。 |
| `listen` | インバウンドのピア push 用レシーバーバインド（`address`）。空 = push のみ（レシーバーなし）。 |
| `replayWindow` | リプレイ保護のために受け入れるメッセージタイムスタンプのスキュー上限を示す Go duration（デフォルト `5m`）。 |

### `EventPeer`

このノードがイベントを push するリモートノード。

| フィールド | 意味 |
|---|---|
| `groupRef` | このピアが所属する `EventGroup`（必須）。 |
| `nodeName` | リモートピアのノード identity（必須）。 |
| `endpoint` | push 先のベース URL。例: `http://10.99.0.7:8787`（push には必須）。 |
| `direction` | 配信方向。`push` のみサポート。空の場合は `push` がデフォルト。 |
| `types` | オプションのイベントタイプ許可リスト。空の場合は全配信。 |
| `subjectPrefixes` | オプションのサブジェクトプレフィクス許可リスト。空の場合は全配信。 |

### `EventSubscription`

マッチしたイベントを `DynamicConfigPart` を発行する plugin 呼び出しに変換します。

| フィールド | 意味 |
|---|---|
| `groupRef` | 消費元の `EventGroup`。 |
| `match` | タイプ/サブジェクトによるイベントマッチ条件。 |
| `trigger.pluginRef` | マッチしたイベントで呼び出す `Plugin`。 |
| `trigger.batchWindow` | マッチしたイベントを 1 回の呼び出しに集約する Go duration。 |
| `trigger.debounce` | 最後のマッチイベント後まで呼び出しを遅延させる Go duration。 |

## `routerctl federation` CLI

```
routerctl federation event emit  --group <g> --type <topic> --subject <entity> [--source-node <n>] [--ttl <dur>] [--payload k=v ...]
routerctl federation event list  --group <g>
routerctl federation event deliveries --group <g>
```

`emit` は観測ファクトをローカルストアに記録します（例:
`--type routerd.client.ipv4.observed --subject 10.88.60.9/32`）。`list` は記録された
イベントを表示し、`deliveries` はピアごとの push 配信状態を表示します。

> セルフキャプチャガード（ADR 0006 の no-feedback-loop 不変条件）: ノードは自身が
> ローカルの `RemoteAddressClaim` で capture しているアドレスに対して
> `routerd.client.ipv4.observed` を発行してはなりません。さもなければ、配信された
> capture アドレスが新しい観測としてループバックしてしまいます。

## トランスポート — `routerd-eventd`

`routerd-eventd@<group>` はグループごとの長寿命デーモンで（Linux では生成された
systemd unit、FreeBSD では rc.d によって管理）、以下を行います。

- ローカルに記録されたイベントを HTTP 経由で各 `EventPeer` に **push** し、グループ
  HMAC で署名します。レシーバーは署名を検証し、`replayWindow` 外のメッセージを拒否します。
- **配信** をピアごと・イベントごとに記録し、at-least-once リトライの範囲を限定して
  観測可能にします。
- グループの `retention` に従ってローカルイベントストアを**プルーニング**します。

outbox は `sourceNode` ガードを持ち、受信したイベントが発信元に再転送されることは
ありません（配信ループなし）。

## Subscription → plugin → DynamicConfigPart フロー

1. ノードが観測ファクトを発行（`routerctl federation event emit`、または将来の
   observer）。
2. `routerd-eventd` がピアに配信し、各ピアが自身のイベントストアに記録。
3. ピアの `EventSubscription` がイベントにマッチし、`trigger.pluginRef` を呼び出す
   （`batchWindow` / `debounce` で集約）。
4. plugin が `DynamicConfigPart`（例: `RemoteAddressClaim`）を返し、
   [dynamic-config](./dynamic-config.md) チェーンが effective config に統合して
   データプレーンに reconcile する。

これにより運用者が書く intent は宣言的に保たれます。運用者は group/peers/subscription
を宣言し、claim、capture、action plan は**導出**されます。
