# ControlAPI

[リソース別の設定](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

状態の取得と設定変更に使うHTTP Control APIを有効化します。Unixソケット経由の制御とWebConsoleは別です。

## 指定条件と参照関係

- ControlAPIは1つだけ対応します。HTTP関連のCLIフラグを明示した場合は、対応するリソース由来の値より優先します。リソースも有効化するフラグもなければHTTP制御は無効です。
- tls.certFileとtls.keyFileは組で指定します。clientCAFileには両方が必要で、クライアント証明書の提示と検証を必須にします。tokenFrom.fileとtokenFrom.envは排他です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    [ allowCIDRs:
        - <string>
    ]
    [ enabled: <boolean> ]
    [ listenAddress: <string> ]
    [ port: <integer> ]
    [ tls:
        [ certFile: <string> ]
        [ clientCAFile: <string> ]
        [ keyFile: <string> ]
    ]
    [ tokenFrom:
        [ base64: <boolean> ]
        [ env: <string> ]
        [ file: <string> ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.enabled` | *任意* | boolean | リソースがあれば省略時true、なければ無効です。HTTP待受のCLIフラグを明示すると有効になります。 |
| `spec.listenAddress` | *任意* | string | 待受IP。省略時127.0.0.1です。空白でない値はIPアドレスである必要があり、指定しても接続元CIDR許可一覧は変更しません。 |
| `spec.port` | *任意* | integer; minimum=1; maximum=65535 | TCPポート。省略時65432。読込み値0も既定値を選び、他の値は1〜65535です。 |
| `spec.allowCIDRs` | *任意* | array | 接続元IPの許可一覧。省略時127.0.0.1/32と::1/128で、空でない一覧を指定すると置換します。有効なCIDRが必要で、0.0.0.0/0と::/0は拒否します。ファイアウォール設定とは別です。 |
| `spec.tokenFrom` | *任意* | object | 任意のBearerトークン取得元。省略時はトークン確認を行いませんが、接続元CIDRの制限は適用します。使う場合はfileかenvのどちらか1つを指定し、routerdリソースは参照しません。 |
| `spec.tokenFrom.file` | **条件付き必須** | string | トークンのファイルパス。省略時は空でenvと排他です。HTTP APIの初期化時にrouterdが読み、読めなければ初期化は失敗します。 |
| `spec.tokenFrom.env` | **条件付き必須** | string | routerdプロセスの環境変数名。省略時は空でfileと排他です。変数がなければHTTP APIの初期化は失敗します。 |
| `spec.tokenFrom.base64` | *任意* | boolean | 省略時false。trueなら取得値の前後空白を除去して標準Base64で復号し、復号後も前後空白を除去します。不正な値や最終的な空トークンは初期化エラーです。 |
| `spec.tls` | *任意* | object | 任意のサーバーTLS設定。証明書・鍵を指定しなければHTTP待受は平文です。 |
| `spec.tls.certFile` | **条件付き必須** | string | サーバー証明書ファイル。省略時は未指定でkeyFileが必要です。起動時にX.509の鍵ペアとして読み込み、TLSの下限は1.2です。 |
| `spec.tls.keyFile` | **条件付き必須** | string | サーバー秘密鍵のファイル。省略時は未指定でcertFileが必要です。ローカルファイルのパスでありリソース参照ではありません。 |
| `spec.tls.clientCAFile` | *任意* | string | 任意のPEM形式クライアントCA束。certFileとkeyFileが必要で、クライアント証明書の検証を必須にします。ファイルがない場合や不正な場合は起動エラーです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/controlapi/server.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/server.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。

[設定例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
