# IPv4Route

[リソース別の設定](../resources.md#ipv4route) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

リソースの状態からデバイス・ゲートウェイを取得できる、IPv4の通常経路または破棄経路です。

## 指定条件と参照関係

- blackholeではdevice、deviceFrom、gateway、gatewayFrom、preferredSourceを併用できません。unicastは実行時にデバイスの解決が必要で、直接接続経路ならgatewayは省略できます。
- 動的な値をdevice・gatewayの固定値より優先し、未解決なら固定値を使います。有効な値が残らない場合、参照のoptionalではDeviceMissing・GatewayMissingを回避できません。参照先は設定内に存在し、fieldの明示と提供フィールドへの適合が必要です。
- Linuxはip route replaceを使い、preferredSourceはsrc、正のmetricのみmetric引数にします。FreeBSDはroute change/addと-ifaを使い、この経路ではmetricを適用しません。Installedは宛先までの疎通成功を意味しません。
- ready_whenとdevice・gateway内の${...}式は廃止され、検証で拒否されます。dependsOnとdeviceFrom・gatewayFromを使用してください。preferredSourceも状態式を扱いません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: IPv4Route
  metadata:
    name: "<name>"
  spec:
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    destination: "<string>" # 必須; string
    device: "<string>" # 任意; string
    deviceFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    gateway: "<string>" # 任意; string
    gatewayFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    metric: 0 # 任意; integer; minimum=0
    preferredSource: "<string>" # 任意; string
    type: "unicast" # 任意; string; enum=["unicast", "blackhole"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.destination` | 宛先のIPv4 CIDRが必須です。デフォルト経路はdefaultではなく0.0.0.0/0を指定します。現行の検証はここでIPv6を拒否しませんが、本リソース・コントローラーはIPv4用です。 |
| `spec.type` | 既定はunicastです。blackholeは通信を破棄し、デバイス・ゲートウェイ・送信元の指定を禁止します。 |
| `spec.device` | deviceFromで値が得られない場合の論理インターフェース名・OSデバイス名です。unicastで空ならPending/DeviceMissing、OS上に存在しなければDeviceNotReadyです。blackholeでは禁止です。 |
| `spec.deviceFrom` | デバイスを取得する状態値参照です。resource・fieldは共通の参照規則に従います。正規化後の先頭値をdeviceより優先します。既定は参照なしで、blackholeでは禁止です。 |
| `spec.gateway` | 任意のIPv4ネクストホップです。既定は空で、ゲートウェイを使わないデバイス経由の経路になります。gatewayFrom未解決時の固定値でもあります。blackholeでは禁止です。 |
| `spec.gatewayFrom` | 共通のresource・field規則に従う状態値参照です。正規化後の先頭値をgatewayより優先します。参照を設定しても固定値を含め値が得られなければPending/GatewayMissingになります。blackholeでは禁止です。 |
| `spec.preferredSource` | ローカル発信時の優先送信元IPv4アドレスです。既定は指定なしです。適用時にローカルアドレスとして存在しないとPending/PreferredSourceNotLocalになります。アドレス割当やSNATは行いません。blackholeでは禁止です。 |
| `spec.metric` | 0以上の整数で、既定は0です。Linuxは正の値だけをコマンドに渡し、FreeBSDの適用では使用しません。通常、同条件の経路では小さいメトリックが優先されます。 |
| `spec.dependsOn` | 共通のリソース依存条件のANDリストで、既定は空です。不成立ならDependsOnFalseとしてインストールを見送ります。resource・field・phase・equals・notEmpty・optionalは共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `type`, `destination`, `device`, `gateway`, `preferredSource`, `metric`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/lan-advertise-with-community.yaml)
