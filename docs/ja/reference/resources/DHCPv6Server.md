# DHCPv6Server

[リソース別の設定](../resources.md#dhcpv6server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

共有dnsmasqでLAN向けDHCPv6アドレス・情報サービスと関連するRAを提供します。

## 指定条件と参照関係

- 現行実行時と旧静的生成は異なります。interfaceが必要でdelegatedAddressだけでは実行時範囲を作らず、dnsSource・selfAddressPolicyは自己DNSを補わず、ra-onlyもstateless分岐です。下記の明示値を使い、実際のDHCP・RA通信を確認します。
- 通常検証はdelegatedAddress指定時にmodeをstateless・ra-onlyへ制限します。delegatedAddressなしではra-onlyを含む非statelessに有効なIPv6開始・終了が必要です。domainSearchとdomainSearchFromは排他で、DNS・SNTPの固定値と参照は併用できます。
- 単一dnsmasqで各資源を処理し、constructorは実インターフェースの/64を使います。Appliedは再調整成功で、クライアント取得・RA受信・デフォルト経路成立を意味しません。廃止ready_whenは拒否します。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv6Server
  metadata:
    name: "<name>"
  spec:
    addressPool: # 任意; object
      end: "<string>" # 任意; string
      leaseTime: "<string>" # 任意; string
      start: "<string>" # 任意; string
    configPath: "<string>" # 任意; string
    defaultRoute: true # 任意; boolean
    delegatedAddress: "<string>" # 任意; string
    dependsOn: # 任意; array
      - # object
        equals: "<string>" # 任意; string
        field: "<string>" # 任意; string
        notEmpty: true # 任意; boolean
        optional: true # 任意; boolean
        phase: "<string>" # 任意; string
        resource: "<string>" # 必須; string
    dnsServerFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    dnsServers: # 任意; array
      - "<string>" # string
    dnsSource: "self" # 任意; string; enum=["self", "static", "none"]
    domainSearch: # 任意; array
      - "<string>" # string
    domainSearchFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    interface: "<string>" # 任意; string
    leaseFile: "<string>" # 任意; string
    leaseTime: "<string>" # 任意; string
    listenInterfaces: # 任意; array
      - "<string>" # string
    logDHCP: true # 任意; boolean
    managed: true # 任意; boolean
    mode: "stateless" # 任意; string; enum=["stateless", "stateful", "both", "ra-only"]
    pidFile: "<string>" # 任意; string
    rapidCommit: true # 任意; boolean
    role: "server" # 任意; string; enum=["server", "transit"]
    selfAddressPolicy: "<string>" # 任意; string
    server: "dnsmasq" # 任意; string; enum=["dnsmasq"]
    sntpServerFrom: # 任意; array
      - # object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
    sntpServers: # 任意; array
      - "<string>" # string
    stickyHoldDays: 0 # 任意; integer; minimum=0
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
| `spec.server` | 任意のdnsmasqで、既定もdnsmasqです。他バックエンドは許さず、DHCPv4・RAとサービスを共有します。 |
| `spec.managed` | 真偽値で既定falseです。現行実行処理はこの値で変更を抑止しません。 |
| `spec.role` | server・transitで、検証・静的生成の既定serverです。現行実行時はtransitによるサービス抑止を行いません。 |
| `spec.listenInterfaces` | 宣言済み論理インターフェース一覧で全参照の存在を検証します。delegatedAddressがあり一覧が非空なら、その参照先interfaceを含む必要があります。系統・ファイアウォール計画に使いますが、現行実行時プールにはinterface自体が必要です。 |
| `spec.logDHCP` | 既定falseです。静的生成ではlog-dhcpにしますが、現行実行時は参照しません。 |
| `spec.stickyHoldDays` | 0以上の日数で既定0は無効です。正のdnsmasq値は割当をdhcp-sticky.dbへ保持して再利用し、実行時はIPv6サーバー全体の最大値を全IPv6イベントに使います。インターフェース別ではなく、DHCPv6ServerLeaseSyncはこのDBを複製しません。 |
| `spec.interface` | 宣言済み論理LANインターフェースです。検証がdelegatedAddressのみを許しても現行実行時には必要で、省略すると実行時プール生成を飛ばします。 |
| `spec.delegatedAddress` | 任意の既存IPv6DelegatedAddress裸名です。静的生成はinterface・prefix・自己DNSを導出しますが、現行実行時はinterface・constructorを直接使い、この参照で補完しません。指定時はmodeをstateless・ra-onlyに制限します。 |
| `spec.mode` | 既定statelessです。statefulはconstructor範囲、bothはslaac付きです。現行実行時はra-onlyもra-statelessの既定分岐に入るため、DHCPv6情報サービスの無効化になりません。delegatedAddressなしではra-onlyでも検証上プール必須です。 |
| `spec.addressPool` | IPv6の状態保持型割当範囲です。delegatedAddressなしで非statelessなら必須で、statelessの割当範囲には使いません。開始・終了の自動補完はありません。 |
| `spec.addressPool.start` | 境界を含むIPv6開始値でend以下です。実行時のstateful範囲はconstructor:<OS名>・長さ64を使います。 |
| `spec.addressPool.end` | 境界を含むIPv6終了値でstart以上です。startと対で指定します。 |
| `spec.addressPool.leaseTime` | バックエンド期間でleaseTimeより優先します。空ならleaseTime・12hの順で補完し、ここではGo durationとして解析しません。 |
| `spec.defaultRoute` | 真偽値で既定falseです。静的生成はルーター期間制御に使えますが、現行実行時は参照しません。DHCPv6自体がIPv6デフォルト経路を配るわけではありません。 |
| `spec.dnsSource` | self・static・noneを検証し、静的生成の既定selfです。staticはdnsServers必須ですが、現行実行時はこの選択を無視して明示dnsServers・dnsServerFromだけを使い、noneでも明示オプションを抑止しません。 |
| `spec.selfAddressPolicy` | 任意の既存SelfAddressPolicy裸名です。現行実行時のLAN生成は使わなくても存在検証し、旧静的自己DNS選択で使います。 |
| `spec.dnsServers` | 任意のIPv6 DNS一覧で既定空です。dnsServerFromと結合してタグ付きoption6:dns-serverにし、DNS待受は起動しません。 |
| `spec.dnsServerFrom` | 共通resource・field参照一覧でoptional既定falseです。必須未解決は固定DNSがあってもPendingにし、この読み取りは解決した全状態値のIP系統を完全検証せず渡します。 |
| `spec.sntpServers` | 任意のIPv6 SNTP一覧で既定空です。sntpServerFromと結合してタグ付きoption6:sntp-serverを出力し、NTPデーモンは起動しません。 |
| `spec.sntpServerFrom` | 共通状態参照一覧です。必須・optionalの待機はdnsServerFromと同じで、既定参照なしです。 |
| `spec.domainSearch` | 任意の単一ドメイン一覧で既定空です。空白・スラッシュ・カンマ不可でdomainSearchFromと排他です。タグ付きoption6:domain-searchにします。 |
| `spec.domainSearchFrom` | DNSZone/<name>・field: zone限定の参照で、存在検証します。状態前でも宣言zoneを読み、必須未解決はサーバー待機、optional trueは省略です。domainSearchと排他です。 |
| `spec.leaseTime` | addressPool.leaseTimeに次ぐ期間補完で、既定12hです。 |
| `spec.rapidCommit` | 真偽値で既定falseです。現行生成はタグ付きoption6:rapid-commitを出力し、別のdnsmasq dhcp-rapid-commit指令は出しません。この値だけで2メッセージRapid Commitの成立を保証しません。 |
| `spec.configPath` | 任意文字列で既定空です。現行コントローラーはリソース別パスではなく共有dnsmasqオプションを使います。 |
| `spec.pidFile` | 任意文字列で既定空です。現行コントローラーはリソース別パスではなく共有dnsmasqオプションを使います。 |
| `spec.leaseFile` | リースファイルの任意パスで、既定<StateDir>/dnsmasq/dnsmasq.leasesです。単一サービス共有のため、全DHCPv4/6の明示leaseFileが不一致なら実行時生成で失敗します。 |
| `spec.dependsOn` | 共通依存一覧として読み参照検証し、既定空です。現行実行時のDHCPv6サーバー生成・状態処理はこの一覧を評価せず、サービスの抑止条件としては使えません。動的値の必須条件・whenは別途評価します。 |
| `spec.when` | 共通条件で既定無条件です。不成立なら有効設定から除外してPending/WhenFalseです。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
