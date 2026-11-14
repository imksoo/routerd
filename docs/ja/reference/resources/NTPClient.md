# NTPClient

[リソース別の設定](../resources.md#ntpclient) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

上流との時刻同期クライアントを管理、または観測します。

## 指定条件と参照関係

- managed=trueではservers/serverFrom/fallbackServersのどれかが必要です。staticではさらにservers自体が必須です。サーバーが解決できなければクライアントはPending/NoServersとなります。chronyを管理する場合は標準のsystemd-timesyncdを無効化します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPClient
  metadata:
    name: "<name>"
  spec:
    fallbackServers: # 任意; array
      - "<string>" # string
    interface: "<string>" # 任意; string
    managed: true # 任意; boolean
    provider: "systemd-timesyncd" # 任意; string; enum=["systemd-timesyncd", "chrony", "ntpd"]
    serverFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    servers: # 任意; array
      - "<string>" # string
    source: "static" # 任意; string; enum=["static", "auto", "dhcp", "dhcpv6"]
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
| `spec.provider` | クライアント実装。通常はsystemd-timesyncd、FreeBSDではntpdが既定値です。chronyも受理します。ntpdはFreeBSDのrc.d、systemd-timesyncdはOSの対応が必要です。 |
| `spec.managed` | 省略時falseで、時刻サービスを設定・起動せずObservedを記録します。ファイルとサービスを管理する場合はtrueにします。 |
| `spec.source` | 省略時staticでserversを使います。auto・dhcp・dhcpv6はいずれも、解決したserverFrom、fallbackServers、serversの順で選びます。DHCPの取得元を自動探索する指定ではありません。 |
| `spec.interface` | 任意の論理インターフェース名。省略時は空で、指定時はRouter全体の検証で存在を確認します。現行NTP controllerは取得元の選択やサービスのbindには使いません。 |
| `spec.servers` | 上流サーバーのホスト名またはIPアドレス。省略時は空ですが、managed=trueかつsource省略/staticでは必須です。各要素は空白を含まない単一の値で、リソース参照ではありません。 |
| `spec.serverFrom` | 明示する値の取得元一覧。省略時は空です。例はDHCPv4Client/wanのntpServers、DHCPv6PrefixDelegation/wan-pdのsntpServersです。auto/dhcp/dhcpv6で使い、各resourceとfieldの存在が必要です。 この取得処理はoptionalを使わず、値がなければ次の取得方法へ進みます。 |
| `spec.fallbackServers` | 代替の上流サーバー。省略時は空です。動的なsourceではserverFromに値がなければ選びます。systemd-timesyncdには追加でFallbackNTPとしても出力しますが、chronyとntpdには独立した代替指示を出力しません。 |
| `spec.when` | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/router-lab.yaml)
