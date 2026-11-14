# PortForward

[リソース別の設定](../resources.md#portforward) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

TCP・UDPポートを1つのIPv4先へ転送し、任意で内部折り返しNATを設定します。LinuxはDNATと転送許可、FreeBSDはrdr passを生成します。

## 指定条件と参照関係

- listen・targetが必須です。targetはaddress・addressFromのちょうど一方とportが必要で、固定値はホスト名・CIDRでなく裸のIPv4です。listenのアドレスを両方省略すると、そのインターフェース上の全宛先が対象です。
- 参照先存在・提供fieldを検証しますが、本NAT描画が読むのは宣言済みIPv4StaticAddress.address・VirtualAddress.addressだけです。optional=trueは対応参照の未解決時にルールを省略できるだけで、任意の動的参照にはなりません。PortForwardにbackendヘルスチェックはありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: firewall.routerd.net/v1alpha1
  kind: PortForward
  metadata:
    name: "<name>"
  spec:
    [ hairpin:
        [ enabled: <boolean> ]
        [ interfaces:
            - <string>
        ]
        [ mode: ("" | "auto" | "manual" | "off") ]
    ]
    listen:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      interface: <string>
      port: <integer>
      protocol: ("tcp" | "udp")
    target:
      [ address: <string> ]
      [ addressFrom:
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
      ]
      port: <integer>
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
| `spec.listen` | **必須** | object | 必須のIPv4公開側設定です。NAT・ファイアウォールを作り、アプリの待受・アドレス割当は行いません。 |
| `spec.listen.interface` | **必須** | string | 必須の裸の論理インターフェース名で、Kind/nameではありません。設定索引に存在し、OS名を解決する必要があります。 |
| `spec.listen.address` | *任意* | string | 任意の裸の宛先IPv4です。addressFromと排他で、省略すると待受インターフェース上の全宛先アドレスに一致します。 |
| `spec.listen.addressFrom` | *任意* | object | addressと排他の共通resource・field・optional参照です。NAT・静的描画は宣言済みIPv4StaticAddress.address・VirtualAddress.addressに対応し、任意の実行時statusは読みません。一般参照の検証成功だけでは描画対応を保証しません。 |
| `spec.listen.protocol` | **必須** | string; enum=["tcp", "udp"] | tcp・udpが必須で既定値はありません。転送通信の選択であり、ヘルスチェック方式とは別です。 |
| `spec.listen.port` | **必須** | integer; minimum=1; maximum=65535 | 必須の宛先ポート1～65535です。範囲・一覧・既定値はありません。 |
| `spec.hairpin` | *任意* | object | 公開アドレス経由で内部へ戻す任意のNAT設定です。有効時はlistenアドレスの解決が必要です。PortForwardの既定はoff、IngressServiceはautoです。 |
| `spec.hairpin.enabled` | *任意* | boolean | 既定falseです。mode省略時、trueはPortForwardでmanual、IngressServiceは引き続きautoです。mode=offはtrueでも無効にします。 |
| `spec.hairpin.mode` | *任意* | string; enum=["", "auto", "manual", "off"] | auto・manual・offです。autoはlisten・backendが宣言IPv4サブネットまたは同じプライベート/24にある場合にlisten側を選び、任意LAN経路を検出するものではありません。manualでルールを作るにはenabled=trueとinterfacesが必要です。 |
| `spec.hairpin.interfaces` | *任意* | array | 任意の裸の宣言論理名で重複は禁止です。有効なmanualには1件以上必要です。autoはlisten側も追加し、IPv4 DNATとmasquerade・PF SNATで戻りをルーター経由にします。 |
| `spec.when` | *任意* | object | 共通の有効化条件で、既定は無条件です。不成立は実効生成から除きます。判定・スキーマ制約は共通項を参照してください。 |
| `spec.target` | **必須** | object | 必須の単一内部転送先です。IP割当・サービス起動は行いません。 |
| `spec.target.address` | *任意* | string | 裸のIPv4転送先で、target.addressFromとちょうど一方が必須です。既定なしです。 |
| `spec.target.addressFrom` | *任意* | object | 共通resource・field・optional参照で、listen.addressFromと同じ宣言値の制約です。IPv4への解決が必要です。 |
| `spec.target.port` | **必須** | integer; minimum=1; maximum=65535 | listen.portとは独立した必須の内部TCP・UDPポート1～65535です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `listenAddress`, `target`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-port-forward-web.yaml)
