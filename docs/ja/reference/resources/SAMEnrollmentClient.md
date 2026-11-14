# SAMEnrollmentClient

[リソース別の設定](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

ローカル登録要求を起動用RRのControlAPIへ送り、許可されたトポロジーを取得して動的SAMRRSet・任意のdirectピアグループとして既存コントローラーへ渡します。クラウド基盤の新規作成は行いません。

## 指定条件と参照関係

- claimRefはローカルSAMEnrollmentClaimを参照し、実行時はそのrrSetRefにSAMRRSet/nameが必要です。leaf要求はローカルにSAMEnrollmentPolicyを複製せず、リモートのポリシーを参照できます。
- bootstrapEndpointsを1件以上またはrrSocketが必要で、両者は排他ではありません。Unixソケットの後にHTTP(S)を一覧順で使います。通常登録は成功したRR応答を利用でき、directトポロジーは設定した接続先の合意が必要で、安全を確認できなければRR経路を維持します。
- direct-meshを無効化すると、RRへの接続前にキャッシュ済みdirectトポロジーを除きます。再試行・更新状態は永続化しますが、トポロジー更新成功はトンネル・BGPの開通を意味しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    bootstrapEndpoints: # 任意; array
      - "<string>" # string
    claimRef: "<string>" # 必須; string
    controlAPITLS: # 任意; object
      caFile: "<string>" # 任意; string
      certFile: "<string>" # 任意; string
      insecureSkipVerify: true # 任意; boolean
      keyFile: "<string>" # 任意; string
      serverName: "<string>" # 任意; string
    controlAPITokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
    retryBackoff: # 任意; object
      max: "<string>" # 任意; string
      min: "<string>" # 任意; string
    rrSocket: "<string>" # 任意; string
    stateTTLRefreshBefore: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.claimRef` | このRouterで宣言したSAMEnrollmentClaim/nameが必須です。状態field指定はありません。 |
| `spec.bootstrapEndpoints` | 任意のhost付き絶対HTTP(S) ControlAPI URL一覧です。rrSocketがなければ1件以上必要で、自動探索はありません。各登録要求には現行で10秒の要求コンテキストを使います。 |
| `spec.controlAPITokenFrom` | HTTP(S)クライアント用の任意Bearerトークン参照です。省略時はBearerヘッダーを付けません。指定時file・envは排他で、要求のjoin HMAC認証とは別です。 |
| `spec.controlAPITokenFrom.file` | ルーター上の生トークンファイルで、envと排他です。前後空白を除去し、HTTP接続先がある場合に欠落・読取不能・空値なら失敗します。 |
| `spec.controlAPITokenFrom.env` | トークンを持つrouterdプロセス環境変数名です。fileと排他で、環境ファイルのパスではありません。 |
| `spec.controlAPITokenFrom.base64` | 既定falseです。trueなら標準Base64デコード後にも前後空白を除去します。暗号化ではありません。 |
| `spec.controlAPITLS` | 任意のHTTPSクライアント設定で、Unixソケットには使いません。http URLをhttpsへ変更する指定ではありません。 |
| `spec.controlAPITLS.caFile` | 任意のPEM CAファイルです。指定時はこのファイルの証明書でクライアントの信頼元プールを置き換え、省略時は通常の信頼設定です。 |
| `spec.controlAPITLS.certFile` | 任意のクライアント証明書ファイルで、keyFileと同時指定が必要です。既定証明書・認証情報の自動生成はありません。 |
| `spec.controlAPITLS.keyFile` | 任意のクライアント秘密鍵ファイルです。certFileと同時指定が必要です。 |
| `spec.controlAPITLS.serverName` | 任意のTLS SNI・検証名の上書きです。空ならURLホストに従い、接続先の経路は変更しません。 |
| `spec.controlAPITLS.insecureSkipVerify` | 既定falseです。trueはサーバー証明書チェーン・ホスト名検証を無効にし、平文HTTP URLには影響しません。 |
| `spec.rrSocket` | 任意のローカルRR Unixソケット絶対パスです。HTTP(S)と併用でき、UnixクライアントにはBearer・TLS設定を使いません。 |
| `spec.stateTTLRefreshBefore` | 更新を前倒しするGo durationで、既定10分です。観測した正のRRリース期間以上なら、即時再更新を避けるため実行時にリースの半分へ調整します。検証は構文のみで正値までは要求しないため、正の値を使います。 |
| `spec.retryBackoff` | 任意の指数再試行設定です。失敗ごとにminを倍にしてmaxで制限します。directトポロジー回復には別の短い収束再試行もあります。 |
| `spec.retryBackoff.min` | Go durationで、既定10秒です。構文検証のみで0・負値も受け付けるため、即時・過去時刻の再試行を避けるには正値を使います。 |
| `spec.retryBackoff.max` | Go durationで、既定15分です。min・maxを両方明示した場合はmax>=minが必要です。正値を使い、検証時の比較には省略項目の既定値を含めません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go) · [pkg/controller/mobility/core_helpers.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/core_helpers.go) · [pkg/controlapi/client.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/client.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
