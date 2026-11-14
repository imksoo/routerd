# DHCPv6PrefixDelegation

[リソース別の設定](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

上流でIPv6プレフィックスを取得します。下流・ホストのアドレス生成と適用はIPv6DelegatedAddressを使い、取得だけではクライアントへ広告しません。

## 指定条件と参照関係

- interfaceは既存論理インターフェース名が必要です。既定clientはrouterd-dhcpv6-clientです。networkd・dhcp6c・dhcpcdも認識してOS別設定を生成しますが、実行時の監督・状態ソケットはrouterdデーモン中心で、同等のライフサイクル対応とは限りません。
- profile・prefixLengthは外部クライアント設定生成と復元スナップショットの整合確認に使います。監督起動するrouterd-dhcpv6-clientにはprofile・prefix-length引数を渡さず、その取得処理で長さ要求には使いません。clientDUIDは渡します。
- iaid・duidTypeのYAML指定は対応しません。routerdデーモンのIA_PD IAIDは既定1で、clientDUIDがなければ上流MACからDUID-LLを生成します。requiredは起動待ちの強制にはなりません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    client: "<string>" # 任意; string
    clientDUID: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    prefixLength: 1 # 任意; integer; minimum=1; maximum=128
    profile: "default" # 任意; string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"]
    required: true # 任意; boolean
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
| `spec.interface` | 同じRouterの論理上流インターフェースが必須です。外部dhcp6c・dhcpcdを使う場合、同じインターフェースのDHCPv6Addressのclientと一致が必要です。 |
| `spec.client` | 既定routerd-dhcpv6-clientで、代替はnetworkd・dhcp6c・dhcpcdです。現行の監督処理はrouterd DHCPv6のsystemdユニットも合成するため、外部指定だけで内蔵デーモンの停止を一般には保証しません。 |
| `spec.profile` | 空・default・ntt-ngn-direct-hikari-denwa・ntt-hgw-lan-pdです。NTTは対応する生成経路に/60とlink-layer DUID既定を補いますが、認証情報設定や内蔵デーモン引数変更は行いません。 |
| `spec.prefixLength` | 任意の1～128です。省略・0はNTTで/60、defaultでは長さ指定なしです。サーバーの割当長を保証せず、内蔵取得処理には上記制限があります。 |
| `spec.clientDUID` | 区切りなし・偶数桁16進数の任意DUIDです。コロン・空白は拒否し、空なら内蔵クライアントがMAC由来DUID-LLを生成します。IDは復元リースの再利用条件にも影響します。 |
| `spec.required` | 真偽値で既定falseです。読み込みますが現行実行処理は起動待ち方針に使いません。依存先は実際のBound状態・依存条件を使います。 |
| `spec.when` | 共通の状態条件で、既定無条件です。監督対象は解決した条件で絞り込むため、宣言を残しても不成立なら取得を停止できます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
