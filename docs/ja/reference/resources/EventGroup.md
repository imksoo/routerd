# EventGroup

[リソース別の設定](../resources.md#eventgroup) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: federation.routerd.net/v1alpha1` · `metadata.name`

共有するイベントグループと、このルーターの識別名・配送先取得・受信・ローカル保持設定を宣言します。

## 指定条件と参照関係

- nodeNameが必須です。metadata.nameがグループ識別名で、相手側と同じグループ名・共有秘密鍵を使います。識別情報だけのグループならsecretFileを省略できますが、通信ヘルパーはsecretFile指定時のみ起動します。Appliedは実行設定の書き込み完了で、相手への配送成功ではありません。
- listen.address・peersFrom・本グループを参照するEventPeerがある場合、設定検証でsecretFileが必要です。auth.secretRefでは代用できません。eventd起動時にファイルが存在し、前後の空白を除いた秘密鍵が空でない必要があります。
- 受信開始はaddressの有無ではなくlisten.port>0で決まります。正のポートとともに具体的なaddressを指定してください。addressが空でポートだけ正の場合、全アドレスで待ち受ける可能性があります。ポート0はTCP受信を無効にし、送信・保持削除は継続できます。
- peersFromはSAMNodeSet内のeventEndpointがあるノードを取得し、nodeRefがnodeNameと同じ自ノードは除外します。相手nodeNameで重複をまとめ、後の取得元が先を上書きし、最後に静的EventPeerが優先します。必須の取得元が欠けるとPendingになり、前の実行設定を残します。optionalの欠落は飛ばします。
- 保持削除はeventdが起動時と定期処理（既定1分）でローカルに実施します。期間・件数制限を両方適用し、期間はobservedAt、件数は観測日時が新しい順で残します。各イベントのexpiresAtやローカルEventRule履歴の保持とは別です。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: federation.routerd.net/v1alpha1
  kind: EventGroup
  metadata:
    name: "<name>"
  spec:
    auth: # 任意; object
      mode: "" # 任意; string; enum=["", "hmac"]
      secretFile: "<string>" # 任意; string
      secretRef: "<string>" # 任意; string
    listen: # 任意; object
      address: "<string>" # 任意; string
      port: 0 # 任意; integer; minimum=0; maximum=65535
    nodeName: "<string>" # 必須; string
    peersFrom: # 任意; array
      - # object
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    replayWindow: "<string>" # 任意; string
    retention: # 任意; object
      maxAge: "<string>" # 任意; string
      maxEvents: 0 # 任意; integer; minimum=0
    when: # 任意; object
      state: # 任意; object
        "<key>": # 任意; object
          contains: "<string>" # 任意; string
          equals: "<string>" # 任意; string
          exists: true # 任意; boolean
          for: "<string>" # 任意; string
          in: # 任意; array
            - "<string>" # string
          maxAge: "<string>" # 任意; string
          status: "set" # 任意; string; enum=["set", "unset", "unknown"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.nodeName` | 必須の空でない自ノード識別名です。ローカル生成した共有イベントに付与します。ホスト名の自動設定はなく、peersFrom利用時はSAMNodeSetのnodeRefに合わせます。 |
| `spec.peersFrom` | 任意の順序付き取得元一覧で、既定は空です。すべてoptionalでも、配送先が0件でも、指定するとauth.secretFileが必要です。 |
| `spec.peersFrom[].resource` | SAMNodeSet/<name>が必須です。形式を検証しますが、宣言がない場合は通常の状態値参照のように拒否せず、実行時にoptional・Pendingで処理します。 |
| `spec.peersFrom[].optional` | 既定falseでは欠落が新設定を止めます。trueは欠落を飛ばしますが、不正形式や解釈不能な取得元はエラーです。 |
| `spec.retention` | 任意のローカル共有イベント保持設定です。既定では期間・件数制限なしです。eventdの保持削除が動く間に適用します。 |
| `spec.retention.maxEvents` | グループごとの最大件数で、0以上です。既定0は無制限、正数は期間削除後にobservedAt・idが新しいものを残します。 |
| `spec.retention.maxAge` | 任意のGo形式期間で、既定の空は無制限です。0・負数も検証を通り、実行設定から省略されるため期間制限なしです。 |
| `spec.auth` | 任意の通信完全性設定です。現行通信は常にHMACを使い、無認証モードはありません。 |
| `spec.auth.mode` | 空またはhmacを受け付け、既定は空です。通信時はどちらもHMACになり、別方式の選択には使いません。 |
| `spec.auth.secretRef` | 既定は空です。受理されますが現行の処理は解決せず、secretFileの代用にもリソース参照検証の対象にもなりません。 |
| `spec.auth.secretFile` | 共有HMAC秘密鍵のファイルパスです。空は通信ヘルパー無効で、前述の受信アドレス・配送先指定時には必須です。秘密鍵の自動配布は行いません。 |
| `spec.listen` | 任意のHTTP受信設定で、既定はaddress空・port 0です。受信には両方を明示してください。 |
| `spec.listen.address` | 受信待受アドレスで、既定は空です。非空ならport 1～65535とsecretFileが必要です。デーモンはaddressが空という理由だけでは受信を止めず、portの規則で決まります。 |
| `spec.listen.port` | 0～65535の整数で、既定0は受信無効です。正数はaddressで平文HTTPを受信します。本リソースにTLS証明書の設定項目はありません。 |
| `spec.replayWindow` | 署名付き要求の時刻ずれ許容範囲をGo形式で指定します。省略・0・負数は5mです。イベント保持期間やメッセージ重複排除TTLではありません。 |
| `spec.when` | 共通の有効化条件で、省略時は無条件です。有効設定の絞り込みを通して通信設定・サービス動作を制御します。条件とスキーマ制限は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_event.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_event.go) · [pkg/controller/eventfederation/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/eventfederation/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/eventd/config.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/config.go) · [pkg/eventd/prune.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/prune.go) · [pkg/eventd/receiver.go](https://github.com/imksoo/routerd/blob/main/pkg/eventd/receiver.go) · [cmd/routerd-eventd/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-eventd/main.go) · [pkg/state/federation.go](https://github.com/imksoo/routerd/blob/main/pkg/state/federation.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `group`, `nodeName`, `peers`, `peersFrom`, `pendingSources`, `listenAddress`, `listenPort`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-mobility-demo/onprem.yaml)
