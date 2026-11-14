# NAT44FlowDNATPinhole

[リソース別の設定](../resources.md#nat44flowdnatpinhole) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

送信UDPからIPv4内部エンドポイントを学習し、受信UDPの宛先ポートで動的DNATします。Linux nftables専用で、現行PFは実装していません。

## 指定条件と参照関係

- fromInterfaceRefs・outbound・inboundと送信元選別が必須です。インターフェース・IPAddressSetの参照先は同じRouterに存在する必要があります。IPv4・UDP専用のポート起動DNATで、通常のconntrack応答処理とは別です。
- キーは送信のローカルUDP送信元ポート、値はSNAT前の内部IPv4と同じポートです。選択インターフェースはリソースごとの1マップを共有します。同じポートを使う別クライアントは上書きし、相手アドレスをキーに含めません。
- 受信DNATは選択した入力インターフェース・相手送信元ポート・学習済み宛先ポートを確認します。送信宛先CIDR・集合を受信相手に再適用せず、転送許可ルールも生成しません。変換後通信はファイアウォール側の許可も必要です。
- 学習マップはカーネル実行状態で、設定・永続DBではありません。NATテーブル再作成・再起動で失われ、送信通信による再学習が必要です。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44FlowDNATPinhole
  metadata:
    name: "<name>"
  spec:
    [ correlation:
        [ key: ("" | "localPort") ]
    ]
    fromInterfaceRefs:
      - <string>
    inbound:
      sourcePorts:
        - <string>
    outbound:
      [ destinationCIDRs:
          - <string>
      ]
      destinationPorts:
        - <string>
      [ destinationSetRefs:
          - <string>
      ]
      protocol: ("udp")
      [ sourceCIDRs:
          - <string>
      ]
      [ sourceSetRef: <string> ]
    [ timeout: <string> ]
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
| `spec.outbound` | **必須** | object | 必須の送信学習条件です。該当UDP通信の識別を記録しますが、送信通信自体のファイアウォール許可ではありません。 |
| `spec.outbound.sourceSetRef` | **条件付き必須** | string | 既存IPAddressSet/<name>の送信元選別です。sourceCIDRsと少なくとも一方が必須で、両方あればORです。既定は空です。 |
| `spec.outbound.sourceCIDRs` | **条件付き必須** | array | 任意のIPv4送信元CIDRのOR一覧で、既定は空です。送信元選別が最低1つ必要です。IPv6は拒否し、描画時はホスト部をマスクします。 |
| `spec.outbound.destinationCIDRs` | *任意* | array | 学習対象の任意のIPv4相手宛先で、既定は空です。destinationSetRefsとORで組み合わせ、両方なければ相手アドレス不問です。 |
| `spec.outbound.destinationSetRefs` | *任意* | array | 任意の既存IPAddressSet/<name>相手宛先参照です。重複は禁止、既定は空です。送信元と宛先の条件群はANDで組み合わせます。 |
| `spec.outbound.protocol` | **必須** | string; enum=["udp"] | udpが必須で既定値はありません。この学習機構はTCP・IPv6に対応しません。 |
| `spec.outbound.destinationPorts` | **必須** | array | 1件以上必須の送信UDP宛先ポートです。1～65535の数値か、唯一のstart-end範囲を指定します。範囲と他項目の併用は禁止です。 |
| `spec.inbound` | **必須** | object | 必須の戻り通信条件です。sourcePortsは相手UDP送信元ポートを選び、ローカル宛先ポートではありません。 |
| `spec.inbound.sourcePorts` | **必須** | array | 1件以上必須の相手UDP送信元ポート一覧です。outbound.destinationPortsと同じ1～65535・範囲単独の制約です。 |
| `spec.timeout` | *任意* | string | 任意の正のGo形式期間で、既定3mです。該当送信パケットごとに学習要素を更新します。描画は秒単位へ丸め最低1秒にし、UDPプロトコルのタイムアウトでなく学習状態の期限です。 |
| `spec.when` | *任意* | object | 共通の有効化条件で、既定は無条件です。除外したリソースの学習マップ・集合ルールは生成しません。入れ子条件とスキーマ制約は共通項を参照してください。 |
| `spec.fromInterfaceRefs` | **必須** | array | 1件以上必須の入出力インターフェース一覧です。裸名・Interface/name・PPPoESession/name・DSLiteTunnel/nameを受け付け、同じ一覧を送信oifname・受信iifnameで使います。 |
| `spec.correlation` | *任意* | object | 任意の相関定義でlocalPortのみ対応します。省略時も同じキーです。 |
| `spec.correlation.key` | *任意* | string; enum=["", "localPort"] | 空またはlocalPortのみです。描画側に別分岐はなく、常にUDP送信元ポートで学習マップを引きます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/local_service_redirect.go](https://github.com/imksoo/routerd/blob/main/pkg/render/local_service_redirect.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`
