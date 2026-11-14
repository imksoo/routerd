# HybridRoute

[リソース別の設定](../resources.md#hybridroute) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

列挙したIPv4宛先網を、OverlayPeer経由の主経路表IPv4Routeへ展開します。

## 指定条件と参照関係

- デフォルトでないIPv4 CIDRのみです。default・0.0.0.0/0・IPv6・主表以外は拒否します。宛先をネットワーク化し、<資源名>-<CIDR内の英数字以外の連続をハイフン化>、例branch-10-20-0-0-16の名前を生成します。
- 生成名は一意で、明示IPv4Routeの名前・宛先との衝突はエラーです。同じHybridRoute内の正規化宛先重複も衝突します。HealthCheck異常はHybridRouteの状態表示だけを変え、適用待機・経路撤去・別相手選択は行いません。
- 生成IPv4Routeはpeerのunderlay.interfaceをdeviceに使います。ipsecだけremote.addressがIPv4ならgatewayにし、他はデバイス経路です。トンネル・VPN設定と到達性は別途用意します。Linuxは正のmetricを使い、FreeBSDのIPv4Route適用はmetricを使いません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: HybridRoute
  metadata:
    name: "<name>"
  spec:
    destinationCIDRs:
      - <string>
    [ healthCheckRef: <string> ]
    [ install:
        [ metric: <integer> ]
        [ table: ("" | "main") ]
    ]
    peerRef: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.destinationCIDRs` | **必須** | array | デフォルト経路を除く1件以上のIPv4 CIDRが必須です。相手ネットワークの補完・検出はしません。 |
| `spec.peerRef` | **必須** | string | 既存OverlayPeerのmetadata.nameが必須です。展開関数はOverlayPeer/nameを正規化できますが、通常参照検証は裸名を必要とします。 |
| `spec.install` | *任意* | object | 経路適用設定です。既定は主表・metric 0で、ポリシールーティング・HA自動選択ではありません。 |
| `spec.install.table` | *任意* | string; enum=["", "main"] | 空・mainだけを許し、どちらも主表IPv4Routeにします。他の表名・番号は拒否します。 |
| `spec.install.metric` | *任意* | integer; minimum=0 | 0以上の整数で既定0です。生成IPv4Routeへコピーし、Linuxは正値だけを出力しFreeBSDは使いません。 |
| `spec.healthCheckRef` | *任意* | string | 既存HealthCheckの任意裸名です。空なら監視状態追加なしです。Unhealthy・failed・errorはDegradedにしますが経路は変えず、この関数は未取得・unknownを異常扱いしません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/controller/chain/dynamic_effective.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dynamic_effective.go) · [pkg/controller/chain/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/hybrid.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `routes`, `peerRef`, `defaultRouteUntouched`, `estimatedMTU`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
