# IPsecConnection

[リソース別の設定](../resources.md#ipsecconnection) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

strongSwanのswanctl接続を生成し、routerdの全IPsec接続をまとめて同期します。CHILD_SAは通信による起動用のtrap設定で、接続の読み込みはSA確立の証明ではありません。

## 指定条件と参照関係

- preSharedKeyとcertificateRefのどちらか一方が必須です。certificateRefはswanctlのlocal.certsに直接渡し、routerdのリソースとしては解決しません。
- 集約設定はLinuxの/etc/routerd/swanctl、FreeBSDの/usr/local/etc/routerd/swanctlに置き、swanctl --load-all --fileで鍵を含む管理対象全体を同期します。証明書・信頼元・対応秘密鍵は、その設定の認証情報ストアに別途配置が必要です。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPsecConnection
  metadata:
    name: "<name>"
  spec:
    certificateRef: "<string>" # 任意; string
    cloudProviderHint: "" # 任意; string; enum=["", "aws", "azure", "gcp"]
    leftSubnet: "<string>" # 必須; string
    localAddress: "<string>" # 必須; string
    phase1Proposals: # 任意; array
      - "<string>" # string
    phase2Proposals: # 任意; array
      - "<string>" # string
    preSharedKey: "<string>" # 任意; string
    remoteAddress: "<string>" # 必須; string
    rightSubnet: "<string>" # 必須; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.localAddress` | 必須のローカル終端IPアドレスです。IPv4・IPv6を使え、ホスト名・インターフェース参照ではありません。 |
| `spec.remoteAddress` | 必須のリモート終端IPv4・IPv6アドレスです。検証は両終端・トラフィックセレクター間のアドレスファミリー一致までは確認しません。 |
| `spec.preSharedKey` | IKEの共通鍵そのものです。certificateRefを使わない場合は必須で、既定値・ファイル・環境変数参照はありません。管理するswanctlのsecrets節に出力します。 |
| `spec.certificateRef` | strongSwanのlocal.certsが扱う証明書名・パスです。preSharedKeyを使わない場合に必須で、相手認証をpubkeyにします。証明書の作成やrouterdのSecret参照ではありません。 |
| `spec.phase1Proposals` | IKE proposal一覧で、既定は[aes256-sha256-modp2048]です。指定一覧をそのままカンマで結合し、アルゴリズムの対応可否はrouterdで検証しません。 |
| `spec.phase2Proposals` | ESP proposal一覧で、既定は[aes256-sha256]です。netという子接続に適用し、アルゴリズムの妥当性は読み込み時にstrongSwanが確認します。 |
| `spec.leftSubnet` | 必須のローカルトラフィックセレクターCIDRです。IPv4・IPv6を使え、local_tsに出力します。 |
| `spec.rightSubnet` | 必須のリモートトラフィックセレクターCIDRです。IPv4・IPv6を使え、remote_tsに出力します。 |
| `spec.cloudProviderHint` | 任意のaws・azure・gcpラベルで、既定は空です。観測結果には載せますが、proposal・認証情報・swanctl生成には影響せず、クラウド資源も作成しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/ipsec/ipsec.go](https://github.com/imksoo/routerd/blob/main/pkg/ipsec/ipsec.go) · [cmd/routerd/ipsec_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/ipsec_apply.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`
