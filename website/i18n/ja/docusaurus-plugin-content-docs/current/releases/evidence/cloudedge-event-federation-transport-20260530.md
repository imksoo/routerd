# CloudEdge Event Federation Phase 2 トランスポートスモーク

Result: PASS

日付: 2026-05-30
ブランチ/ビルド: event-federation / f951fd471a7e
ビルドコマンド: `make dist`

エビデンスバンドル:
`/home/imksoo/routerd-labs/event-federation/evidence/20260530T091652Z-phase2-transport-f951fd47`

## トポロジー

トランスポート専用スモークは PVE のみのペアを使用しました。Azure、AWS、OCI の VM は起動していません。

- 送信側: router03 / 192.168.123.125 / `router03.lain.local`
- 受信側: router05 / 192.168.123.127 / `router05.lain.local`
- EventGroup: `cloudedge-event-smoke`
- 送信側 EventGroup nodeName: `onprem-event-node`
- 受信側 EventGroup nodeName: `cloud-event-node`
- オーバーレイ: `wg-hybrid`
- 送信側オーバーレイアドレス: `169.254.250.3/32`
- 受信側オーバーレイアドレス: `169.254.250.5/32`
- 受信側 eventd listen: `169.254.250.5:9443`
- 送信側 EventPeer endpoint: `http://169.254.250.5:9443`

emit されたイベントは `--source-node onprem-event-node` を使用し、送信側 EventGroup の `spec.nodeName` と一致。

## デプロイエビデンス

- `make dist` が静的 Linux アーティファクトで完了。
- ビルド `f951fd471a7e` の `routerd`、`routerctl`、`routerd-eventd` を両ノードにデプロイ。
- 生成された両方の設定が `routerd check` をパス。
- 受信側の `routerd-eventd@cloudedge-event-smoke.service` が `169.254.250.5:9443` で listen。
- 送信側の `routerd-eventd@cloudedge-event-smoke.service` は想定どおり push/prune のみ実行。
- オーバーレイの到達性が双方向でパス:
  - router03 -> `169.254.250.5`: 3/3 ping、0% loss
  - router05 -> `169.254.250.3`: 3/3 ping、0% loss
  - router03 から `http://169.254.250.5:9443/` への curl: eventd から HTTP 404、リスナー到達性を証明

## アサーション

### A. 送信側ローカルストア

PASS。送信側がイベントを格納:

- ID: `evt-phase2-smoke-20260530T092231Z`
- Group: `cloudedge-event-smoke`
- SourceNode: `onprem-event-node`
- Type: `routerd.client.ipv4.observed`
- Subject: `10.88.60.9/32`

### B. 送信側配信

PASS。送信側の配信が受信側 peer に到達:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- Peer: `cloud-event-node`
- Status: `delivered`
- Attempts: `1`
- DeliveredAt: `2026-05-30T09:22:41Z`

### C. 受信側ストア

PASS。受信側が同一イベント ID を格納:

- EventID: `evt-phase2-smoke-20260530T092231Z`
- 受信側の RecordedAt: `2026-05-30T09:22:43Z`
- 初回配信後の受信側ステータス: `received=1 duplicate=0 rejected=0 storedEvents=1`

### D. 冪等な重複

PASS。同一イベント ID を再 emit しても受信側に 2 番目のイベントは作成されなかった。

- 送信側配信は `attempts=1` のまま
- 受信側には ID `evt-phase2-smoke-20260530T092231Z` のイベントが 1 つのまま
- 受信側ステータスは `received=1 duplicate=0 storedEvents=1` のまま

### E. 不正な HMAC

PASS。不正な `X-Routerd-Signature` を持つ合成 POST が以下を返した:

- HTTP ステータス: `401 Unauthorized`
- ボディ: `bad signature`
- 受信側ストア変更なし
- 受信側ステータスは `rejected=1` に進行

### F. 期限切れイベント

PASS。期限切れイベントは送信側にローカルで格納されたが、push されなかった。

- 期限切れ EventID: `evt-expired-20260530T092347Z`
- ObservedAt: `2026-05-30T09:14:02Z`
- ExpiresAt: `2026-05-30T09:14:03Z`
- 送信側配信クエリ: `null`
- 受信側は期限切れイベントを受信しなかった

### G. 再起動後の再開

PASS。送信側 eventd が停止中に emit された新規イベントは、送信側 eventd サービスの再起動後に配信された。

- 再開 EventID: `evt-resume-20260530T092347Z`
- emit 前の送信側 eventd: `inactive`
- 送信側再起動前の受信側: オリジナルのメインイベントのみ
- 再起動後の配信: `delivered`、`attempts=1`、`deliveredAt=2026-05-30T09:24:18Z`
- 再開後の受信側ステータス: `received=2 duplicate=0 rejected=1 storedEvents=2`

## 既知のラボノート

- PVE 受信側はすでにファイアウォールの default-drop ポリシーを持っていました。eventd が新しいオーバーレイインターフェースでトラフィックを受け入れられるよう、このスモーク用に `WireGuardInterface/wg-hybrid` を router05 の既存管理ファイアウォールゾーンに追加しました。
- オーバーレイ peer アドレスの明示的な `/32` ルートリソースを追加:
  router03 に `169.254.250.5 dev wg-hybrid metric 120`、
  router05 に `169.254.250.3 dev wg-hybrid metric 120`。
- ランブックでは `routerctl federation event deliveries --group ...` を使用していましたが、
  現在の CLI は `--event-id` による配信ルックアップをサポートしています。アサーションでは
  `--event-id` を使用。
- `make dist` は当初 `routerd-eventd` をリリースペイロードに含んでいませんでした。
  ワーキングツリーでの追加により Makefile のリリースビルド/インストールリストに含まれるようになりました。

## 判定

CloudEdge Event Federation Phase 2 トランスポート専用スモークがパス:

- ローカル emit が送信側 SQLite に永続化
- outbox ループが EventPeer にプッシュ
- 受信側の HMAC 検証が有効なイベントを受け入れ
- 受信側が同一イベント ID を永続化
- 送信側配信が `delivered` になった
- 重複 ID は冪等に処理
- 不正な HMAC は 401 で拒否
- 期限切れイベントは配信されなかった
- 再起動後の再開により SQLite ベースの outbox 配信を証明
- EventSubscription、plugin トリガー、DynamicConfigPart、ARP observer、プロバイダーアクション、クラウドミューテーションは使用されていない
