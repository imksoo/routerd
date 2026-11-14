# ClusterNetworkRoute

[リソース別の設定](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

KubernetesのPod・ServiceネットワークへのIPv4経路を、明示したネクストホップ経由で設定します。クラスターのCIDRやノードを自動検出する機能ではありません。

## 指定条件と参照関係

- podsとservicesを合わせて1件以上のCIDRと、1件以上のviaが必要です。CIDRはIPv4限定で、両リストをまたぐ重複・包含関係も拒否されます。
- CIDRとviaの組み合わせごとに、<name>-pods-NN-via-NNまたは<name>-services-NN-via-NNというIPv4StaticRouteを生成します。番号は1始まりです。同じapiVersion・kind・nameの明示リソースがある場合、その経路を優先して生成を省略します。
- weightはECMPの通信量配分ではなく経路メトリックです。省略・0は100、1～999は1000−weightに変換します。正の値は大きいほどメトリックが小さくなり、0と900は同じメトリックになります。
- 生成経路にもIPv4StaticRouteの実装制限が適用されます。FreeBSDは出力しますが、現行Linuxの処理は生成された静的経路をインストールしません。展開・検証の成功だけでは転送動作の証明になりません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    [ pods:
        [ cidrs:
            - <string>
        ]
    ]
    [ services:
        [ cidrs:
            - <string>
        ]
    ]
    via:
      -
        interface: <string>
        nextHop: <string>
        [ weight: <integer> ]
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
| `spec.pods` | *任意* | object | PodネットワークのCIDR群です。省略時の自動補完はありません。 |
| `spec.pods.cidrs` | *任意* | array | PodネットワークのIPv4プレフィックスです。既定は空で、services.cidrsも空なら必須です。両グループ内外の重複・包含は不可です。 |
| `spec.services` | *任意* | object | ServiceネットワークのCIDR群です。省略時の自動補完はありません。 |
| `spec.services.cidrs` | *任意* | array | ServiceネットワークのIPv4プレフィックスです。既定は空で、pods.cidrsも空なら必須です。両グループを通して同じ重複・包含制限が適用されます。 |
| `spec.via` | **必須** | array | 1件以上必要なネクストホップ一覧です。各項目をすべてのCIDRに適用します。interfaceとnextHopが同じ組み合わせはweightが異なっても重複エラーです。 |
| `spec.via[].interface` | **必須** | string | 同じRouterで宣言した論理インターフェース名が必須です。設定のインターフェース索引で解決する必要があり、ステータス値参照ではありません。 |
| `spec.via[].nextHop` | **必須** | string | CIDRではないIPv4のネクストホップアドレスが必須です。リソース参照ではなく、既定値・自動検出はありません。 |
| `spec.via[].weight` | *任意* | integer; minimum=0; maximum=999 | 0～999の整数で、既定は0です。前述のメトリックに変換され、通信量の比例配分にはなりません。 |
| `spec.when` | *任意* | object | 共通の状態条件です。省略時は無条件です。state/all/anyの意味とスキーマ上の制限は共通項を参照してください。条件で絞り込んだ後に経路を生成します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `pods`, `services`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
