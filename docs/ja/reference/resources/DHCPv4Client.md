# DHCPv4Client

[リソース別の設定](../resources.md#dhcpv4client) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

管理下のrouterd-dhcpv4-client daemonを動かし、取得したリースのアドレス、必要に応じて既定経路とホストのDNS設定を反映します。

## 指定条件と参照関係

- interfaceは宣言済みRouterに存在する必要があります。識別情報はプロトコルのオプションとして送信し、リソース参照には使いません。リースの期限切れ・Releaseでは反映済みのアドレス・経路・ホストDNSを撤去します。useRoutes/useDNSはリース取得自体を止めません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Client
  metadata:
    name: "<name>"
  spec:
    classID: "<string>" # 任意; string
    clientID: "<string>" # 任意; string
    hostname: "<string>" # 任意; string
    interface: "<string>" # 必須; string
    requestedAddress: "<string>" # 任意; string
    routeMetric: 0 # 任意; integer; minimum=0
    useDNS: true # 任意; boolean
    useRoutes: true # 任意; boolean
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
| `spec.interface` | 必須の論理インターフェース名。Interface/は付けず、宣言済みRouterのインターフェース一覧に存在する必要があります。既定値はありません。 |
| `spec.hostname` | DHCPオプション12のホスト名。省略時は空で、オプションを送りません。OSのホスト名を自動設定する処理はなく、空白文字は拒否します。 |
| `spec.requestedAddress` | 任意の希望IPv4アドレス。省略時は未指定です。プレフィックス長のないIPv4が必要で、オプション50として要求します。確実な割当てや静的設定にはなりません。 |
| `spec.classID` | オプション60として送るvendor class識別子の文字列バイト。省略時は送信しません。16進文字列の復号は行いません。 |
| `spec.clientID` | オプション61のclient識別子。明示値は文字列のバイトとして送り、16進復号しません。省略時は先頭バイト1とインターフェースのハードウェアアドレスです。サーバーがclient IDでリースを識別する場合は値を安定させます。 |
| `spec.useRoutes` | 省略時trueで、リースにdefaultGatewayがあれば既定経路へ反映します。falseなら以前の管理対象の既定経路を撤去しますが、リース取得とアドレス反映は続きます。状態のgateway情報は抑止しません。 |
| `spec.useDNS` | 省略時true。/etc/resolv.confがsystemd-resolvedを指す場合はresolvectl、それ以外はresolv.confへの書込みで取得DNSをホストへ反映します。falseは以前の管理DNSを解除します。DNSResolverなどから参照する状態上のDNSアドレスは残ります。 |
| `spec.routeMetric` | 省略時0です。反映する既定経路のmetricと、OS別のリースアドレス調整に使います。スキーマは0以上の整数を要求し、経路表IDではありません。 |
| `spec.when` | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/dhcpv4client/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dhcpv4client/controller.go) · [pkg/dhcpv4client/message.go](https://github.com/imksoo/routerd/blob/main/pkg/dhcpv4client/message.go) · [cmd/routerd-dhcpv4-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-dhcpv4-client/main.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `device`, `currentAddress`, `gateway`, `defaultGateway`, `dnsServers`, `ntpServers`, `domain`, `leaseTime`, `renewAt`, `rebindAt`, `expiresAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/basic-dhcp.yaml)
