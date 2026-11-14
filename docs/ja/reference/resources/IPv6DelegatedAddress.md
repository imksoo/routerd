# IPv6DelegatedAddress

[リソース別の設定](../resources.md#ipv6delegatedaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

DHCPv6PrefixDelegationのプレフィックスからローカルIPv6アドレスを生成・適用します。

## 指定条件と参照関係

- prefixDelegationは既存DHCPv6PrefixDelegationの裸名で、interfaceもRouter索引に必要です。通常の主系適用には有効なBoundプレフィックス、準備済みリンク、依存条件が必要です。HAのVMAC事前配置は主系取得前に新しい同期スナップショットを使えます。
- 生成のプレフィックス長はmax(64,委任長)です。subnetIDを16進数としてその境界へシフトしてORし、suffixは残りのホストビットにマスクしてORします。現行処理はsubnetIDが委任範囲に収まるか検査しないため、範囲内の値を指定してください。
- Linuxはip address、FreeBSDはifconfigで操作します。主系は適用後に読み戻してAppliedを公開し、DAD中などはPendingになり得ます。変更前のアドレスは有効期限まで非推奨として保持でき、/128は直結サブネットではなくホストアドレスとして扱います。
- sendRA・announceだけで現行アドレスコントローラーがRAサービスを起動することはありません。IPv6RouterAdvertisementまたはDHCPv6Serverを別途設定します。廃止prefixSource・ready_whenは拒否されます。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv6DelegatedAddress
  metadata:
    name: "<name>"
  spec:
    addressSuffix: <string>
    [ announce: <boolean> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    interface: <string>
    prefixDelegation: <string>
    [ prefixLength: <integer> ]
    [ sendRA: <boolean> ]
    [ subnetID: <string> ]
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
| `spec.prefixDelegation` | **必須** | string | DHCPv6PrefixDelegationのmetadata.nameが必須です。Kind/name・状態式ではなく、既定値はありません。 |
| `spec.interface` | **必須** | string | 適用先の宣言済み論理インターフェースが必須です。OS名はRouterから解決します。 |
| `spec.subnetID` | *任意* | string | 任意の16進文字列で既定0です。"10"は十進10ではなく0x10で、0x接頭辞も可能です。厳密な収容検証がないため委任範囲内の番号を指定してください。 |
| `spec.addressSuffix` | **必須** | string | ::1などのIPv6サフィックスが必須です。生成境界より下のホストビットだけを使い、絶対アドレスとしては扱いません。::1の自動補完はありません。 |
| `spec.prefixLength` | *任意* | integer; minimum=1; maximum=128 | 省略・0は生成長、通常/64です。実行時に同じ長さまたは128を許し、他の明示長はスキーマ範囲1～128を通っても適用で失敗します。/128は通常の直結プレフィックス経路を作りません。 |
| `spec.sendRA` | *任意* | boolean | 真偽値で既定falseです。一部のホスト依存・転送準備に使いますが、実行時のアドレスコントローラーでRAを自動起動するスイッチではありません。 |
| `spec.announce` | *任意* | boolean | 真偽値で既定falseです。RAサービスに関する制限はsendRAと同じで、自発的近隣広告・RA送信を保証しません。 |
| `spec.when` | *任意* | object | 共通条件で既定無条件です。不成立時は明示的な後片付け処理で生成アドレスを除去できます。 |
| `spec.dependsOn` | *任意* | array | 共通AND依存一覧で既定空です。不成立ならPending/DependsOnFalseです。HA事前配置はPD・VMAC前提の特別処理を持つため、実際のリソースphaseを使います。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/pdclient/derive.go](https://github.com/imksoo/routerd/blob/main/pkg/pdclient/derive.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/hostdeps/hostdeps.go](https://github.com/imksoo/routerd/blob/main/pkg/hostdeps/hostdeps.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `address`, `interface`, `prefixSource`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
