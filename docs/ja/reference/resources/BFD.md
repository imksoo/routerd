# BFD

[リソース別の設定](../resources.md#bfd) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

vtysh経由でFRR bfddのセッションを設定・観測し、BGPPeerの障害処理に使います。GoBGPとは別バックエンドで、BFD DownによりBGPピアをリセットします。

## 指定条件と参照関係

- 宣言したBFDは少なくとも1つのBGPPeer.bfdから参照される必要があります。BFDのpeerは参照元BGPPeer名またはその相手アドレスに一致する必要があり、BGPDynamicPeerにはbfd項目がありません。
- Linux・FreeBSDでFRR bfdd・vtyshが必要です。interfaceなしは参照BGPRouter.routerIDをローカルに使うmultihopで、直接IP指定かつinterfaceなしの場合はBGPRouterがちょうど1件必要です。interfaceありはsingle-hopです。
- BFDはそのセッションの障害を検出するもので、Upでも全宛先の疎通は保証しません。GoBGPは過去のBFD UpまたはBGP確立後のDown観測に反応し、起動時・不明状態をすべてDownとは扱いません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: BFD
  metadata:
    name: "<name>"
  spec:
    [ detectMultiplier: <integer> ]
    [ interface: <string> ]
    [ minRx: <string> ]
    [ minTx: <string> ]
    peer: <string>
    [ profile: ("" | "fast" | "normal" | "slow") ]
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
| `spec.peer` | **必須** | string | IPアドレスそのものかBGPPeer/name参照が必須です。BGPPeer参照は全相手アドレスへ展開し、routerRef先のrouterIDをローカルアドレスに使います。直接指定のホスト名は不可です。 |
| `spec.interface` | *任意* | string | 任意のsingle-hopインターフェースです。Interface/nameなら宣言したInterface.ifnameへ解決します。名前だけの場合は論理インターフェースの存在を検証しますが、実行時はその文字列をFRRへ渡すため、Interface/nameを推奨します。省略時はmultihopです。 |
| `spec.profile` | *任意* | string; enum=["", "fast", "normal", "slow"] | fast・省略はminRx=minTx=300ms・倍率3、normalは500ms・500ms・3、slowは1000ms・1000ms・5です。個別指定があれば優先します。 |
| `spec.minRx` | *任意* | string | 任意のGo durationで50ms～60000msです。プロファイルの受信間隔を上書きして整数msに変換し、省略時はプロファイル値です。BFDプロトコルタイマーであり、ルーター再起動の期限ではありません。 |
| `spec.minTx` | *任意* | string | 任意のGo durationで50ms～60000msです。プロファイルの送信間隔を上書きし、整数msで出力します。 |
| `spec.detectMultiplier` | *任意* | integer; minimum=1; maximum=50 | 任意の整数1～50です。省略・デコード値0ならプロファイル既定（通常3、slowは5）です。交渉された検出間隔の倍率で、routerdの再調整回数ではありません。 |
| `spec.when` | *任意* | object | 任意の共通有効化条件です。省略時は無条件です。state・all・any、参照検証、スキーマ制限は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/bgp_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/bgp_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/bfd/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bfd/controller.go) · [pkg/controller/bgp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/bgp/controller.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `peer`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/bgp-bfd.yaml)
