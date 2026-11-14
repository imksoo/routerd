# RouterdCluster

[リソース別の設定](../resources.md#routerdcluster) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

ファイルのリースとOSファイルロックで、どのrouterdが変更処理を行うかを調整します。VRRPの選出や対向ネットワークの自動探索ではありません。

## 指定条件と参照関係

- 参加者間で同じleasePathと信頼できるロック動作が必要です。peersは識別子で、接続先ではありません。リース実装は空でない一覧を調整の有効化に使い、対向への通信や参加資格の照合は行いません。待機側の変更controllerはdry-runで動作します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: RouterdCluster
  metadata:
    name: "<name>"
  spec:
    [ identity: <string> ]
    leasePath: <string>
    [ leaseTTL: <string> ]
    peers:
      - <string>
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
| `spec.peers` | **必須** | array | 空白・重複を除去した後に2件以上となる一覧が必須です。既定値はなく、Kind/名前の参照や、実際に接続するネットワークアドレスではありません。 |
| `spec.leaseTTL` | *任意* | string | リースの有効期間。省略時30sで、明示する場合は5s〜10mのGo durationです。排他的な変更はロックが制御し、期限だけでネットワーク障害を検知する仕組みではありません。 |
| `spec.leasePath` | **必須** | string | 改行を含まない絶対パスのリースファイル。必須で既定値はありません。ホストごとに別のファイルを用意してもホスト間の調整にはなりません。 |
| `spec.identity` | *任意* | string | ローカルのリース保持者の識別子。省略時は空白を除去したos.Hostname()です。Router.metadata.nameからは決めません。 |
| `spec.when` | *任意* | object | 有効化条件。省略時は条件なし。共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/ha/lease.go](https://github.com/imksoo/routerd/blob/main/pkg/ha/lease.go) · [cmd/routerd/cmd_apply.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_apply.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `leader`, `leaseExpiresAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/ha-2-node.yaml)
