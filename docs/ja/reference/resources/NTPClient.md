# NTPClient

[リソース別の設定](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

上流との時刻同期クライアントを管理、または観測します。

## 指定条件と参照関係

- managed=trueではservers/serverFrom/fallbackServersのどれかが必要です。staticではさらにservers自体が必須です。サーバーが解決できなければクライアントはPending/NoServersとなります。chronyを管理する場合は標準のsystemd-timesyncdを無効化します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    [ fallbackServers:
        - <string>
    ]
    [ interface: <string> ]
    [ managed: <boolean> ]
    [ provider: ("systemd-timesyncd" | "chrony" | "ntpd") ]
    [ serverFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ servers:
        - <string>
    ]
    [ source: ("static" | "auto" | "dhcp" | "dhcpv6") ]
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
| `spec.provider` | *任意* | string; enum=["systemd-timesyncd", "chrony", "ntpd"] | クライアント実装。通常はsystemd-timesyncd、FreeBSDではntpdが既定値です。chronyも受理します。ntpdはFreeBSDのrc.d、systemd-timesyncdはOSの対応が必要です。 |
| `spec.managed` | *任意* | boolean | 省略時falseで、時刻サービスを設定・起動せずObservedを記録します。ファイルとサービスを管理する場合はtrueにします。 |
| `spec.source` | *任意* | string; enum=["static", "auto", "dhcp", "dhcpv6"] | 省略時staticでserversを使います。auto・dhcp・dhcpv6はいずれも、解決したserverFrom、fallbackServers、serversの順で選びます。DHCPの取得元を自動探索する指定ではありません。 |
| `spec.interface` | *任意* | string | 任意の論理インターフェース名。省略時は空で、指定時はRouter全体の検証で存在を確認します。現行NTP controllerは取得元の選択やサービスのbindには使いません。 |
| `spec.servers` | **条件付き必須** | array | 上流サーバーのホスト名またはIPアドレス。省略時は空ですが、managed=trueかつsource省略/staticでは必須です。各要素は空白を含まない単一の値で、リソース参照ではありません。 |
| `spec.serverFrom` | *任意* | array | 明示する値の取得元一覧。省略時は空です。例はDHCPv4Client/wanのntpServers、DHCPv6PrefixDelegation/wan-pdのsntpServersです。auto/dhcp/dhcpv6で使い、各resourceとfieldの存在が必要です。 この取得処理はoptionalを使わず、値がなければ次の取得方法へ進みます。 |
| `spec.fallbackServers` | *任意* | array | 代替の上流サーバー。省略時は空です。動的なsourceではserverFromに値がなければ選びます。systemd-timesyncdには追加でFallbackNTPとしても出力しますが、chronyとntpdには独立した代替指示を出力しません。 |
| `spec.when` | *任意* | object | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `servers`, `source`, `updatedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
