# IngressService

[リソース別の設定](../resources.md#ingressservice) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: firewall.routerd.net/v1alpha1` · `metadata.name`

IPv4 TCP・UDPの公開先をヘルスチェックと転送先選択で制御し、任意でローカルDNS・内部折り返しNATを使います。L4 NATであり、HTTPプロキシ・TLS終端ではありません。

## 指定条件と参照関係

- listenと1件以上のbackendが必須です。各backendはaddress・addressFromのちょうど一方とportが必要で、固定値はIPv4・ホスト名を受け付けます。転送はIPv4です。ヘルス履歴は名前単位のため、安定した一意の名前を使ってください。省略時はbackend-0・backend-1等です。
- healthCheck.intervalにかかわらず通常5秒ごとに各backendを順番に確認します。方式省略はlistenがUDPでもTCPです。ホスト名は文字列順の先頭IPv4を使い、DNS失敗時は前回アドレスを再利用する場合がありますが、疎通確認は継続します。
- NoHealthyBackends・policy.onNoHealthyBackendsは状態表示で、drop・reject生成の保証ではありません。有効backendがないとNAT側は元のbackend定義を残すため、最初の静的転送先へ戻る場合があります。本設定を全異常時の確実な遮断として扱わないでください。
- failoverは宣言順で最初の正常backendを選びます。LinuxのsourceHash・randomは正常な解決済み転送先へ分散できますがweightは使いません。PFは先頭の選択先だけを出し分散方式を実装しません。静的描画はヘルス状態による選択をせず、宣言先頭を使う場合があります。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: IngressService
  metadata:
    name: "<name>"
  spec:
    backends: # 必須; array
      - # object
        address: "<string>" # 任意; string
        addressFrom: # 任意; object
          field: "<string>" # 任意; string
          optional: true # 任意; boolean
          resource: "<string>" # 必須; string
        name: "<string>" # 任意; string
        port: 1 # 必須; integer; minimum=1; maximum=65535
        weight: 0 # 任意; integer; minimum=0
    externalDNS: true # 任意; boolean
    hairpin: # 任意; object
      enabled: true # 任意; boolean
      interfaces: # 任意; array
        - "<string>" # string
      mode: "" # 任意; string; enum=["", "auto", "manual", "off"]
    healthCheck: # 任意; object
      expectedBody: "<string>" # 任意; string
      expectedStatus: # 任意; array
        - 0 # integer
      healthyThreshold: 1 # 任意; integer; minimum=1
      host: "<string>" # 任意; string
      interval: "<string>" # 任意; string
      path: "<string>" # 任意; string
      protocol: "" # 任意; string; enum=["", "tcp", "http", "https"]
      timeout: "<string>" # 任意; string
      tlsSkipVerify: true # 任意; boolean
      unhealthyThreshold: 1 # 任意; integer; minimum=1
    hostname: "<string>" # 任意; string
    listen: # 必須; object
      address: "<string>" # 任意; string
      addressFrom: # 任意; object
        field: "<string>" # 任意; string
        optional: true # 任意; boolean
        resource: "<string>" # 必須; string
      interface: "<string>" # 必須; string
      port: 1 # 必須; integer; minimum=1; maximum=65535
      protocol: "tcp" # 必須; string; enum=["tcp", "udp"]
    policy: # 任意; object
      onNoHealthyBackends: "" # 任意; string; enum=["", "drop", "reject"]
      selection: "" # 任意; string; enum=["", "failover", "sourceHash", "random"]
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
| `spec.listen` | 必須のIPv4公開側設定です。NAT・ファイアウォールを作り、アプリの待受・アドレス割当は行いません。 |
| `spec.listen.interface` | 必須の裸の論理インターフェース名で、Kind/nameではありません。設定索引に存在し、OS名を解決する必要があります。 |
| `spec.listen.address` | 任意の裸の宛先IPv4です。addressFromと排他で、省略すると待受インターフェース上の全宛先アドレスに一致します。 |
| `spec.listen.addressFrom` | addressと排他の共通resource・field・optional参照です。NAT・静的描画は宣言済みIPv4StaticAddress.address・VirtualAddress.addressに対応し、任意の実行時statusは読みません。一般参照の検証成功だけでは描画対応を保証しません。 |
| `spec.listen.protocol` | tcp・udpが必須で既定値はありません。転送通信の選択であり、ヘルスチェック方式とは別です。 |
| `spec.listen.port` | 必須の宛先ポート1～65535です。範囲・一覧・既定値はありません。 |
| `spec.hairpin` | 公開アドレス経由で内部へ戻す任意のNAT設定です。有効時はlistenアドレスの解決が必要です。PortForwardの既定はoff、IngressServiceはautoです。 |
| `spec.hairpin.enabled` | 既定falseです。mode省略時、trueはPortForwardでmanual、IngressServiceは引き続きautoです。mode=offはtrueでも無効にします。 |
| `spec.hairpin.mode` | auto・manual・offです。autoはlisten・backendが宣言IPv4サブネットまたは同じプライベート/24にある場合にlisten側を選び、任意LAN経路を検出するものではありません。manualでルールを作るにはenabled=trueとinterfacesが必要です。 |
| `spec.hairpin.interfaces` | 任意の裸の宣言論理名で重複は禁止です。有効なmanualには1件以上必要です。autoはlisten側も追加し、IPv4 DNATとmasquerade・PF SNATで戻りをルーター経由にします。 |
| `spec.when` | 共通の有効化条件で、既定は無条件です。不成立は実効生成から除きます。判定・スキーマ制約は共通項を参照してください。 |
| `spec.hostname` | 任意のFQDNで、ローカルDNS登録・状態表示に使います。既定は空で、HTTP Host振分・検査Host/SNIには使いません。登録には対応するDNSZoneをDNSResolverで提供する必要があります。 |
| `spec.externalDNS` | 既定falseです。trueは自動ローカル名登録とDNS提供範囲の警告を省き、外部DNS事業者への登録は行いません。 |
| `spec.backends` | 1件以上必須の転送先一覧で、宣言順がfailover優先順です。設定はホスト名を受理しますが、静的NAT描画はIPv4解決済み値が必要でDNSを引きません。 |
| `spec.backends[].name` | 任意のヘルス履歴キーで、既定はbackend-<0始まり番号>です。重複はこの検証で拒否せず履歴が混ざり得るため、一意名にしてください。 |
| `spec.backends[].address` | 裸のIPv4・ホスト名で、addressFromとちょうど一方が必須です。ホスト名はIPv4だけを解決し、DNS失敗時に前回値があれば保持します。 |
| `spec.backends[].addressFrom` | addressと排他の共通参照です。稼働検査は実行statusを読み、optionalでも未解決先を正常にしません。静的描画の対応は前述の宣言アドレス値だけです。 |
| `spec.backends[].port` | 必須の転送先ポート1～65535です。実通信の変換先とヘルスチェック方式の接続先に使います。 |
| `spec.backends[].weight` | 0以上の整数で既定0ですが、現在のヘルス選択・Linux/PF NATは読みません。重み付き分散にはなりません。 |
| `spec.healthCheck` | 任意の稼働検査設定です。省略は無効化でなくTCP検査になります。 |
| `spec.healthCheck.protocol` | tcp（既定）・http・httpsです。listen.protocolとは独立し、HTTP(S)は解決済みbackendのアドレス・ポートへGETします。 |
| `spec.healthCheck.interval` | 検証でGo形式の期間を受け付けますが、コントローラーは読みません。実際はチェーンの5秒間隔と再評価イベントです。 |
| `spec.healthCheck.timeout` | 実効既定1sの正のGo形式期間です。0・負数も構文検証は通りますが、実行時は省略と同じ1sです。backendごとの検査で、全周期・DNS解決の期限ではありません。 |
| `spec.healthCheck.path` | HTTP(S)のURLパスで既定/です。非空は/で始め、TCP検査では使いません。 |
| `spec.healthCheck.host` | 任意のHTTP Hostヘッダー・HTTPS TLS ServerName/SNI指定です。既定は空で、URLは解決済みIPを使います。空白・制御文字は禁止です。 |
| `spec.healthCheck.expectedStatus` | 任意のHTTP状態許可一覧100～599です。省略・空は200～399を許可し、HTTPクライアントは既定でリダイレクトを追った結果を評価します。 |
| `spec.healthCheck.tlsSkipVerify` | 既定falseです。trueはHTTPS検査の証明書・ホスト検証だけを省き、実通信のTLS処理ではありません。 |
| `spec.healthCheck.expectedBody` | HTTP応答本文の先頭1MiB内の大文字小文字を区別する部分一致です。既定の空は本文検査なしで、正規表現ではありません。 |
| `spec.healthCheck.healthyThreshold` | 正常へ移す連続成功数で、省略・0は1です。状態・回数はbackend statusから引き継ぎます。スキーマ最小1と実装の0既定は異なります。 |
| `spec.healthCheck.unhealthyThreshold` | 正常だったbackendを外す連続失敗数です。省略・0は1で、初回の未解決先は異常から開始します。 |
| `spec.policy` | 任意の選択設定で既定はfailover・dropです。ただし全異常時の動作は現在、表示だけです。 |
| `spec.policy.selection` | failover（既定）・sourceHash・randomです。LinuxのsourceHashはクライアントIPv4送信元のハッシュ、randomは同じ重みのbackend件数で選びます。正常1件はfailoverへ戻し、PFは常に1先です。 |
| `spec.policy.onNoHealthyBackends` | drop（既定）・rejectを受理しますがstatus.conditions.reasonに入れるだけです。明示drop・rejectルールは生成せず、前述の全異常時の戻り挙動に注意してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/ingressservice/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/ingressservice/controller.go) · [pkg/controller/nat44/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/nat44/controller.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/ingress.go](https://github.com/imksoo/routerd/blob/main/pkg/render/ingress.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/render/pf.go](https://github.com/imksoo/routerd/blob/main/pkg/render/pf.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `hostname`, `listenAddress`, `activeBackend`, `activeBackends`, `healthyBackends`, `totalBackends`, `backends`, `observedAt`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/k8s-api-vip-dualstack.yaml)
