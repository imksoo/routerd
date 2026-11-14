# NAT44Rule

[リソース別の設定](../resources.md#nat44rule) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

送信インターフェースまたはEgressRoutePolicyで選んだデバイス経由で、転送IPv4の送信元を変換します。転送許可・変換先アドレスの割当自体は行いません。

## 指定条件と参照関係

- 実行コントローラーにはtype・egressInterface/egressPolicyRef・sourceRangesの形を使います。typeはmasquerade・snatのいずれか、送信先選択1種類以上、IPv4送信元範囲1件以上が必須です。両送信先指定があるとegressInterfaceが優先します。
- snatはsnatAddress・snatAddressFromのちょうど一方が必須で、masqueradeでは両方禁止です。動的参照は共通のresource・field検証に従い、有効アドレス未解決ならPendingです。optionalでもSNATアドレスは必要です。
- 別形状のoutboundInterface・sourceCIDRs・translationは検証・計画にありますが、実行NAT44コントローラーは描画ルールへ変換しません。Linuxで適用されずPendingや描画エラーになる場合があります。静的PFはこの形状のinterfaceAddress・addressだけに対応しpoolは非対応です。portMappingも描画効果がありません。動作する実行設定として使わないでください。
- 2形状は排他です。別形状はoutboundInterface・1件以上のIPv4 sourceCIDRs・translation.typeが必須で、従来形状のtype・egress・sourceRanges・宛先・SNAT項目を併用できません。translation.address・addressesの不要側は拒否されませんが、選択した分岐だけが使います。
- LinuxはdestinationCIDRs・destinationSetRefsをORにし、指定CIDR・集合を除外します。PFは静的宛先・除外CIDRを適用しますが、アドレス集合参照は両一覧とも使いません。ルール順はYAML順でなくmetadata.name順です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44Rule
  metadata:
    name: "<name>"
  spec:
    [ destinationCIDRs:
        - <string>
    ]
    [ destinationSetRefs:
        - <string>
    ]
    [ egressInterface: <string> ]
    [ egressPolicyRef: <string> ]
    [ excludeDestinationCIDRs:
        - <string>
    ]
    [ excludeDestinationSetRefs:
        - <string>
    ]
    [ outboundInterface: <string> ]
    [ snatAddress: <string> ]
    [ snatAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ sourceCIDRs:
        - <string>
    ]
    [ sourceRanges:
        - <string>
    ]
    [ translation:
        [ address: <string> ]
        [ addresses:
            - <string>
        ]
        [ portMapping:
            [ end: <integer> ]
            [ start: <integer> ]
            [ type: ("auto" | "preserve" | "range") ]
        ]
        type: ("interfaceAddress" | "address" | "pool")
    ]
    [ type: ("masquerade" | "snat") ]
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
| `spec.type` | **条件付き必須** | string; enum=["masquerade", "snat"] | 実行形状で必須です。masqueradeは送信側アドレス、snatは指定アドレスを使います。既定なしで別形状とは併用禁止です。 |
| `spec.egressInterface` | **条件付き必須** | string | 任意の裸の論理インターフェース・トンネル名です。指定時は設定内のインターフェース・DSLite索引での解決が必要で、egressPolicyRefより優先します。 |
| `spec.egressPolicyRef` | **条件付き必須** | string | egressInterface省略時の裸のEgressRoutePolicy名です。status.phase=Appliedと空でないselectedDeviceが必要です。NAT44の検証では存在を確認せず、名前違い・未解決はPendingです。 |
| `spec.sourceRanges` | **条件付き必須** | array | 実行形状で必須の1件以上のIPv4 CIDRです。LAN自動取得はなく、sourceCIDRs・translation形状と排他です。 |
| `spec.destinationCIDRs` | *任意* | array | 任意のIPv4宛先OR一覧で既定は空です。LinuxはdestinationSetRefsと和集合にし、PFはこの静的CIDRを使います。 |
| `spec.destinationSetRefs` | *任意* | array | 任意の既存IPAddressSet/<name>参照で重複は禁止です。既定は空、Linuxで有効、PFは無視します。 |
| `spec.excludeDestinationCIDRs` | *任意* | array | 本NATから除外する任意のIPv4宛先です。既定は空で、PFは本ルールの変換より前にno natを生成します。 |
| `spec.excludeDestinationSetRefs` | *任意* | array | 任意の既存IPAddressSet/<name>除外で既定は空です。Linuxだけで有効、PFは無視します。 |
| `spec.snatAddress` | **条件付き必須** | string | type=snat用の裸のIPv4変換アドレスです。snatAddressFromとちょうど一方が必須で、masqueradeでは禁止です。アドレス割当は行いません。 |
| `spec.snatAddressFrom` | **条件付き必須** | object | snat専用の共通resource・field・optional参照です。状態を先に読み、対応する宣言済みリソース値を次に使い、利用可能な値がなければPendingです。IPv4アドレスを提供するfieldを指定してください。 |
| `spec.outboundInterface` | **条件付き必須** | string | 別形状で必須の裸の設定インターフェース名です。前述のとおり実行コントローラーは対応しません。egressInterface・egressPolicyRefとの併用は禁止です。 |
| `spec.sourceCIDRs` | **条件付き必須** | array | 別形状で1件以上必須のIPv4プレフィックスです。既定なしで、実行コントローラーは読みません。 |
| `spec.translation` | **条件付き必須** | object | outboundInterface・sourceCIDRsと組み合わせる別形状の変換定義です。検証・計画への対応は実行適用の保証ではありません。 |
| `spec.translation.type` | **条件付き必須** | string; enum=["interfaceAddress", "address", "pool"] | 別形状ではinterfaceAddress・address・poolが必須です。静的PFは前2種だけに対応し、実行コントローラーは本形状を適用しません。 |
| `spec.translation.address` | **条件付き必須** | string | translation.type=addressで必須の裸のIPv4です。既定は空で、他の型は使いません。 |
| `spec.translation.addresses` | **条件付き必須** | array | type=poolで必須の1件以上の裸のIPv4です。受理・計画されますが、現行の実行コントローラー・静的PFは実装していません。 |
| `spec.translation.portMapping` | *任意* | object | 任意の別形状のポート方針で、検証・計画だけです。現行Linux実行・PF描画はこのポート設定を強制しません。 |
| `spec.translation.portMapping.type` | *任意* | string; enum=["auto", "preserve", "range"] | auto（既定）・preserve・rangeです。auto・preserveではstart・end禁止、rangeは両端必須です。現行描画の効果はありません。 |
| `spec.translation.portMapping.start` | *任意* | integer; minimum=1; maximum=65535 | range時だけ必須の開始値です。1～65535でend以下、既定0は未指定です。現行では検証だけです。 |
| `spec.translation.portMapping.end` | *任意* | integer; minimum=1; maximum=65535 | range時だけ必須の終了値です。1～65535でstart以上、既定0は未指定です。現行では検証だけです。 |
| `spec.when` | *任意* | object | 共通の有効化条件で、省略は無条件です。不成立で除外したルールは実効NAT生成に含めません。条件とスキーマ制約は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `egressInterface`, `snatAddress`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dslite-lan-range-snat.yaml)
