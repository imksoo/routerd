# DHCPv4Server

[リソース別の設定](../resources.md#dhcpv4server) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

共有dnsmasqサービスでIPv4 DHCPプール、選別条件付きの追加プール、予約を提供します。

## 指定条件と参照関係

- 現行の実行処理ではinterface・addressPoolとgateway/gatewayFrom、dnsServers/dnsServerFrom、ntpServers/ntpServerFromを使います。静的生成と実行時生成の挙動は異なり、旧routerSource・dnsSource、内蔵dns、authoritativeを実行時制御として扱えません。
- 有効プールには既存論理インターフェースと有効なIPv4開始・終了が必要です。start<=endを検証しますが、サブネット内所属・プール間重複・配布ゲートウェイの到達性は検証で保証しません。
- 共通の値参照は既存リソースと提供フィールドの明示が必要です。gateway・DNS・NTP・domainの必須参照がoptional falseで未解決なら、固定値があってもプールを見送ります。optional trueなら未解決値を省略します。動的アドレス読み取りは全解決値のIPv4系統を強制しません。
- スコープはMAC/OUIの選択条件と、ローカルprofileRefまたはインライン設定の片方を使います。プロファイル・スコープは有効な開始・終了が必要です。条件一致の動的リースが全一致プールの外なら適用時に除去してdnsmasqを再起動し、明示予約は残します。クライアントへの即時更新強制ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Server
  metadata:
    name: "<name>"
  spec:
    [ addressPool:
        [ end: <string> ]
        [ leaseTime: <string> ]
        [ start: <string> ]
    ]
    [ authoritative: <boolean> ]
    [ dns:
        [ cacheSize: <integer> ]
        [ enabled: <boolean> ]
        [ upstreamInterface: <string> ]
        [ upstreamServers:
            - <string>
        ]
        [ upstreamSource: ("dhcpv4" | "static" | "system" | "none") ]
    ]
    [ dnsInterface: <string> ]
    [ dnsServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ dnsServers:
        - <string>
    ]
    [ dnsSource: ("dhcpv4" | "static" | "self" | "none") ]
    [ domain: <string> ]
    [ domainFrom:
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
    [ interface: <string> ]
    [ leaseFile: <string> ]
    [ leaseTime: <string> ]
    [ listenInterfaces:
        - <string>
    ]
    [ logDHCP: <boolean> ]
    [ managed: <boolean> ]
    [ ntpServerFrom:
        -
          field: <string>
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ ntpServers:
        - <string>
    ]
    [ options:
        -
          [ code: <integer> ]
          [ name: <string> ]
          [ value: <string> ]
    ]
    [ profiles:
        -
          addressPool:
            [ end: <string> ]
            [ leaseTime: <string> ]
            [ start: <string> ]
          [ dnsServers:
              - <string>
          ]
          [ gateway: <string> ]
          name: <string>
          [ netmask: <string> ]
          [ ntpServers:
              - <string>
          ]
          [ options:
              -
                [ code: <integer> ]
                [ name: <string> ]
                [ value: <string> ]
          ]
    ]
    [ rangeEnd: <string> ]
    [ rangeStart: <string> ]
    [ role: ("server" | "transit") ]
    [ router: <string> ]
    [ routerSource: ("interfaceAddress" | "static" | "none") ]
    [ scopes:
        -
          addressPool:
            [ end: <string> ]
            [ leaseTime: <string> ]
            [ start: <string> ]
          [ dnsServers:
              - <string>
          ]
          [ gateway: <string> ]
          match:
            [ macAddresses:
                - <string>
            ]
            [ ouiPrefixes:
                - <string>
            ]
          name: <string>
          [ netmask: <string> ]
          [ ntpServers:
              - <string>
          ]
          [ options:
              -
                [ code: <integer> ]
                [ name: <string> ]
                [ value: <string> ]
          ]
          [ profileRef: <string> ]
    ]
    [ server: ("dnsmasq" | "kea" | "dhcpd") ]
    [ stickyHoldDays: <integer> ]
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
| `spec.server` | *任意* | string; enum=["dnsmasq", "kea", "dhcpd"] | スキーマはdnsmasq・kea・dhcpdを許し、計画・依存選択の既定はdnsmasqです。現行の実行コントローラーはこの値によらずdnsmasqを使い、kea・dhcpdへの切替にはなりません。固定保持日数の選択はdnsmasq指定だけを対象にします。 |
| `spec.managed` | *任意* | boolean | 真偽値で既定falseです。現行の実行処理は参照せず、falseを観測専用・無効化として使えません。 |
| `spec.role` | *任意* | string; enum=["server", "transit"] | server・transitを検証し、既定serverです。静的レンダラーはtransitを区別しますが、現行の実行時生成はこの切替を反映しません。割当抑止にtransitを使わないでください。 |
| `spec.listenInterfaces` | *任意* | array | 任意の宣言済み論理インターフェース一覧で、全参照の存在が必要です。検証・ファイアウォール計画に使いますが、現行の実行時プール生成はinterfaceだけを使い、この一覧で代替できません。 |
| `spec.logDHCP` | *任意* | boolean | 既定falseです。静的生成はlog-dhcpを出力できますが、現行の実行時設定はこのフラグを使いません。リースイベントのコールバック生成とは別です。 |
| `spec.stickyHoldDays` | *任意* | integer; minimum=0 | 0以上の保持日数で、既定0は無効です。正なら解放・期限切れ割当をdhcp-sticky.dbに保持して再利用します。実行時はdnsmasqのDHCPv4Server全体の最大値を全IPv4イベントに使い、インターフェース別には選びません。明示予約が優先です。 |
| `spec.interface` | **条件付き必須** | string | 主プールの宣言済み論理インターフェースです。スキーマ・検証は省略を許しますが、現行の実行時プール生成には必要です。Routerの索引に存在する必要があります。 |
| `spec.addressPool` | **条件付き必須** | object | 主IPv4割当プールです。interface指定時はstart・endが必須です。各値は旧rangeStart・rangeEnd・leaseTimeより優先します。 |
| `spec.addressPool.start` | **条件付き必須** | string | 裸IPv4の開始アドレスで、境界を含みます。rangeStartで補わない限り必須で、end以下が必要です。サブネット・範囲の自動推定はありません。 |
| `spec.addressPool.end` | **条件付き必須** | string | 裸IPv4の終了アドレスで、境界を含みます。rangeEndで補わない限り必須で、start以上が必要です。 |
| `spec.addressPool.leaseTime` | *任意* | string | バックエンドのリース期間文字列です。addressPool.leaseTime→leaseTime→12hの順に選びます。本リソースの検証はGo durationとして解析しません。 |
| `spec.rangeStart` | *任意* | string | addressPool.startの旧補完値で、既定空です。静的レンダラーを旧経路に切り替える指定でもあるためaddressPoolを推奨します。 |
| `spec.rangeEnd` | *任意* | string | addressPool.endの旧補完値です。静的レンダラーの切替はrangeStartと同じです。 |
| `spec.leaseTime` | *任意* | string | addressPool.leaseTimeが空の場合のリース期間で、既定12hです。 |
| `spec.routerSource` | *任意* | string; enum=["interfaceAddress", "static", "none"] | interfaceAddress・static・noneを検証し、staticはrouter必須です。静的生成の既定はinterfaceAddressでIPv4StaticAddressから取得しますが、現行実行時はrouterSource・routerを使わずgateway・gatewayFromを使用します。 |
| `spec.router` | **条件付き必須** | string | routerSource: staticで必須のIPv4値です。旧静的生成だけで使い、現行実行時のゲートウェイオプションには使いません。 |
| `spec.gateway` | *任意* | string | 任意の裸IPv4によるDHCPルーターオプションです。既定は明示オプションなしで、有効なgatewayFromが優先します。gatewayFromと排他ではありません。 |
| `spec.gatewayFrom` | *任意* | object | resource・fieldを指定する状態値参照です。optional既定falseで、未解決ならgateway指定があってもサーバーをPendingにします。optional trueなら未解決時にgatewayを使えます。 |
| `spec.dnsSource` | *任意* | string; enum=["dhcpv4", "static", "self", "none"] | dhcpv4・static・self・noneを検証し、staticはdnsServers、dhcpv4はdnsInterface必須です。静的生成の既定selfですが、現行実行時はこの選択を無視してdnsServers・dnsServerFromを使用します。 |
| `spec.dnsInterface` | **条件付き必須** | string | dnsSource: dhcpv4で必須の宣言済み論理インターフェースです。他の選択でも指定時は存在検証し、現行実行時はこの指定によるDNS取得を行いません。 |
| `spec.dnsServers` | *任意* | array | 裸IPv4のDNSサーバー一覧です。既定空で、dnsServerFrom解決値と結合してDHCPオプション6にします。DNSResolverやDNS待受は開始しません。 |
| `spec.dnsServerFrom` | *任意* | array | resource・fieldによる状態値参照一覧で、optional既定falseです。必須の未解決値は固定DNSがあってもサーバー全体を待機にします。実行時は状態から読みCIDRをアドレス化するため、IPv4を提供するフィールドを選びます。 |
| `spec.ntpServers` | *任意* | array | 任意の裸IPv4 NTPサーバー一覧で、既定空です。ntpServerFromと結合してオプション42にし、NTPServerは起動しません。 |
| `spec.ntpServerFrom` | *任意* | array | 共通のresource・field参照一覧です。必須・optionalの待機条件と状態のみの読み取りはdnsServerFromと同じで、既定参照なしです。 |
| `spec.domain` | *任意* | string | 空白・スラッシュ・カンマを含まない単一ドメインで、domainFromと排他です。domain-nameと、オプション119・domain-searchを別指定しない場合のdomain-searchを生成します。 |
| `spec.domainFrom` | *任意* | object | DNSZone/<name>・field: zoneに限定する参照です。参照先の存在が必要で、状態確定前でも宣言zoneを読めます。optional falseなら未解決で待機し、trueなら省略できます。domainと排他です。 |
| `spec.authoritative` | *任意* | boolean | 既定falseです。旧静的生成の切替とdhcp-authoritativeに使いますが、現行実行時はこの指定を出力しません。 |
| `spec.leaseFile` | *任意* | string | 任意パスで、既定<StateDir>/dnsmasq/dnsmasq.leases、通常/var/lib/routerd/dnsmasq/dnsmasq.leasesです。共有dnsmasqなので全DHCPv4/6Serverの明示leaseFileが一致しないと実行時エラーです。 |
| `spec.when` | *任意* | object | 共通の状態条件で既定無条件です。不成立ならプールを除外してPending/WhenFalseとなり、別途必須動的値の未解決でもPendingになります。 |
| `spec.dns` | *任意* | object | 旧dnsmasq内蔵DNS設定です。現行実行時はport=0・no-resolvを固定出力するため、この設定でDNS転送を有効にできません。DNSResolver・DNSForwarder・DNSUpstreamを使用します。一部は検証に影響します。 |
| `spec.dns.enabled` | *任意* | boolean | 既定falseです。trueは待受競合の検証でTCP/UDP 53を予約しますが、実行時のDNS待受は開きません。 |
| `spec.dns.upstreamSource` | *任意* | string; enum=["dhcpv4", "static", "system", "none"] | 空・dhcpv4・static・system・noneを許します。dhcpv4はupstreamInterface、staticはupstreamServersが必要です。現行実行時のDNS転送には影響しません。 |
| `spec.dns.upstreamInterface` | **条件付き必須** | string | dns.upstreamSource: dhcpv4で非空必須です。現行実行時のDNS解決には使わず、dnsInterfaceと異なりこの旧値はインターフェース索引との照合もありません。 |
| `spec.dns.upstreamServers` | **条件付き必須** | array | upstreamSourceがstaticなら必須のIPv4/IPv6固定値です。既定空で、検証しますが現行実行時のDNS転送では使いません。 |
| `spec.dns.cacheSize` | *任意* | integer; minimum=0 | スキーマ上0以上の整数で、既定0です。現行実行時の設定生成では使いません。DNSResolver.cacheとは別です。 |
| `spec.profiles` | *任意* | array | このサーバー内で再利用する設定一覧です。scopes[].profileRefから選ばなければ作用しない、別リソースではない項目です。既定空です。 |
| `spec.profiles[].name` | **条件付き必須** | string | サーバー内で一意のプロファイル名が必須です。空白・カンマ不可で、Kind/name接頭辞は付けません。 |
| `spec.scopes` | *任意* | array | 主プールより先に生成する、選別条件付きの追加プール一覧です。条件不一致のクライアントは主プールを使います。既定空で、複数条件への同時一致は検証で拒否しません。 |
| `spec.scopes[].name` | **条件付き必須** | string | サーバー内で一意のスコープ名が必須です。空白・カンマ不可で、dnsmasqタグ生成に使い、外部リソース参照ではありません。 |
| `spec.scopes[].match` | **条件付き必須** | object | macAddressesまたはouiPrefixesを1件以上持つ選別条件が必要です。項目は選択肢であり、両リスト同時一致の条件ではありません。 |
| `spec.scopes[].match.macAddresses` | *任意* | array | net.ParseMACで解析するMAC完全一致の一覧です。既定空です。dnsmasq出力にはコロン区切り6バイトの標準表記を使います。 |
| `spec.scopes[].match.ouiPrefixes` | *任意* | array | 任意の3バイトOUIプレフィックスです。実行時は指定値に:*:*:*を付けるためaa:bb:cc形式を使います。検証は別表記を正規化しても、実行時生成はその正規化値を使いません。 |
| `spec.scopes[].profileRef` | *任意* | string | 同じサーバーのprofiles[]名を指定します。インラインのaddressPool各値・netmask・gateway・dnsServers・ntpServers・optionsと排他です。空ならインライン設定です。 |
| `spec.profiles[].addressPool` | **条件付き必須** | object | プロファイル・インラインスコープのIPv4プールで、開始・終了の既定値はありません。 |
| `spec.profiles[].addressPool.start` | *任意* | string | 境界を含む裸IPv4開始値が必須でend以下です。プール間重複の検証はありません。 |
| `spec.profiles[].addressPool.end` | *任意* | string | 境界を含む裸IPv4終了値が必須でstart以上です。 |
| `spec.profiles[].addressPool.leaseTime` | *任意* | string | 任意のバックエンド期間です。空ならサーバーの有効期間（addressPool.leaseTime→leaseTime→12h）を継承します。 |
| `spec.profiles[].netmask` | *任意* | string | 1～32ビットが連続するドット表記IPv4ネットマスクです。空なら明示せずdnsmasqの導出に任せ、別プロファイルからは継承しません。 |
| `spec.profiles[].gateway` | *任意* | string | このタグ付きプールの任意IPv4ルーターオプションです。空でもserver.gatewayをコピーしません。 |
| `spec.profiles[].dnsServers` | *任意* | array | このタグ付きプールのIPv4 DNS一覧で既定空です。動的参照やサーバー一覧の自動継承はありません。 |
| `spec.profiles[].ntpServers` | *任意* | array | このタグ付きプールの任意IPv4 NTP一覧で、既定空です。動的参照フィールドはありません。 |
| `spec.profiles[].options` | *任意* | array | プール個別の任意追加オプションです。各項目はcode・nameの片方と非空valueが必要で、既定空です。 |
| `spec.profiles[].options[].code` | **条件付き必須** | integer; minimum=1; maximum=65535 | 数値のオプションでnameと排他です。スキーマ範囲1～65535です。 |
| `spec.profiles[].options[].name` | **条件付き必須** | string | dnsmasqオプション名でcodeと排他です。空白・カンマは禁止です。 |
| `spec.profiles[].options[].value` | *任意* | string | バックエンド形式値が必須です。CR/LFは不可で、カンマによる複数値は可能です。 |
| `spec.scopes[].addressPool` | **条件付き必須** | object | プロファイル・インラインスコープのIPv4プールで、開始・終了の既定値はありません。 |
| `spec.scopes[].addressPool.start` | *任意* | string | 境界を含む裸IPv4開始値が必須でend以下です。プール間重複の検証はありません。 |
| `spec.scopes[].addressPool.end` | *任意* | string | 境界を含む裸IPv4終了値が必須でstart以上です。 |
| `spec.scopes[].addressPool.leaseTime` | *任意* | string | 任意のバックエンド期間です。空ならサーバーの有効期間（addressPool.leaseTime→leaseTime→12h）を継承します。 |
| `spec.scopes[].netmask` | *任意* | string | 1～32ビットが連続するドット表記IPv4ネットマスクです。空なら明示せずdnsmasqの導出に任せ、別プロファイルからは継承しません。 |
| `spec.scopes[].gateway` | *任意* | string | このタグ付きプールの任意IPv4ルーターオプションです。空でもserver.gatewayをコピーしません。 |
| `spec.scopes[].dnsServers` | *任意* | array | このタグ付きプールのIPv4 DNS一覧で既定空です。動的参照やサーバー一覧の自動継承はありません。 |
| `spec.scopes[].ntpServers` | *任意* | array | このタグ付きプールの任意IPv4 NTP一覧で、既定空です。動的参照フィールドはありません。 |
| `spec.scopes[].options` | *任意* | array | プール個別の任意追加オプションです。各項目はcode・nameの片方と非空valueが必要で、既定空です。 |
| `spec.scopes[].options[].code` | **条件付き必須** | integer; minimum=1; maximum=65535 | 数値のオプションでnameと排他です。スキーマ範囲1～65535です。 |
| `spec.scopes[].options[].name` | **条件付き必須** | string | dnsmasqオプション名でcodeと排他です。空白・カンマは禁止です。 |
| `spec.scopes[].options[].value` | *任意* | string | バックエンド形式値が必須です。CR/LFは不可で、カンマによる複数値は可能です。 |
| `spec.options` | *任意* | array | プール個別の任意追加オプションです。各項目はcode・nameの片方と非空valueが必要で、既定空です。 |
| `spec.options[].code` | **条件付き必須** | integer; minimum=1; maximum=65535 | 数値のオプションでnameと排他です。スキーマ範囲1～65535です。 |
| `spec.options[].name` | **条件付き必須** | string | dnsmasqオプション名でcodeと排他です。空白・カンマは禁止です。 |
| `spec.options[].value` | *任意* | string | バックエンド形式値が必須です。CR/LFは不可で、カンマによる複数値は可能です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_dhcp.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_dhcp.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/listen_ports.go](https://github.com/imksoo/routerd/blob/main/pkg/config/listen_ports.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase2.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase2.go) · [pkg/controller/chain/dhcp_profile_leases.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/dhcp_profile_leases.go) · [pkg/render/dnsmasq.go](https://github.com/imksoo/routerd/blob/main/pkg/render/dnsmasq.go) · [cmd/routerd/runtime_services.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/runtime_services.go) · [pkg/platform/platform.go](https://github.com/imksoo/routerd/blob/main/pkg/platform/platform.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `dnsServers`, `ntpServers`, `domain`, `configPath`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
