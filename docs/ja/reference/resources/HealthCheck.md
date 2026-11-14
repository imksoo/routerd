# HealthCheck

[リソース別の設定](../resources.md#healthcheck) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

IP宛先を確認し、経路選択用のHealthy・Unhealthy状態を発行します。成功が示す範囲は指定したプロトコルと経路に限られます。

## 指定条件と参照関係

- 運用時はtargetを明示してください。targetSourceとinterfaceは検証・計画表示に使いますが、現行のヘルパー生成はtargetを直接渡し、デフォルトゲートウェイやDS-Lite対向を自動解決しません。ヘルパーはtargetが空なら起動を拒否します。
- 送信元のバインドやソケットマークは参照元のEgressRoutePolicy候補・対象から導出し、HealthCheckのYAMLでは直接指定できません。異なる導出値が競合すると経路を一意に選べません。DS-Liteに関連する確認では必要なバインドが利用可能である必要があります。
- TCPは接続確立だけを確認します。DNSは対象サーバーでexample.comのAレコードを解決します。HTTPはhttp://<target>:<port>/へGETし、4xxを含む200～499を成功扱いにします。アプリケーション固有のURLや応答本文は検査しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: HealthCheck
  metadata:
    name: "<name>"
  spec:
    [ addressFamily: ("ipv4" | "ipv6") ]
    [ enabled: <boolean> ]
    [ healthyThreshold: <integer> ]
    [ interface: <string> ]
    [ interval: <string> ]
    [ port: <integer> ]
    [ protocol: ("" | "icmp" | "tcp" | "dns" | "http") ]
    [ role: ("link" | "next-hop" | "internet" | "service" | "policy") ]
    [ target: <string> ]
    [ targetSource: ("auto" | "static" | "defaultGateway" | "dsliteRemote") ]
    [ timeout: <string> ]
    [ type: ("ping") ]
    [ unhealthyThreshold: <integer> ]
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
| `spec.enabled` | *任意* | boolean | 既定はtrueで、falseは確認処理を無効にします。 |
| `spec.type` | *任意* | string; enum=["ping"] | 既定はpingで、pingのみ指定できます。TCP・DNS・HTTPは名前にかかわらずprotocolで選択します。 |
| `spec.role` | *任意* | string; enum=["link", "next-hop", "internet", "service", "policy"] | link・next-hop・internet・service・policyの分類です。検証・計画の既定はnext-hopです。実行時の確認先やプロトコルを選ぶ値ではありません。 |
| `spec.addressFamily` | *任意* | string; enum=["ipv4", "ipv6"] | ipv4・ipv6を指定します。宛先の検証は省略をipv4扱いにするため、IPv6宛先にはipv6を明示します。targetはIPリテラルが必要で、ホスト名を解決する指定ではありません。 |
| `spec.target` | **条件付き必須** | string | addressFamilyと一致するIPアドレスです。targetSourceがstaticなら検証上も必須で、現行ヘルパーはすべてのモードで運用上必須です。実行時に使える既定値はありません。 |
| `spec.targetSource` | *任意* | string; enum=["auto", "static", "defaultGateway", "dsliteRemote"] | 検証上の既定はautoで、static・defaultGateway・dsliteRemoteも指定できます。現行の生成処理では確認先の自動取得を実装していません。前述の運用上の制限を参照してください。 |
| `spec.interface` | *任意* | string | 任意の論理インターフェース・PPPoESession・DSLiteTunnel名です。指定時は共通のインターフェース・トンネル索引に存在する必要があります。この項目だけでは現行ヘルパーの--source-interfaceになりません。 |
| `spec.protocol` | *任意* | string; enum=["", "icmp", "tcp", "dns", "http"] | 既定はicmpで、tcp・dns・httpは前述の確認を選びます。HTTPはHTTPSではなく平文です。 |
| `spec.port` | *任意* | integer; minimum=0; maximum=65535 | 0～65535です。既定の0はプロトコルに従い、TCPは443、DNSは53、HTTPは80です。ICMPはポートを使いません。 |
| `spec.interval` | *任意* | string | 正のGo durationです。ヘルパー・実行時の既定は30sです。現行の計画表示は省略時に60sと表示しますが、ヘルパーの既定値ではありません。 |
| `spec.timeout` | *任意* | string | 1回の確認に対する正のGo durationで、既定は3sです。確認処理のタイムアウトであり、ルーター再起動の期限ではありません。 |
| `spec.healthyThreshold` | *任意* | integer; minimum=1 | Healthyに移る連続成功回数で、既定は1です。失敗すると成功回数をリセットします。正の整数を使い、明示の0はスキーマの最小値を満たしません。 |
| `spec.unhealthyThreshold` | *任意* | integer; minimum=1 | Unhealthyに移る連続失敗回数で、既定は3です。成功すると失敗回数をリセットし、閾値未満の失敗はFailingです。 |
| `spec.when` | *任意* | object | 任意の共通有効化条件で、省略時は無条件です。入れ子のstate・all・anyと指定条件は共通項を参照してください。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/healthcheck/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/engine.go) · [pkg/healthcheck/dslite_binding.go](https://github.com/imksoo/routerd/blob/main/pkg/healthcheck/dslite_binding.go) · [pkg/render/healthcheck_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/healthcheck_systemd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [cmd/routerd-healthcheck/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-healthcheck/main.go) · [pkg/apply/engine.go](https://github.com/imksoo/routerd/blob/main/pkg/apply/engine.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `role`, `target`, `sourceAddress`, `sourceInterface`, `protocol`, `consecutiveFailed`, `lastCheckedAt`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/home-router-mgmt-protected.yaml)
