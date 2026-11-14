# DSLiteTunnel

[リソース別の設定](../resources.md#dslitetunnel) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

AFTRへ向けてIPv4をIPv6にカプセル化します。トンネル作成、IPv4デフォルト経路、NAT、疎通確認は別の設定・処理です。

## 指定条件と参照関係

- 無効時もinterfaceとaftrFrom・aftrFQDN・aftrIPv6・remoteAddressのいずれかが必須です。AFTR指定は併用でき、実行時はこの順で解決できるものを探します。
- localAddressは外側のIPv6送信元ですが、現行のlocalAddressFromは内側のIPv4送信元を選びます。両者は置き換え可能な指定ではありません。内側送信元の既定は192.0.0.2、対向は192.0.0.1です。IPv4の参照がなければ既定の送信元を使います。
- AFTR・送信元・依存値の未解決はPendingです。Upはトンネル・リンク設定を示し、AFTRやインターネットへの疎通を保証しません。有効化条件が偽、または無効なリソースは未使用のトンネルを削除しますが、依存条件の不成立だけでは処理を見送るため即時削除を保証しません。
- Linuxはip6tnlのipip6、FreeBSDはgifを使います。gif名でない希望名はgif100～gif999へ決定的に変換し、実名を状態で公開します。廃止したaftrSource・ready_whenは拒否されるため、aftrFrom・dependsOnを使います。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DSLiteTunnel
  metadata:
    name: "<name>"
  spec:
    [ aftrAddressOrdinal: <integer> ]
    [ aftrAddressSelection: ("ordinal" | "ordinalModulo") ]
    [ aftrDNSServers:
        - <string>
    ]
    [ aftrFQDN: <string> ]
    [ aftrFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ aftrIPv6: <string> ]
    [ defaultRoute: <boolean> ]
    [ dependsOn:
        -
          [ equals: <string> ]
          [ field: <string> ]
          [ notEmpty: <boolean> ]
          [ optional: <boolean> ]
          [ phase: <string> ]
          resource: <string>
    ]
    [ enabled: <boolean> ]
    [ encapsulationLimit: <string> ]
    interface: <string>
    [ localAddress: <string> ]
    [ localAddressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ localAddressSource: ("interface" | "static" | "delegatedAddress") ]
    [ localAddressSuffix: <string> ]
    [ localDelegatedAddress: <string> ]
    [ mtu: <integer> ]
    [ remoteAddress: <string> ]
    [ routeMetric: <integer> ]
    [ tunnelName: <string> ]
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
| `spec.interface` | **必須** | string | 宣言済みの論理アンダーレイインターフェースが必須で、通常の参照検証で存在を確認します。 |
| `spec.tunnelName` | *任意* | string | 任意のデバイス名で、既定はmetadata.nameです。FreeBSDは前述のgif名に変換する場合があります。 |
| `spec.enabled` | *任意* | boolean | 既定はtrueです。falseは未使用の設定トンネルを削除しDisabledにしますが、検証は免除しません。 |
| `spec.aftrFQDN` | **条件付き必須** | string | 任意のAFTRホスト名で、空白・スラッシュは禁止です。aftrFromの後、固定アドレスの前に試します。 |
| `spec.aftrIPv6` | **条件付き必須** | string | 任意のAFTR IPv6アドレスです。aftrFQDNの後に試し、既定値はありません。 |
| `spec.remoteAddress` | **条件付き必須** | string | 解決順序の最後に使う任意のAFTR IPv6アドレスです。内側IPv4の宛先ではありません。 |
| `spec.aftrFrom` | **条件付き必須** | object | 共通のresource・field参照で、通常はDHCPv6のAFTR名を使います。実在する参照先・提供フィールドが必要です。同じリソースのdnsServersも読み、aftrDNSServersに加えます。 |
| `spec.aftrDNSServers` | *任意* | array | 任意のIPv4・IPv6 DNSサーバーです。53番で問い合わせます。明示値・参照由来の両方がなければ実行時は既定で127.0.0.1:1053を使います。宣言してもリゾルバーは起動しないため、利用可能なサーバーを指定します。 |
| `spec.aftrAddressOrdinal` | *任意* | integer; minimum=1 | AAAAの1始まりの選択番号で、既定は1です。番号・選択モードを明示するとIPv6文字列の辞書順で選び、両方省略ならリゾルバーの順序を使います。 |
| `spec.aftrAddressSelection` | *任意* | string; enum=["ordinal", "ordinalModulo"] | 既定はordinalで、件数外の番号は失敗します。ordinalModuloは番号−1を件数で循環させ、AFTR間の通信量を分散する指定ではありません。 |
| `spec.localAddress` | **条件付き必須** | string | 任意の外側IPv6送信元です。localAddressSource:staticでは必須で、内側IPv4送信元ではありません。 |
| `spec.localAddressFrom` | *任意* | object | 内側IPv4の共通状態参照です。状態を先に読み、次に宣言済みIPv4StaticAddress.addressを読みます。必須値未解決はPending、optional:trueは192.0.0.2へ戻ります。有効なユニキャストIPv4が必要です。 |
| `spec.localAddressSource` | *任意* | string; enum=["interface", "static", "delegatedAddress"] | 既定のinterfaceはlocalAddressがなければ利用可能なグローバルIPv6を探します。staticはlocalAddress必須です。delegatedAddressはlocalDelegatedAddressから導出し、DAD完了まで/128を準備して待つ場合があります。 |
| `spec.localDelegatedAddress` | **条件付き必須** | string | 既存IPv6DelegatedAddressの裸名で、delegatedAddress時に必須です。他モードでも指定した参照は存在を確認します。 |
| `spec.localAddressSuffix` | *任意* | string | 任意のIPv6サフィックスです。委譲モードの優先順は本項目→参照先addressSuffix→::1です。interfaceモードでもこの値で送信元を導出できます。 |
| `spec.defaultRoute` | *任意* | boolean | 既定はfalseです。FreeBSDの実行処理は192.0.0.1経由のデフォルト経路を変更・追加できます。Linuxのトンネル処理はこの値でIPv4デフォルトを作らないため、IPv4Route・EgressRoutePolicyを使います。 |
| `spec.routeMetric` | *任意* | integer; minimum=0 | 任意の0以上の整数で、デコード時の既定は0です。現行DS-Liteの実行処理はデフォルト経路設定にこの値を渡しません。 |
| `spec.mtu` | *任意* | integer; minimum=1280; maximum=65535 | 任意の1280～65535で、実行時の既定は1460です。トンネルリンクに適用し、アンダーレイ経路がそのサイズを通すことは保証しません。 |
| `spec.encapsulationLimit` | *任意* | string | Linuxのencaplimit引数で、既定はnoneです。詳細値をここでは検証せず、不正値はipコマンドで失敗します。FreeBSDの実行処理では使いません。 |
| `spec.when` | *任意* | object | 任意の共通有効化条件で、既定は無条件です。偽なら未使用の設定トンネルを削除します。 |
| `spec.dependsOn` | *任意* | array | 共通依存条件のANDリストで、既定は空です。不成立ならPending/DependsOnFalseとしてトンネル更新を見送ります。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `device`, `tunnelName`, `localIPv6`, `innerLocalIPv4`, `innerRemoteIPv4`, `localInterface`, `aftrName`, `aftrIPv6`, `mtu`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-dslite-home.yaml)
