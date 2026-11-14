# NTPServer

[リソース別の設定](../resources.md#ntpserver) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: system.routerd.net/v1alpha1` · `metadata.name`

chronyまたはFreeBSD ntpdを使い、時刻サーバーと上流サーバーを設定します。

## 指定条件と参照関係

- managedの既定値はfalseです。trueでは上流の取得元宣言が必要で、staticならserversが追加で必須です。待受・許可元の配列は結合しますが、上流の各取得方法はsourceの選択順で切り替えます。
- ntpdの生成処理はallowCIDRsによる制限を出力しません。この一覧の宣言だけでntpdへのクライアント制限が適用されるとは扱わないでください。
- 動的なsourceでは、上流の値が未解決でも、それだけでサーバー設定の生成を止めません。上流なしの設定になる場合があります。Appliedは設定・サービスの反映を示し、時刻同期済みの証明ではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: system.routerd.net/v1alpha1
  kind: NTPServer
  metadata:
    name: "<name>"
  spec:
    allowCIDRFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    allowCIDRs: # 任意; array
      - "<string>" # string
    fallbackServers: # 任意; array
      - "<string>" # string
    listenAddressFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    listenAddresses: # 任意; array
      - "<string>" # string
    managed: true # 任意; boolean
    provider: "chrony" # 任意; string; enum=["chrony", "ntpd"]
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
| `spec.managed` | 省略時falseで、時刻サービスを設定・起動せずObservedを記録します。ファイルとサービスを管理する場合はtrueにします。 |
| `spec.source` | 省略時staticでserversを使います。auto・dhcp・dhcpv6はいずれも、解決したserverFrom、fallbackServers、serversの順で選びます。DHCPの取得元を自動探索する指定ではありません。 |
| `spec.servers` | 上流サーバーのホスト名またはIPアドレス。省略時は空ですが、managed=trueかつsource省略/staticでは必須です。各要素は空白を含まない単一の値で、リソース参照ではありません。 |
| `spec.serverFrom` | 明示する値の取得元一覧。省略時は空です。例はDHCPv4Client/wanのntpServers、DHCPv6PrefixDelegation/wan-pdのsntpServersです。auto/dhcp/dhcpv6で使い、各resourceとfieldの存在が必要です。 この取得処理はoptionalを使わず、値がなければ次の取得方法へ進みます。 |
| `spec.when` | 有効化条件。省略時は条件を設けません。state/all/any、unknown、経過時間の条件は共通の条件指定を参照してください。 |
| `spec.provider` | サーバー実装。通常はchrony、FreeBSDではntpdが既定値です。ntpdはFreeBSDでのみ対応します。 |
| `spec.fallbackServers` | 代替上流の一覧。省略時は空です。auto/dhcp/dhcpv6でserverFromに値がないとき、serversより先に選びます。独立した代替指示は生成しません。 |
| `spec.listenAddresses` | 固定の待受IPアドレス。省略時は空です。CIDRはホスト部分のIPへ変換します。待受設定が空ならbindaddress/interface listen指示を生成せず、実装サービスの既定動作を使います。 |
| `spec.listenAddressFrom` | 追加の待受アドレス取得元。省略時は空で、listenAddressesと結合します。各要素のresourceとfieldが必要です。待受設定を宣言したのに結合結果が空ならPending/NoListenAddressesです。 |
| `spec.allowCIDRs` | 利用を許可するクライアントのCIDR一覧。省略時は空です。chronyでは各allowを生成しますが、ntpdの生成設定にはこの一覧を使いません。固定値の入力検証は空白でないことまでで、CIDR構文は確認しません。 |
| `spec.allowCIDRFrom` | allowCIDRsと結合するCIDR取得元。省略時は空です。optionalでない取得元に値がなければPending/AllowCIDRFromPending、取得値が不正なCIDRならPending/AllowCIDRFromInvalidです。プレフィックス長のないIPアドレスはCIDRとして受理しません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_system.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_system.go) · [pkg/controller/chain/ntp.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ntp.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `servers`, `listenAddresses`, `allowCIDRs`, `source`, `updatedAt`
