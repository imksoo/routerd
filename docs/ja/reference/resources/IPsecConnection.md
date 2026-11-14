# IPsecConnection

[リソース別の設定](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

strongSwanのswanctl接続を生成し、routerdの全IPsec接続をまとめて同期します。CHILD_SAは通信による起動用のtrap設定で、接続の読み込みはSA確立の証明ではありません。

## 指定条件と参照関係

- preSharedKeyとcertificateRefのどちらか一方が必須です。certificateRefはswanctlのlocal.certsに直接渡し、routerdのリソースとしては解決しません。
- 集約設定はLinuxの/etc/routerd/swanctl、FreeBSDの/usr/local/etc/routerd/swanctlに置き、swanctl --load-all --fileで鍵を含む管理対象全体を同期します。証明書・信頼元・対応秘密鍵は、その設定の認証情報ストアに別途配置が必要です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    [ certificateRef: <string> ]
    [ cloudProviderHint: ("" | "aws" | "azure" | "gcp") ]
    leftSubnet: <string>
    localAddress: <string>
    [ phase1Proposals:
        - <string>
    ]
    [ phase2Proposals:
        - <string>
    ]
    [ preSharedKey: <string> ]
    remoteAddress: <string>
    rightSubnet: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.localAddress` | **必須** | string | 必須のローカル終端IPアドレスです。IPv4・IPv6を使え、ホスト名・インターフェース参照ではありません。 |
| `spec.remoteAddress` | **必須** | string | 必須のリモート終端IPv4・IPv6アドレスです。検証は両終端・トラフィックセレクター間のアドレスファミリー一致までは確認しません。 |
| `spec.preSharedKey` | **条件付き必須** | string | IKEの共通鍵そのものです。certificateRefを使わない場合は必須で、既定値・ファイル・環境変数参照はありません。管理するswanctlのsecrets節に出力します。 |
| `spec.certificateRef` | **条件付き必須** | string | strongSwanのlocal.certsが扱う証明書名・パスです。preSharedKeyを使わない場合に必須で、相手認証をpubkeyにします。証明書の作成やrouterdのSecret参照ではありません。 |
| `spec.phase1Proposals` | *任意* | array | IKE proposal一覧で、既定は[aes256-sha256-modp2048]です。指定一覧をそのままカンマで結合し、アルゴリズムの対応可否はrouterdで検証しません。 |
| `spec.phase2Proposals` | *任意* | array | ESP proposal一覧で、既定は[aes256-sha256]です。netという子接続に適用し、アルゴリズムの妥当性は読み込み時にstrongSwanが確認します。 |
| `spec.leftSubnet` | **必須** | string | 必須のローカルトラフィックセレクターCIDRです。IPv4・IPv6を使え、local_tsに出力します。 |
| `spec.rightSubnet` | **必須** | string | 必須のリモートトラフィックセレクターCIDRです。IPv4・IPv6を使え、remote_tsに出力します。 |
| `spec.cloudProviderHint` | *任意* | string; enum=["", "aws", "azure", "gcp"] | 任意のaws・azure・gcpラベルで、既定は空です。観測結果には載せますが、proposal・認証情報・swanctl生成には影響せず、クラウド資源も作成しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`
