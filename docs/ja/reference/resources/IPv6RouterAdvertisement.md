# IPv6RouterAdvertisement

[リソース別の設定](../resources.md#ipv6routeradvertisement) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

LANのIPv6ルーター広告オプションと、以前の委任プレフィックスの非推奨化を設定します。

## 指定条件と参照関係

- interfaceとprefixまたはprefixFromが必須です。静的生成と実行時生成は異なり、現行実行時はinterface・enable-ra・オプションを出力してもprefix・prefixFromから通常dhcp-rangeを生成しません。実際に動作するDHCPv6Server・アドレス設定と組み合わせ、実通信のPIOを確認してください。スキーマ・Appliedだけでは広告を証明しません。
- dnsslとdnsslFromは排他ですが、prefixとprefixFrom、rdnssとrdnssFromは併用できます。共通参照はoptional trueでも既存リソース・提供フィールド明示が必要です。廃止prefixSource・ready_whenは拒否します。
- prefixFromがIPv6DelegatedAddressなら、有効期限内の旧/64をpreferred 0・残存valid lifetimeのPIOとして直接送信します。実リンクローカル送信元・hop limit 255を使い、現在の通常広告は共有dnsmasq側です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6RouterAdvertisement
  metadata:
    name: "<name>"
  spec:
    [ configPath: <string> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ dnssl:
        - <string>
    ]
    [ dnsslFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    interface: <string>
    [ mFlag: <boolean> ]
    [ mtu: <integer> ]
    [ oFlag: <boolean> ]
    [ pidFile: <string> ]
    [ preferredLifetime: <string> ]
    [ prefix: <string> ]
    [ prefixFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ prfPreference: ("" | "low" | "medium" | "high") ]
    [ rdnss:
        - <string>
    ]
    [ rdnssFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ validLifetime: <string> ]
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
| `spec.interface` | **必須** | string | 必須の論理インターフェースです。他のアドレス資源と異なりRAのこの値は全体参照検証で存在確認せず、実行時は別名解決後に未解決ならOS名として使います。Interfaceを宣言して名前を指定してください。 |
| `spec.prefix` | **条件付き必須** | string | 固定IPv6プレフィックスで、prefixFrom.resourceがなければ必須です。prefixFromの状態補完として併用できます。現行実行時はこの値から通常のプレフィックス範囲を生成しないため、宣言だけで広告されるとは限りません。 |
| `spec.prefixFrom` | **条件付き必須** | object | 既存resource・提供fieldの明示が必要な状態値参照です。表示プレフィックスと、IPv6DelegatedAddress参照時の非推奨化対象旧/64の取得に使います。未解決だけでは通常設定生成を待機にしません。 |
| `spec.rdnss` | *任意* | array | 任意のIPv6 DNS一覧で、既定空です。rdnssFromと結合し、共有dnsmasqにタグなしoption6:dns-serverとして出力するため、この生成処理ではRAインターフェース別に分離しません。 |
| `spec.rdnssFrom` | *任意* | array | 共通の状態値参照一覧で、既定参照なしです。現行RA処理は未解決の待機理由を捨て、得られた値だけを使います。optionalをサービス準備条件としては使えません。 |
| `spec.dnssl` | *任意* | array | 任意の検索ドメイン一覧で既定空です。各値は空白・スラッシュ・カンマのない単一ドメインで、dnsslFromと排他です。共有タグなしoption6:domain-searchに出力します。 |
| `spec.dnsslFrom` | *任意* | array | DNSZone/<name>・field: zoneに限定した参照一覧です。各参照先の存在が必要で、状態前でも宣言zoneを読み、dnsslと排他です。現行通常生成は未解決の待機理由を無視します。 |
| `spec.mFlag` | *任意* | boolean | 真偽値で既定falseです。旧委任プレフィックスを非推奨化する直接RAで使いますが、現行通常dnsmasq生成ではMへ反映しません。 |
| `spec.oFlag` | *任意* | boolean | 真偽値で既定falseです。mFlagと同じ直接非推奨化経路で使い、通常dnsmasqのOビット切替にはなりません。 |
| `spec.mtu` | *任意* | integer; minimum=1280; maximum=65535 | 任意の1280～65535です。省略・0は明示MTUなしですが、PathMTU由来値があればそちらを優先します。dnsmasqのra-param mtu:<値>に渡します。 |
| `spec.prfPreference` | *任意* | string; enum=["", "low", "medium", "high"] | 空・mediumは通常優先度です。low・highはdnsmasq優先度指定と旧プレフィックス直接広告に使います。 |
| `spec.preferredLifetime` | *任意* | string | 任意文字列で既定空です。現行の通常実行時生成では使いません。旧プレフィックス直接PIOはこの指定によらずpreferred lifetime 0です。 |
| `spec.validLifetime` | *任意* | string | 通常のdnsmasq実行時生成では現在プレフィックスのPIO期限ではなくra-paramのルーター有効期間です。十進秒数で指定し、省略時はdnsmasq既定です。旧プレフィックス直接RAは十進秒として読み、空・不正なら1800、上限65535で、旧PIO自体は残りリース期間を使います。 |
| `spec.configPath` | *任意* | string | 任意文字列で既定空です。現行実行コントローラーは共有dnsmasqのConfigPath・PIDFileオプションを使い、このリソース別指定は使用しません。 |
| `spec.pidFile` | *任意* | string | 任意文字列で既定空です。現行実行コントローラーは共有dnsmasqのConfigPath・PIDFileオプションを使い、このリソース別指定は使用しません。 |
| `spec.when` | *任意* | object | 共通の状態条件で既定無条件です。不成立なら有効設定から除外してPending/WhenFalseです。 |
| `spec.dependsOn` | *任意* | array | 共通AND依存一覧で既定空です。不成立はPending/DependsOnFalseとなり旧プレフィックス直接広告を省略しますが、通常共有dnsmasqの行生成はこの条件を確認しません。通常サービス全体の抑止スイッチとはなりません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/routeradvertisement.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/routeradvertisement.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `prefix`, `rdnss`, `configPath`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
