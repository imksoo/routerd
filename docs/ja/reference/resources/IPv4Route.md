# IPv4Route

[リソース別の設定](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

リソースの状態からデバイス・ゲートウェイを取得できる、IPv4の通常経路または破棄経路です。

## 指定条件と参照関係

- blackholeではdevice、deviceFrom、gateway、gatewayFrom、preferredSourceを併用できません。unicastは実行時にデバイスの解決が必要で、直接接続経路ならgatewayは省略できます。
- 動的な値をdevice・gatewayの固定値より優先し、未解決なら固定値を使います。有効な値が残らない場合、参照のoptionalではDeviceMissing・GatewayMissingを回避できません。参照先は設定内に存在し、fieldの明示と提供フィールドへの適合が必要です。
- Linuxはip route replaceを使い、preferredSourceはsrc、正のmetricのみmetric引数にします。FreeBSDはroute change/addと-ifaを使い、この経路ではmetricを適用しません。Installedは宛先までの疎通成功を意味しません。
- ready_whenとdevice・gateway内の${...}式は廃止され、検証で拒否されます。dependsOnとdeviceFrom・gatewayFromを使用してください。preferredSourceも状態式を扱いません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    destination: <string>
    [ device: <string> ]
    [ deviceFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ gateway: <string> ]
    [ gatewayFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ metric: <integer> ]
    [ preferredSource: <string> ]
    [ type: ("unicast" | "blackhole") ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.destination` | **必須** | string | 宛先のIPv4 CIDRが必須です。デフォルト経路はdefaultではなく0.0.0.0/0を指定します。現行の検証はここでIPv6を拒否しませんが、本リソース・コントローラーはIPv4用です。 |
| `spec.type` | *任意* | string; enum=["unicast", "blackhole"] | 既定はunicastです。blackholeは通信を破棄し、デバイス・ゲートウェイ・送信元の指定を禁止します。 |
| `spec.device` | *任意* | string | deviceFromで値が得られない場合の論理インターフェース名・OSデバイス名です。unicastで空ならPending/DeviceMissing、OS上に存在しなければDeviceNotReadyです。blackholeでは禁止です。 |
| `spec.deviceFrom` | *任意* | object | デバイスを取得する状態値参照です。resource・fieldは共通の参照規則に従います。正規化後の先頭値をdeviceより優先します。既定は参照なしで、blackholeでは禁止です。 |
| `spec.gateway` | *任意* | string | 任意のIPv4ネクストホップです。既定は空で、ゲートウェイを使わないデバイス経由の経路になります。gatewayFrom未解決時の固定値でもあります。blackholeでは禁止です。 |
| `spec.gatewayFrom` | *任意* | object | 共通のresource・field規則に従う状態値参照です。正規化後の先頭値をgatewayより優先します。参照を設定しても固定値を含め値が得られなければPending/GatewayMissingになります。blackholeでは禁止です。 |
| `spec.preferredSource` | *任意* | string | ローカル発信時の優先送信元IPv4アドレスです。既定は指定なしです。適用時にローカルアドレスとして存在しないとPending/PreferredSourceNotLocalになります。アドレス割当やSNATは行いません。blackholeでは禁止です。 |
| `spec.metric` | *任意* | integer; minimum=0 | 0以上の整数で、既定は0です。Linuxは正の値だけをコマンドに渡し、FreeBSDの適用では使用しません。通常、同条件の経路では小さいメトリックが優先されます。 |
| `spec.dependsOn` | *任意* | array | 共通のリソース依存条件のANDリストで、既定は空です。不成立ならDependsOnFalseとしてインストールを見送ります。resource・field・phase・equals・notEmpty・optionalは共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `type`, `destination`, `device`, `gateway`, `preferredSource`, `metric`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
