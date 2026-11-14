# DHCPv6PrefixDelegation

[リソース別の設定](../resources.md#dhcpv6prefixdelegation) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

上流でIPv6プレフィックスを取得します。下流・ホストのアドレス生成と適用はIPv6DelegatedAddressを使い、取得だけではクライアントへ広告しません。

## 指定条件と参照関係

- interfaceは既存論理インターフェース名が必要です。既定clientはrouterd-dhcpv6-clientです。networkd・dhcp6c・dhcpcdも認識してOS別設定を生成しますが、実行時の監督・状態ソケットはrouterdデーモン中心で、同等のライフサイクル対応とは限りません。
- profile・prefixLengthは外部クライアント設定生成と復元スナップショットの整合確認に使います。監督起動するrouterd-dhcpv6-clientにはprofile・prefix-length引数を渡さず、その取得処理で長さ要求には使いません。clientDUIDは渡します。
- iaid・duidTypeのYAML指定は対応しません。routerdデーモンのIA_PD IAIDは既定1で、clientDUIDがなければ上流MACからDUID-LLを生成します。requiredは起動待ちの強制にはなりません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6PrefixDelegation
  metadata:
    name: "<name>"
  spec:
    [ client: <string> ]
    [ clientDUID: <string> ]
    interface: <string>
    [ prefixLength: <integer> ]
    [ profile: ("default" | "ntt-ngn-direct-hikari-denwa" | "ntt-hgw-lan-pd") ]
    [ required: <boolean> ]
    [ when:
        [ state:
            [ <name>:
                [ contains: <string> ]
                [ equals: <string> ]
                [ exists: <boolean> ]
                [ for: <string> ]
                [ in:
                    - <string>
                ]
                [ maxAge: <string> ]
                [ status: ("set" | "unset" | "unknown") ]
            ]
        ]
    ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.interface` | **必須** | string | 同じRouterの論理上流インターフェースが必須です。同じインターフェースにDHCPv6Addressもある場合、外部dhcp6c・dhcpcdのPD clientはそちらのclientと一致が必要です。 |
| `spec.client` | *任意* | string | 既定routerd-dhcpv6-clientで、代替はnetworkd・dhcp6c・dhcpcdです。現行の監督処理はrouterd DHCPv6のsystemdユニットも合成するため、外部指定だけで内蔵デーモンの停止を一般には保証しません。 |
| `spec.profile` | *任意* | string; enum=["default", "ntt-ngn-direct-hikari-denwa", "ntt-hgw-lan-pd"] | 空・default・ntt-ngn-direct-hikari-denwa・ntt-hgw-lan-pdです。NTTは対応する生成経路に/60とlink-layer DUID既定を補いますが、認証情報設定や内蔵デーモン引数変更は行いません。 |
| `spec.prefixLength` | *任意* | integer; minimum=1; maximum=128 | 任意の1～128です。省略・0はNTTで/60、defaultでは長さ指定なしです。割当長を保証せず内蔵取得にも渡しません。一方、HA事前配置の復元は有効長との完全一致が必要で、defaultかつ省略時は0比較となり通常の委任プレフィックスと一致しません。この経路には実際の長さを明示してください。 |
| `spec.clientDUID` | *任意* | string | 区切りなし・偶数桁16進数の任意DUIDです。コロン・空白は拒否し、空なら内蔵クライアントがMAC由来DUID-LLを生成します。IDは復元リースの再利用条件にも影響します。 |
| `spec.required` | *任意* | boolean | 真偽値で既定falseです。読み込みますが現行実行処理は起動待ち方針に使いません。依存先は実際のBound状態・依存条件を使います。 |
| `spec.when` | *任意* | object | 共通の状態条件で、既定無条件です。監督対象は解決した条件で絞り込むため、宣言を残しても不成立なら取得を停止できます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/profiles.go](https://github.com/imksoo/routerd/blob/main/pkg/api/profiles.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-dhcpv6-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv6-client/main.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `currentPrefix`, `dnsServers`, `sntpServers`, `domainSearch`, `aftrName`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
