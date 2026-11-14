# ClusterNetworkRoute

[リソース別の設定](../resources.md#clusternetworkroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

KubernetesのPod・ServiceネットワークへのIPv4経路を、明示したネクストホップ経由で設定します。クラスターのCIDRやノードを自動検出する機能ではありません。

## 指定条件と参照関係

- podsとservicesを合わせて1件以上のCIDRと、1件以上のviaが必要です。CIDRはIPv4限定で、両リストをまたぐ重複・包含関係も拒否されます。
- CIDRとviaの組み合わせごとに、<name>-pods-NN-via-NNまたは<name>-services-NN-via-NNというIPv4StaticRouteを生成します。番号は1始まりです。同じapiVersion・kind・nameの明示リソースがある場合、その経路を優先して生成を省略します。
- weightはECMPの通信量配分ではなく経路メトリックです。省略・0は100、1～999は1000−weightに変換します。正の値は大きいほどメトリックが小さくなり、0と900は同じメトリックになります。
- 生成経路にもIPv4StaticRouteの実装制限が適用されます。FreeBSDは出力しますが、現行Linuxの処理は生成された静的経路をインストールしません。展開・検証の成功だけでは転送動作の証明になりません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: ClusterNetworkRoute
  metadata:
    name: "<name>"
  spec:
    pods: # 任意; object
      cidrs: # 任意; array
        - "<string>" # string
    services: # 任意; object
      cidrs: # 任意; array
        - "<string>" # string
    via: # 必須; array
      - # object
        interface: "<string>" # 必須; string
        nextHop: "<string>" # 必須; string
        weight: 0 # 任意; integer; minimum=0; maximum=999
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
| `spec.pods` | PodネットワークのCIDR群です。省略時の自動補完はありません。 |
| `spec.pods.cidrs` | PodネットワークのIPv4プレフィックスです。既定は空で、services.cidrsも空なら必須です。両グループ内外の重複・包含は不可です。 |
| `spec.services` | ServiceネットワークのCIDR群です。省略時の自動補完はありません。 |
| `spec.services.cidrs` | ServiceネットワークのIPv4プレフィックスです。既定は空で、pods.cidrsも空なら必須です。両グループを通して同じ重複・包含制限が適用されます。 |
| `spec.via` | 1件以上必要なネクストホップ一覧です。各項目をすべてのCIDRに適用します。interfaceとnextHopが同じ組み合わせはweightが異なっても重複エラーです。 |
| `spec.via[].interface` | 同じRouterで宣言した論理インターフェース名が必須です。設定のインターフェース索引で解決する必要があり、ステータス値参照ではありません。 |
| `spec.via[].nextHop` | CIDRではないIPv4のネクストホップアドレスが必須です。リソース参照ではなく、既定値・自動検出はありません。 |
| `spec.via[].weight` | 0～999の整数で、既定は0です。前述のメトリックに変換され、通信量の比例配分にはなりません。 |
| `spec.when` | 共通の状態条件です。省略時は無条件です。state/all/anyの意味とスキーマ上の制限は共通項を参照してください。条件で絞り込んだ後に経路を生成します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/api/cluster_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/api/cluster_routes.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/render/networkd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/networkd.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/k8s-cluster-routes.yaml)
