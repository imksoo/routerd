# VirtualAddress

[リソース別の設定](../resources.md#virtualaddress) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

固定アドレス、または選出したノードの共有アドレスを維持します。Linuxは固定にiproute2・VRRPにkeepalived、FreeBSDはifconfig・CARPを使います。

## 指定条件と参照関係

- family・interfaceとaddress・addressFromのちょうど一方が必須です。固定IPv4値は/32、IPv6 VRRP・CARPは/128で、固定IPv6だけは他のプレフィックス長も許可します。addressFromの裸の値は/32・/128にします。
- interfaceは設定のインターフェース索引に存在する必要があります。同じインターフェースでIPv4StaticAddressと同じ索引アドレスを管理できず、同インターフェースのIPv4StaticAddressをaddressFromにする指定も拒否します。VRRP IDはfamily・interfaceごとに一意です。
- addressFromは一般の参照・field検証を通しますが、アドレス描画は宣言済みIPv4StaticAddress.address・VirtualAddress.addressだけを読み、任意の実行statusは読みません。optionalでも最終VIPは空にできず、描画・適用でError・Pendingになる場合があります。
- mode=vrrpはvirtualRouterIDが必須で、Linuxでは1件以上のユニキャストpeersも必要です。FreeBSDはCARPでpeersをユニキャスト配送先に使いません。preempt省略は無効ですが、CARPのpreemptはホスト共通で1件でもtrueならsysctlを有効にします。
- 切替VMACは別のVRRP選挙を持たず、このVIPの選出に従います。Linux専用ヘルパーは実行時も名前・MAC・リンクローカル値を検証しますが、additionalFailoverVMACsのYAML検証は主項目より限定的です。描画・適用時に有効な親参照とヘルパーが必要です。
- gracefulActivationはLinux IPv4 VRRP専用です。VIP公開前に選出・VMAC有効化を行い、readyWhenがVIP公開を許可します。期限超過は失敗表示だけでVIPを保留して再試行し、強制公開しません。一度正常に公開したVIPは、主系のままなら後の準備条件低下だけでは取り下げません。
- VRRP広告間隔は実装が決め（通常1秒）、advertInterval・preemptDelayでは設定しません。旧メンバーはYAML・JSONスキーマから除外しています。優先度・認証の実際の動作はkeepalived・CARPにも依存します。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: VirtualAddress
  metadata:
    name: "<name>"
  spec:
    [ address: <string> ]
    [ addressFrom:
        field: <string>
        [ optional: <boolean> ]
        resource: <string>
    ]
    [ externalDNS: <boolean> ]
    family: ("ipv4" | "ipv6")
    [ gratuitousARP: <boolean> ]
    [ hostname: <string> ]
    interface: <string>
    [ mode: ("" | "static" | "vrrp") ]
    [ track:
        -
          [ confirmConsecutiveHealthy: <integer> ]
          [ confirmConsecutiveUnhealthy: <integer> ]
          resource: <string>
          [ unhealthyPenalty: <integer> ]
    ]
    [ vrrp:
        [ additionalFailoverVMACs:
            -
              interface: <string>
              [ linkLocalAddress: <string> ]
              macAddress: <string>
              parentInterface: <string>
              [ withdrawRouterAdvertisement: <boolean> ]
        ]
        [ authentication: <string> ]
        [ authenticationFrom:
            [ base64: <boolean> ]
            [ env: <string> ]
            [ file: <string> ]
        ]
        [ failoverVMAC:
            interface: <string>
            [ linkLocalAddress: <string> ]
            macAddress: <string>
            parentInterface: <string>
            [ withdrawRouterAdvertisement: <boolean> ]
        ]
        [ gracefulActivation:
            readyWhen:
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
            [ timeout: <string> ]
        ]
        [ peers:
            - <string>
        ]
        [ preempt: <boolean> ]
        [ priority: <integer> ]
        [ virtualRouterID: <integer> ]
    ]
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
| `spec.family` | **必須** | string; enum=["ipv4", "ipv6"] | ipv4・ipv6が必須で既定値はありません。アドレスの系統と一致させます。 |
| `spec.interface` | **必須** | string | 必須の裸の宣言論理インターフェース名で、OS名へ解決します。Kind/nameではありません。 |
| `spec.address` | **条件付き必須** | string | addressFromと排他の固定プレフィックスです。IPv4は/32、IPv6は固定mode以外/128です。意味検証はaddressFromを許可しますが、JSON Schemaはaddressを必須としています。 |
| `spec.addressFrom` | **条件付き必須** | object | addressと排他の共通参照です。実効描画は宣言IPv4StaticAddress.address・VirtualAddress.addressで、field明示が必要です。VirtualAddressの参照を再帰的にたどりません。 |
| `spec.hostname` | *任意* | string | 提供中のDNSZoneへのローカル名登録・状態表示用FQDNです。既定空で、OSホスト名・外部DNS登録は変更しません。 |
| `spec.externalDNS` | *任意* | boolean | 既定falseです。trueはローカル名登録・DNS提供範囲警告を省き、外部DNSへの通信はしません。 |
| `spec.mode` | *任意* | string; enum=["", "static", "vrrp"] | static（既定）・vrrpです。staticは選挙なしで自機に適用し、vrrpはLinux keepalived・FreeBSD CARPです。vrrp・trackオブジェクトは固定の所有を制御しません。 |
| `spec.gratuitousARP` | *任意* | boolean | 既定falseで、trueはLinux固定IPv4だけです。欠落VIPの追加・復元後にarping -U -c 3を使い、通常の再評価では連発しません。失敗は報告し再試行します。 |
| `spec.when` | *任意* | object | 共通の有効化条件で、既定は無条件です。リソース全体を除外する条件で、gracefulActivation.readyWhenの公開条件とは別です。 |
| `spec.vrrp` | **条件付き必須** | object | mode=vrrp専用の選出設定です。staticは使わず、staticでは子項目すべてを検証するわけではありません。 |
| `spec.vrrp.virtualRouterID` | **条件付き必須** | integer; minimum=1; maximum=255 | vrrpで必須の1～255で、CARPのvhidへ対応します。同じRouterのfamily・interfaceごとに一意とし、同じ選挙の相手と合わせます。 |
| `spec.vrrp.priority` | *任意* | integer; minimum=1; maximum=254 | 1～254で省略・0は100です。trackの減点後も最小1にし、CARPはadvskew=max(1,254-priority)へ変換します。 |
| `spec.vrrp.preempt` | *任意* | boolean | 省略・falseは高優先度だけを理由に主系を奪い返しません。trueで有効です。CARPは全体のnet.inet.carp.preemptで、1定義でもtrueなら有効です。 |
| `spec.vrrp.peers` | **条件付き必須** | array | Linux vrrpで必須の相手アドレス・ホスト名一覧です。構文を検証し、描画で重複排除・ソートします。FreeBSD CARPは相手指定として使いません。 |
| `spec.vrrp.authentication` | *任意* | string | 任意の平文認証値で、既定はなしです。authenticationFromと排他です。Linuxはkeepalived auth_type PASS、CARPはpassで、VRRP通信の暗号化ではありません。 |
| `spec.vrrp.authenticationFrom` | *任意* | object | 平文authenticationと排他の秘密値取得元です。指定時はfile・envのちょうど一方が必要です。描画時に読み、ファイル・環境欠落や不正Base64はそこで失敗します。解決結果が空なら認証を省略します。 |
| `spec.vrrp.authenticationFrom.file` | *任意* | string | ルーター上の秘密値ファイルでenvと排他です。描画は末尾CR・LFを除きますが、全空白を除くわけではありません。 |
| `spec.vrrp.authenticationFrom.env` | *任意* | string | routerdプロセスの環境変数名でfileと排他です。シェル展開・相手環境変数ではありません。 |
| `spec.vrrp.authenticationFrom.base64` | *任意* | boolean | 既定falseです。trueは符号化値前後の空白を除いて標準Base64を解読し、結果末尾のCR・LFも除きます。 |
| `spec.vrrp.failoverVMAC` | *任意* | object | 本VRRP選出で制御する任意のLinux macvlan識別です。別選挙はありません。主項目はLinux VRRPで検証し、FreeBSDでは使えません。 |
| `spec.vrrp.additionalFailoverVMACs` | *任意* | array | 同じ役割遷移に従う追加VMACで、既定は空です。子項目はfailoverVMACと同じ意味です。現行YAML検証は限定的ですが、Linuxヘルパー・描画で正しい親・インターフェース名・Ethernet MACが必要です。 |
| `spec.vrrp.gracefulActivation` | *任意* | object | 任意のLinux IPv4 VRRP公開条件です。既定なしで、他family・FreeBSDは禁止です。選出後に依存状態を準備し、VIP公開を待ちます。 |
| `spec.vrrp.gracefulActivation.readyWhen` | **条件付き必須** | object | 条件定義時に必須の空でない共通state・all・anyです。参照・条件を検証し、初回公開だけを制御して、正常公開後の継続取り下げ条件ではありません。 |
| `spec.vrrp.gracefulActivation.timeout` | *任意* | string | 正のGo形式期間で既定45sです。満了はFailed・ReadinessTimeout表示に変えるだけで、VIP強制公開・再試行停止・ルーター再起動はしません。 |
| `spec.track` | *任意* | array | 任意のリソース健全性一覧で既定は空、vrrp専用です。異常の減点を合計して選出優先度を下げ、固定VIPを直接消しません。 |
| `spec.track[].resource` | **条件付き必須** | string | 必須のKind/name形式で、ここでは形式のみを検証します。phaseを読み、通常の準備完了系phaseは正常、BGPRouter・BGPPeerはEstablishedが必要です。Disabled・Standby・NotApplicable・when不成立は中立で現在の減点状態を維持します。 |
| `spec.track[].unhealthyPenalty` | *任意* | integer; minimum=0; maximum=254 | 0～254で省略・0は50です。0で減点を無効にはできません。異常確認後に適用し、正常確認後に戻します。 |
| `spec.track[].confirmConsecutiveUnhealthy` | *任意* | integer; minimum=1; maximum=255 | 減点前の連続異常評価数で、省略・0は3、明示値1～255です。秒数でなく再評価の観測回数です。 |
| `spec.track[].confirmConsecutiveHealthy` | *任意* | integer; minimum=1; maximum=255 | 減点解除までの連続正常評価数で、省略・0は2、明示値1～255です。減点判断・回数は再起動後に保存statusから復元できます。 |
| `spec.vrrp.failoverVMAC.parentInterface` | **条件付き必須** | string | 必須の設定論理親インターフェース名で、描画時にOS名へ解決します。状態値参照ではありません。 |
| `spec.vrrp.additionalFailoverVMACs[].parentInterface` | **条件付き必須** | string | 必須の設定論理親インターフェース名で、描画時にOS名へ解決します。状態値参照ではありません。 |
| `spec.vrrp.failoverVMAC.interface` | **条件付き必須** | string | 必須のLinux VMACデバイス名です。15文字以内の有効なインターフェース名とし、役割ヘルパーが作成・有効化・無効化します。 |
| `spec.vrrp.additionalFailoverVMACs[].interface` | **条件付き必須** | string | 必須のLinux VMACデバイス名です。15文字以内の有効なインターフェース名とし、役割ヘルパーが作成・有効化・無効化します。 |
| `spec.vrrp.failoverVMAC.macAddress` | **条件付き必須** | string | 必須の6バイトEthernet MACです。同じ切替識別を持つ相手と合わせ、VRIDから自動生成しません。 |
| `spec.vrrp.additionalFailoverVMACs[].macAddress` | **条件付き必須** | string | 必須の6バイトEthernet MACです。同じ切替識別を持つ相手と合わせ、VRIDから自動生成しません。 |
| `spec.vrrp.failoverVMAC.linkLocalAddress` | *任意* | string | 任意の裸のIPv6リンクローカル識別（fe80::/10）で、プレフィックス・ゾーン接尾辞なしです。Linuxヘルパーが実行時に検証し、省略時は通常のリンクローカル処理です。 |
| `spec.vrrp.additionalFailoverVMACs[].linkLocalAddress` | *任意* | string | 任意の裸のIPv6リンクローカル識別（fe80::/10）で、プレフィックス・ゾーン接尾辞なしです。Linuxヘルパーが実行時に検証し、省略時は通常のリンクローカル処理です。 |
| `spec.vrrp.failoverVMAC.withdrawRouterAdvertisement` | *任意* | boolean | 既定falseです。複数VMAC引数形式ではtrueで無効化前にrouter lifetime=0のRAを送り、リンクローカル送信元が必要です。linkLocalAddressなしの単一VMAC旧引数形式は本フラグを渡しません。 |
| `spec.vrrp.additionalFailoverVMACs[].withdrawRouterAdvertisement` | *任意* | boolean | 既定falseです。複数VMAC引数形式ではtrueで無効化前にrouter lifetime=0のRAを送り、リンクローカル送信元が必要です。linkLocalAddressなしの単一VMAC旧引数形式は本フラグを渡しません。 |
| `spec.vrrp.gracefulActivation.readyWhen.state` | *任意* | object | readyWhen内の状態キーと条件のマップで、すべての項目の成立を要求します。空でないstate・all・anyのいずれか1つだけを選びます。equals・in・contains・exists・status・for・maxAgeは共通項に従い、リソース形式のキーは宣言済みリソース・提供フィールドを参照します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/diagnostics.go](https://github.com/imksoo/routerd/blob/main/pkg/config/diagnostics.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/controller/vrrp/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/controller.go) · [pkg/controller/vrrp/backend.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/vrrp/backend.go) · [pkg/controller/dnsresolver/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/dnsresolver/controller.go) · [pkg/render/vrrp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/vrrp.go) · [pkg/render/carp.go](https://github.com/imksoo/routerd/blob/main/pkg/render/carp.go) · [pkg/render/status_source.go](https://github.com/imksoo/routerd/blob/main/pkg/render/status_source.go) · [pkg/render/secrets.go](https://github.com/imksoo/routerd/blob/main/pkg/render/secrets.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `address`, `ifname`, `role`, `hostname`, `virtualRouterID`, `priority`, `failoverVMACs`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/dhcp-lease-sync-ha.yaml)
