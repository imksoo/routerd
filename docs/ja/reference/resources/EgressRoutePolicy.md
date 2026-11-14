# EgressRoutePolicy

[リソース別の設定](../resources.md#egressroutepolicy) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

送信経路を選択・設定します。実際の動作は明示modeとhostTrafficによって異なります。

## 指定条件と参照関係

- 経路を明示して選びます。mode省略は助言状態だけで、Linux priority・mark・hashはIPv4表・nftables、hostTraffic trueは別のローカルIPv6処理です。スキーマが許してもIPv4生成経路で一般IPv6転送は実装していません。
- 有効な直接Linux制御にはinterface・device・deviceFromの出力と、正のtable・priority・markの組が必要です。検証は番号全部0を許しても実行時の直接表適用は飛ばすため、Applied・選択表示だけでは通信制御の証明になりません。Linux経路は設定metric 0なら50です。
- priorityは準備済みの最大weightを選び、小priority・名前・入力順で同点判定します。新しい監視、実デバイス、直接候補の依存、DS-Lite Upを確認します。追加プールは正常・存在する対象1件以上が必要ですが、このpriority判定は候補dependsOnを評価しません。候補なしはPending/NoReadyCandidatesで新規通信制御を外し、監視経路・既存状態の全撤去は保証しません。
- markは適格な直接候補のマークを資源・候補順に書き、重複一致は後の規則で上書きされ得ます。最大weight1件の選択ではありません。hash・priority追加プールはアドレスjhash、固定256バケットのランデブー対応、conntrackマークを使い、対象重みは設定できません。変更後も既存通信のマークは残り得ます。
- 共通device・gateway参照は実在資源・提供field明示が必須です。DHCP gatewayは自動発見せずgatewayFromを指定します。table・routeTable、routeMetric・metricの両非0は不可です。targetsは直接device・gateway・table・mark・metricと排他で、明示hash・priorityだけを許し、検証がhashだけ必須としても生成にはhashFieldsが必要です。
- Linuxの対象表設定は実リンクを確認し欠落を飛ばします。一方、その後のhash対応用コピーは通常の非DS-Lite対象では監視を見てもリンク存在確認を繰り返さないため、欠落デバイスへ新規バケットを割り当て得ます。分配には監視だけでなく実リンクも確認します。
- FreeBSDはmode hash・family ipv4・hashFields: [sourceAddress]・非空sourceCIDRs・有効候補1件・異なる固定IPv4 interface/gateway対象2件以上のみです。selection・hysteresisと候補の直接・動的・監視・依存は省略し、対象table・priority・mark・metricは0、監視・動的gatewayは禁止です。集合参照禁止、除外CIDR最大1件、宛先包含・除外併用不可です。PFはroute-to round-robin sticky-addressで、Linuxの決定的hashではありません。
- FreeBSD生成はこのroute-to形と明示FirewallRule・自動穴あけ・FirewallEventLog・ClientPolicyとの混在を拒否します。FirewallZoneの選別がある場合は広い許可転送経路が必要です。構文検証だけでは他資源と組み合わせた最終PF生成を保証しません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: EgressRoutePolicy
  metadata:
    name: "<name>"
  spec:
    candidates: # 必須; array
      - # object
        dependsOn: # 任意; array
          - # object
            equals: "<string>" # 任意; string
            field: "<string>" # 任意; string
            notEmpty: true # 任意; boolean
            optional: true # 任意; boolean
            phase: "<string>" # 任意; string
            resource: "<string>" # 必須; string
        device: "<string>" # 任意; string
        deviceFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        enabled: true # 任意; boolean
        gateway: "<string>" # 任意; string
        gatewayFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        gatewaySource: "" # 任意; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
        healthCheck: "<string>" # 任意; string
        interface: "<string>" # 任意; string
        mark: 0 # 任意; integer; minimum=0; maximum=4294967295
        metric: 0 # 任意; integer; minimum=0
        name: "<string>" # 任意; string
        preferredSource: "" # 任意; string; enum=["", "interface"]
        priority: 0 # 任意; integer; minimum=0; maximum=32765
        routeMetric: 0 # 任意; integer; minimum=0
        routeTable: 0 # 任意; integer; minimum=0; maximum=4294967295
        source: "<string>" # 任意; string
        table: 0 # 任意; integer; minimum=0; maximum=4294967295
        targets: # 任意; array
          - # object
            gateway: "<string>" # 任意; string
            gatewayFrom: # 任意; object
              field: "<string>" # 任意; string
              optional: true # 任意; boolean
              resource: "<string>" # 必須; string
            gatewaySource: "" # 任意; string; enum=["", "static", "dhcpv4", "dhcpv6", "none"]
            healthCheck: "<string>" # 任意; string
            interface: "<string>" # 任意; string
            mark: 0 # 任意; integer; minimum=0; maximum=4294967295
            metric: 0 # 任意; integer; minimum=0
            name: "<string>" # 任意; string
            outboundInterface: "<string>" # 任意; string
            priority: 0 # 任意; integer; minimum=0; maximum=32765
            routeMetric: 0 # 任意; integer; minimum=0
            routeTable: 0 # 任意; integer; minimum=0; maximum=4294967295
            table: 0 # 任意; integer; minimum=0; maximum=4294967295
        weight: 0 # 任意; integer; minimum=0
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
    destinationCIDRs: # 任意; array
      - "<string>" # string
    destinationSetRefs: # 任意; array
      - "<string>" # string
    excludeDestinationCIDRs: # 任意; array
      - "<string>" # string
    excludeDestinationSetRefs: # 任意; array
      - "<string>" # string
    family: "ipv4" # 任意; string; enum=["ipv4", "ipv6"]
    hashFields: # 任意; array
      - "<string>" # string
    hostTraffic: true # 任意; boolean
    hysteresis: "<string>" # 任意; string
    mode: "priority" # 任意; string; enum=["priority", "mark", "hash"]
    selection: "highest-weight-ready" # 任意; string; enum=["highest-weight-ready", "weighted-ecmp"]
    sourceCIDRs: # 任意; array
      - "<string>" # string
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
| `spec.family` | 既定ipv4です。スキーマ・検証はipv6も許しますが、現行Linuxの転送mark・priority・hashはIPv4専用です。IPv6はhostTraffic別経路で、一般IPv6転送との同等性はありません。FreeBSDの対応形もipv4必須です。 |
| `spec.hostTraffic` | 既定falseです。Linuxのtrueはfamily ipv6・候補1件・preferredSource: interface・固定static gateway・mark 0・targetsなしが必須です。CIDR選別ではなく全ローカル通信のiif lo方針を作ります。デバイス・アドレス未準備時は以前の有効規則を保持し、初回はPendingです。この別処理は候補health・依存・選択条件を評価しません。 |
| `spec.mode` | 省略・空は助言用の状態選択だけで経路表を適用しません。priorityは準備済み候補1件を新規通信のマークに選び、markは直接パケットマーク、hashは候補targetsへ通信を割り当てます。Linux転送ではmodeを明示します。FreeBSDは制限付きhash形だけです。 |
| `spec.sourceCIDRs` | 任意の同系統送信元プレフィックスで、既定は全送信元、一覧はORです。FreeBSD hashは1件以上のIPv4が必要で、hostTrafficはこの選別を使いません。 |
| `spec.destinationCIDRs` | 任意の同系統宛先プレフィックスです。destinationSetRefsも空なら全宛先で、mark・hashでは集合参照とOR結合します。priorityの既定マーク生成はCIDRだけを使い集合参照を使いません。 |
| `spec.destinationSetRefs` | 任意の既存IPAddressSet/<name>参照で、重複は拒否します。Linux mark・hashの選別に使いますが、priorityの既定マークは反映せずFreeBSDは禁止です。空なら集合による選択肢はありません。 |
| `spec.excludeDestinationCIDRs` | 任意の同系統宛先除外で既定空です。全除外を避ける条件となり、IPv4 /0除外では選別対象なしです。FreeBSDは最大1件で非空destinationCIDRsとの併用禁止です。 |
| `spec.excludeDestinationSetRefs` | 任意の既存IPAddressSet/<name>除外です。重複拒否でLinux mark・hashに使い、priority既定マークでは使わずFreeBSDは禁止です。 |
| `spec.hashFields` | sourceAddress・destinationAddressの順序付き一覧です。hashで必須で、priorityの追加プールを含めtargetsがある生成でも必要です。Linuxはポートでなく指定アドレスのjhash、資源由来固定seed、256バケットのランデブー対応を使います。FreeBSDは[sourceAddress]だけを許しますが実処理はjhashでなくround-robin sticky-addressです。 |
| `spec.selection` | 既定highest-weight-readyです。準備済みをweight降順・priority昇順・有効名辞書順・入力順で選びます。weighted-ecmpはスキーマ上許しても助言・priorityで未実装のPending/UnsupportedSelectionです。hash対象算法とは別でmark・hashはこの選択を使いません。FreeBSDは空必須です。 |
| `spec.hysteresis` | Go durationで、省略modeの助言選択だけ既定30sです。変更後の期間内に以前の選択が準備済みなら保持し、未準備候補は保持しません。0・負値は遅延なしです。明示priority・mark・hashは使わずFreeBSDは空必須です。 |
| `spec.candidates` | 1件以上の候補が必須で、無効候補も構造検証します。Linux priorityは直接候補・追加プール候補を混在できます。候補名の一意検証はなく、状態・hash識別の曖昧さを避けるため明示の一意名を使います。 |
| `spec.when` | 共通状態条件で既定無条件です。不成立なら有効設定から除外し、候補条件は別途選別します。未解決条件は未確定として残ることがあり、必ず停止するとは限りません。 |
| `spec.candidates[].name` | 任意の識別名です。助言はsource→candidate-N（0始まり）、明示priority状態はinterface/device→targets、無名hashプールは入力番号で補います。安定した識別には一意名を使います。 |
| `spec.candidates[].enabled` | 既定trueです。falseは選択・生成から除外しますが、設定・参照検証は省略せず、既存通信・経路表の全消去を単独では保証しません。 |
| `spec.candidates[].source` | Kind/nameで指定する任意の助言準備状態参照で、device・gatewayは自動取得しません。この文字列は型付き共通参照の存在検証対象外です。助言はApplied・Bound・Healthy・Installed・Ready・Running・Upを認め、新しい正常監視で未準備sourceを補える場合があります。明示経路はDS-LiteのUpを特別確認しますが他kindを一般確認しません。 |
| `spec.candidates[].interface` | 明示Linux経路の宣言済み論理名でdeviceより優先します。指定時はRouterのinterface・DS-Lite索引を検証します。省略modeの助言出力はこの別名を使わずdevice・deviceFromを使います。targets非空では禁止です。 |
| `spec.candidates[].device` | 任意の固定デバイス補完値です。明示経路はinterfaceを先に使い、助言はdeviceを直接使います。この文字列の共通存在検証はありませんが、実行時には実リンクが必要です。${...}は廃止でdeviceFromを使います。targetsとは併用禁止です。 |
| `spec.candidates[].deviceFrom` | 既存リソース・提供fieldを検証する共通状態参照です。正規化先頭値がinterface・deviceより優先し、未解決なら固定値へ補完します。optionalでもデバイスは作らずtargetsとの併用不可です。 |
| `spec.candidates[].gateway` | familyと同じ裸アドレスです。gatewaySource: staticが必要で、その場合gatewayFromと排他です。DHCPモードの固定値は禁止、既定空、追加プール候補でも禁止です。 |
| `spec.candidates[].gatewayFrom` | 既存resource・提供fieldの状態参照です。staticはgateway・gatewayFromの片方必須で、DHCPモードもこれを使えますがinterfaceから自動発見しません。助言はoptional trueでも設定参照の未解決を未準備とし、明示適用はstatic・DHCPの未解決をエラーにするためoptionalは一般回避ではありません。targetsと併用禁止です。 |
| `spec.candidates[].gatewaySource` | 既定noneでデバイス既定経路となりgateway指定禁止です。staticは固定・参照の片方、dhcpv4はIPv4・固定なし・明示gatewayFromの有効値が必要です。dhcpv6はIPv6で検証許可しても通常IPv4適用は非対応で、hostTraffic別経路もstatic必須です。追加プール候補では禁止です。 |
| `spec.candidates[].preferredSource` | 空・interfaceです。hostTrafficでinterface必須となり、候補上のtentative・deprecatedでない最初のグローバルIPv6を使います。通常IPv4転送・追加プールでは使いません。 |
| `spec.candidates[].table` | Linux経路表番号でスキーマ0～4294967295、既定0は未設定です。非0のrouteTableと排他です。有効な直接制御は正のtable・priority・markが必要でhostTrafficだけmark 0です。targets候補は禁止、FreeBSDは0必須です。 |
| `spec.candidates[].routeTable` | tableが0の場合の別名で、両方非0は拒否します。範囲・条件はtableと同じです。 |
| `spec.candidates[].priority` | 直接mark・table・priorityを設定する場合のip rule優先度1～32765です。既定0は有効な適用規則になりません。候補選択の小さい値優先の同点判定にも使い、追加プール候補でも直接table・markなしで指定できます。FreeBSDは0必須です。 |
| `spec.candidates[].mark` | Linuxのpacket・fwmark識別子でスキーマ0～4294967295です。直接転送は正のmark・table・priorityが必要で、既定0なら直接経路表適用を飛ばします。hostTrafficは0、追加プール候補とFreeBSDは直接非0禁止です。 |
| `spec.candidates[].routeMetric` | 0以上の優先メトリックです。非0のmetricと排他で、有効0ならhostTrafficを含むLinux適用は50にします。助言状態は設定0を表示する場合があります。targetsは禁止、FreeBSDは0必須です。 |
| `spec.candidates[].metric` | routeMetricが0の場合の別名です。両方非0は不可で、Linux適用時の有効既定は同じ50です。 |
| `spec.candidates[].weight` | 0以上で既定0です。助言・priority選択は大きいほど優先します。割合・ECMP重み・対象別hash重みではなく、mark・hashの比例配分には使いません。FreeBSDは0必須です。 |
| `spec.candidates[].healthCheck` | 既存HealthCheckの任意裸名で、空なら監視条件なしです。選択には新しいHealthyまたは異常閾値前のFailingと、max(2分,3×interval+timeout)以内のlastCheckedAtが必要です。同じmark経由の監視なら異常時でも監視用表を適用し得ますが通信適格性とは別です。hostTrafficは使わずFreeBSDは禁止です。 |
| `spec.candidates[].targets` | 任意の追加対象プールでhash・priorityだけです。直接interface・device・deviceFrom・gateway・gatewayFrom・gatewaySource・table・routeTable・mark・routeMetric・metricと排他です。Linuxは対象別の正table・priority・markとhashFieldsが必要、FreeBSDは候補1件・異なる固定経路先2件以上でLinux番号なしです。 |
| `spec.candidates[].dependsOn` | 共通AND依存で既定空です。通常参照検証を行い、助言・明示直接経路・mark/hash候補選別で評価します。現行priority追加プールの適格判定は候補dependsOnを評価せずhostTrafficも使いません。候補全体の停止にはwhenを使います。FreeBSDは禁止です。 |
| `spec.candidates[].when` | 共通状態条件で既定無条件です。不成立はコントローラー選択前に除外します。未解決は未確定として残る場合があり、enabled falseも除外します。FreeBSD固定形の候補条件は禁止です。 |
| `spec.candidates[].targets[].name` | 任意対象名で既定空です。有効interface・mark・tableとともに固定hash識別に使い、変更は新規通信を再配分し得ます。Linuxでは名前・mark・priority・tableの一意性を一般検証しません。 |
| `spec.candidates[].targets[].interface` | outboundInterfaceがなければ必須の論理対象名です。宣言済みinterface・DS-Lite索引を検証します。Linuxは両指定ならoutboundInterface優先で、FreeBSDは両方非空を拒否します。 |
| `spec.candidates[].targets[].outboundInterface` | interfaceより優先する別名で、同じ参照検証です。実配送には利用可能なOSインターフェースが必要で、トンネルは作りません。 |
| `spec.candidates[].targets[].gateway` | familyと合う固定値で、gatewaySource staticが必要かつgatewayFromと排他です。DHCPの固定値は禁止し、FreeBSD対象ではこの固定IPv4が必須です。 |
| `spec.candidates[].targets[].gatewayFrom` | 共通resource・field参照でoptional trueでも存在・提供fieldを検証します。staticは固定値・参照の片方必須です。Linuxは先頭値を使いoptionalはgateway欠落回避にはなりません。FreeBSDは動的gateway自体を禁止します。 |
| `spec.candidates[].targets[].gatewaySource` | 既定noneでgateway・gatewayFrom禁止、デバイス経路です。staticはgateway・gatewayFromの片方必須です。dhcpv4/6は同系統・固定なしですが自動取得せずgatewayFromを指定します。通常実行時はnone・static・dhcpv4だけ対応し、FreeBSDはstatic必須です。 |
| `spec.candidates[].targets[].table` | Linuxは全対象で正の表番号が必須で、スキーマ上限4294967295、非0routeTableと排他です。自動採番・予約表・一意検証はありません。FreeBSDは省略・0でPF route-toを使います。 |
| `spec.candidates[].targets[].routeTable` | tableが0の場合の別名です。両方非0は拒否し、Linux正値・FreeBSD 0の条件は同じです。 |
| `spec.candidates[].targets[].priority` | Linuxの対象ip rule優先度1～32765が必須です。自動採番はなく、候補選択のpriorityとは別です。FreeBSDは省略・0です。 |
| `spec.candidates[].targets[].mark` | Linuxの対象fwmarkは正値必須でスキーマ上限4294967295です。hashバケットとip ruleに使い、自動採番・衝突検証はありません。FreeBSDは省略・0です。 |
| `spec.candidates[].targets[].routeMetric` | 0以上のLinuxメトリックでmetricより優先します。両方非0は不可で、有効な省略・0は適用時50です。FreeBSDは省略・0必須です。 |
| `spec.candidates[].targets[].metric` | routeMetricが0の場合の別名です。同じ条件・Linux有効既定50を適用します。 |
| `spec.candidates[].targets[].healthCheck` | 既存HealthCheckの任意裸名で、既定監視条件なしです。候補と同じ鮮度・閾値を使い、異常対象は新規hash対応から外します。既存conntrackマークは復元するため全通信を終了せず、同markの監視用表は残り得ます。FreeBSDは禁止です。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_route.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_route.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/egressroute/selection.go](https://github.com/imksoo/routerd/blob/main/pkg/egressroute/selection.go) · [pkg/controller/chain/policyroute.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/policyroute.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/resourcequery/when.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/when.go) · [pkg/resourcequery/resourcequery.go](https://github.com/imksoo/routerd/blob/main/pkg/resourcequery/resourcequery.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `family`, `selectedCandidate`, `selectedSource`, `selectedDevice`, `selectedGateway`, `selectedGatewaySource`, `selectedRouteTable`, `selectedMetric`, `selectedWeight`, `selectedTargets`, `selectedInterface`, `candidates`, `role`, `advisory`, `lastTransitionAt`, `datapathRoleResource`, `datapathRole`, `datapathRoleTransitionAt`, `datapathReady`, `datapathState`, `datapathReadyAt`, `datapathReadyDuration`, `datapathReadyDurationMillis`, `datapathFallbackBehavior`, `newFlowBehavior`, `updatedAt`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
