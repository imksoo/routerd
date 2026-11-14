# SAMEnrollmentClient

[リソース別の設定](../resources.md#samenrollmentclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: mobility.routerd.net/v1alpha1` · `metadata.name`

ローカル登録要求を起動用RRのControlAPIへ送り、許可されたトポロジーを取得して動的SAMRRSet・任意のdirectピアグループとして既存コントローラーへ渡します。クラウド基盤の新規作成は行いません。

## 指定条件と参照関係

- claimRefはローカルSAMEnrollmentClaimを参照し、実行時はそのrrSetRefにSAMRRSet/nameが必要です。leaf要求はローカルにSAMEnrollmentPolicyを複製せず、リモートのポリシーを参照できます。
- bootstrapEndpointsを1件以上またはrrSocketが必要で、両者は排他ではありません。Unixソケットの後にHTTP(S)を一覧順で使います。通常登録は成功したRR応答を利用でき、directトポロジーは設定した接続先の合意が必要で、安全を確認できなければRR経路を維持します。
- direct-meshを無効化すると、RRへの接続前にキャッシュ済みdirectトポロジーを除きます。再試行・更新状態は永続化しますが、トポロジー更新成功はトンネル・BGPの開通を意味しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: mobility.routerd.net/v1alpha1
  kind: SAMEnrollmentClient
  metadata:
    name: "<name>"
  spec:
    [ bootstrapEndpoints:
        - <string>
    ]
    claimRef: <string>
    [ controlAPITLS:
        [ caFile: <string> ]
        [ certFile: <string> ]
        [ insecureSkipVerify: <boolean> ]
        [ keyFile: <string> ]
        [ serverName: <string> ]
    ]
    [ controlAPITokenFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
    [ retryBackoff:
        [ max: <string> ]
        [ min: <string> ]
    ]
    [ rrSocket: <string> ]
    [ stateTTLRefreshBefore: <string> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.claimRef` | **必須** | string | このRouterで宣言したSAMEnrollmentClaim/nameが必須です。状態field指定はありません。 |
| `spec.bootstrapEndpoints` | **条件付き必須** | array | 任意のhost付き絶対HTTP(S) ControlAPI URL一覧です。rrSocketがなければ1件以上必要で、自動探索はありません。各登録要求には現行で10秒の要求コンテキストを使います。 |
| `spec.controlAPITokenFrom` | *任意* | object | HTTP(S)クライアント用の任意Bearerトークン参照です。省略時はBearerヘッダーを付けません。指定時file・envは排他で、要求のjoin HMAC認証とは別です。 |
| `spec.controlAPITokenFrom.file` | **条件付き必須** | string | ルーター上の生トークンファイルで、envと排他です。前後空白を除去し、HTTP接続先がある場合に欠落・読取不能・空値なら失敗します。 |
| `spec.controlAPITokenFrom.env` | **条件付き必須** | string | トークンを持つrouterdプロセス環境変数名です。fileと排他で、環境ファイルのパスではありません。 |
| `spec.controlAPITokenFrom.base64` | *任意* | boolean | 既定falseです。trueなら標準Base64デコード後にも前後空白を除去します。暗号化ではありません。 |
| `spec.controlAPITLS` | *任意* | object | 任意のHTTPSクライアント設定で、Unixソケットには使いません。http URLをhttpsへ変更する指定ではありません。 |
| `spec.controlAPITLS.caFile` | *任意* | string | 任意のPEM CAファイルです。指定時はこのファイルの証明書でクライアントの信頼元プールを置き換え、省略時は通常の信頼設定です。 |
| `spec.controlAPITLS.certFile` | *任意* | string | 任意のクライアント証明書ファイルで、keyFileと同時指定が必要です。既定証明書・認証情報の自動生成はありません。 |
| `spec.controlAPITLS.keyFile` | *任意* | string | 任意のクライアント秘密鍵ファイルです。certFileと同時指定が必要です。 |
| `spec.controlAPITLS.serverName` | *任意* | string | 任意のTLS SNI・検証名の上書きです。空ならURLホストに従い、接続先の経路は変更しません。 |
| `spec.controlAPITLS.insecureSkipVerify` | *任意* | boolean | 既定falseです。trueはサーバー証明書チェーン・ホスト名検証を無効にし、平文HTTP URLには影響しません。 |
| `spec.rrSocket` | **条件付き必須** | string | 任意のローカルRR Unixソケット絶対パスです。HTTP(S)と併用でき、UnixクライアントにはBearer・TLS設定を使いません。 |
| `spec.stateTTLRefreshBefore` | *任意* | string | 更新を前倒しするGo durationで、既定10分です。観測した正のRRリース期間以上なら、即時再更新を避けるため実行時にリースの半分へ調整します。検証は構文のみで正値までは要求しないため、正の値を使います。 |
| `spec.retryBackoff` | *任意* | object | 任意の指数再試行設定です。失敗ごとにminを倍にしてmaxで制限します。directトポロジー回復には別の短い収束再試行もあります。 |
| `spec.retryBackoff.min` | *任意* | string | Go durationで、既定10秒です。構文検証のみで0・負値も受け付けるため、即時・過去時刻の再試行を避けるには正値を使います。 |
| `spec.retryBackoff.max` | *任意* | string | Go durationで、既定15分です。min・maxを両方明示した場合はmax>=minが必要です。正値を使い、検証時の比較には省略項目の既定値を含めません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_mobility.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_mobility.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/mobility/enrollmentclient.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/enrollmentclient.go) · [pkg/controller/mobility/core_helpers.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/mobility/core_helpers.go) · [pkg/controlapi/client.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/client.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `claimRef`, `observedRRSet`, `observedDirectPeerGroup`, `lastAttempt`, `lastSuccess`, `nextAttempt`, `backoff`, `failureCount`, `directTopologyPending`, `reason`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-dynamic-leaf-a-wg.yaml)
