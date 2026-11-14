# ControlAPI

[リソース別の設定](../resources.md#controlapi) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

状態の取得と設定変更に使うHTTP Control APIを有効化します。Unixソケット経由の制御とWebConsoleは別です。

## 指定条件と参照関係

- ControlAPIは1つだけ対応します。HTTP関連のCLIフラグを明示した場合は、対応するリソース由来の値より優先します。リソースも有効化するフラグもなければHTTP制御は無効です。
- tls.certFileとtls.keyFileは組で指定します。clientCAFileには両方が必要で、クライアント証明書の提示と検証を必須にします。tokenFrom.fileとtokenFrom.envは排他です。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: ControlAPI
  metadata:
    name: "<name>"
  spec:
    allowCIDRs: # 任意; array
      - "<string>" # string
    enabled: true # 任意; boolean
    listenAddress: "<string>" # 任意; string
    port: 1 # 任意; integer; minimum=1; maximum=65535
    tls: # 任意; object
      certFile: "<string>" # 任意; string
      clientCAFile: "<string>" # 任意; string
      keyFile: "<string>" # 任意; string
    tokenFrom: # 任意; object
      base64: true # 任意; boolean
      env: "<string>" # 任意; string
      file: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.enabled` | リソースがあれば省略時true、なければ無効です。HTTP待受のCLIフラグを明示すると有効になります。 |
| `spec.listenAddress` | 待受IP。省略時127.0.0.1です。空白でない値はIPアドレスである必要があり、指定しても接続元CIDR許可一覧は変更しません。 |
| `spec.port` | TCPポート。省略時65432。読込み値0も既定値を選び、他の値は1〜65535です。 |
| `spec.allowCIDRs` | 接続元IPの許可一覧。省略時127.0.0.1/32と::1/128で、空でない一覧を指定すると置換します。有効なCIDRが必要で、0.0.0.0/0と::/0は拒否します。ファイアウォール設定とは別です。 |
| `spec.tokenFrom` | 任意のBearerトークン取得元。省略時はトークン確認を行いませんが、接続元CIDRの制限は適用します。使う場合はfileかenvのどちらか1つを指定し、routerdリソースは参照しません。 |
| `spec.tokenFrom.file` | トークンのファイルパス。省略時は空でenvと排他です。HTTP APIの初期化時にrouterdが読み、読めなければ初期化は失敗します。 |
| `spec.tokenFrom.env` | routerdプロセスの環境変数名。省略時は空でfileと排他です。変数がなければHTTP APIの初期化は失敗します。 |
| `spec.tokenFrom.base64` | 省略時false。trueなら取得値の前後空白を除去して標準Base64で復号し、復号後も前後空白を除去します。不正な値や最終的な空トークンは初期化エラーです。 |
| `spec.tls` | 任意のサーバーTLS設定。証明書・鍵を指定しなければHTTP待受は平文です。 |
| `spec.tls.certFile` | サーバー証明書ファイル。省略時は未指定でkeyFileが必要です。起動時にX.509の鍵ペアとして読み込み、TLSの下限は1.2です。 |
| `spec.tls.keyFile` | サーバー秘密鍵のファイル。省略時は未指定でcertFileが必要です。ローカルファイルのパスでありリソース参照ではありません。 |
| `spec.tls.clientCAFile` | 任意のPEM形式クライアントCA束。certFileとkeyFileが必要で、クライアント証明書の検証を必須にします。ファイルがない場合や不正な場合は起動エラーです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [cmd/routerd/cmd_serve.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_serve.go) · [pkg/controlapi/server.go](https://github.com/imksoo/routerd/blob/main/pkg/controlapi/server.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

このKindには通常の状態値参照の許可フィールドが登録されていません。

[設定例](https://github.com/imksoo/routerd/blob/main/examples/pve-minimal-rr-b.yaml)
