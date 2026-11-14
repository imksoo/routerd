# PPPoESession

[リソース別の設定](../resources.md#pppoesession) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Linuxのpppd・FreeBSDのmpd5を使うrouterd-pppoe-clientを監視実行し、交渉したIPv4セッション状態を公開します。一部の旧項目はFreeBSDの手動render出力だけに使われ、主デーモン経路には効きません。

## 指定条件と参照関係

- interfaceは同じRouterの基本Interfaceを参照し、PPPoESession・トンネル等を下位インターフェースには使えません。enabled:falseでもusernameとpassword・passwordFileのどちらか一方が必須です。
- 実動作の監視処理はauthMethod・mtu/mru・serviceName/acName・lcpEchoInterval/lcpEchoFailureをデーモンへ渡しますが、ifname・managed・persist・ipv6・usePeerDNS・lcpInterval/lcpFailureは渡しません。別経路のFreeBSD手動render出力から実動作を推測しないでください。
- 主デーモンの既定値は両OSともMTU・MRU 1454、LCP echo 30秒・4回です。別のFreeBSD手動mpd5生成では1492、旧LCP 20秒・3回です。設定計画やConnectedは実際の上流疎通を保証しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: PPPoESession
  metadata:
    name: "<name>"
  spec:
    [ acName: <string> ]
    [ authMethod: ("chap" | "pap" | "both") ]
    [ defaultRoute: <boolean> ]
    [ enabled: <boolean> ]
    [ ifname: <string> ]
    interface: <string>
    [ ipv6: <boolean> ]
    [ lcpEchoFailure: <integer> ]
    [ lcpEchoInterval: <integer> ]
    [ lcpFailure: <integer> ]
    [ lcpInterval: <integer> ]
    [ managed: <boolean> ]
    [ mru: <integer> ]
    [ mtu: <integer> ]
    [ password: <string> ]
    [ passwordFile: <string> ]
    [ persist: <boolean> ]
    [ secretEncoding: ("plain") ]
    [ serviceName: <string> ]
    [ usePeerDNS: <boolean> ]
    username: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.interface` | **必須** | string | 必須の下位基本Interfaceのmetadata.nameです。物理OS名へ解決し、状態値参照ではありません。 |
| `spec.ifname` | *任意* | string | 希望するPPP名で、15文字以内・空白とスラッシュ不可です。省略時はppp-<name>が15文字以内である必要があります。現行の主監視処理はこの項目を渡さずデーモンのDefaultIfNameを使い、FreeBSDの手動renderのみ値を使います。実動作で独自名に依存しないでください。 |
| `spec.enabled` | *任意* | boolean | 既定trueです。falseは起動を抑止し監視するセッションを停止してDisabledを表示しますが、設定検証は省略しません。 |
| `spec.authMethod` | *任意* | string; enum=["chap", "pap", "both"] | 既定chapで、pap・bothも指定できます。実際のpppd・mpd5設定で許可するPAP・CHAPを制御します。 |
| `spec.username` | **必須** | string | 必須のPPP認証ユーザー名で、自動の既定値はありません。 |
| `spec.password` | **条件付き必須** | string | 平文PPPパスワードです。passwordFileがなければ必須で、passwordFileとは排他です。 |
| `spec.passwordFile` | **条件付き必須** | string | ルーター上の生パスワードファイルです。passwordがなければ必須で、起動時に読んで前後空白を除去します。欠落・読取不能・空なら失敗します。環境変数代入ファイルではありません。 |
| `spec.mtu` | *任意* | integer; minimum=576; maximum=1500 | 任意の576～1500です。省略・0なら主デーモンは1454、FreeBSD手動renderは1492です。PPPで1500を使うには対応する下位網が必要で、設定検証では保証しません。 |
| `spec.mru` | *任意* | integer; minimum=576; maximum=1500 | 受信上限の576～1500です。省略・0なら主デーモンは1454、FreeBSD手動renderは1492です。 |
| `spec.serviceName` | *任意* | string | 任意のPPPoEサービス選択名です。空なら明示指定せず、改行は禁止です。 |
| `spec.acName` | *任意* | string | 任意のアクセスコンセントレーター選択名です。空なら明示指定せず、改行は禁止です。 |
| `spec.defaultRoute` | *任意* | boolean | 既定falseです。trueでは主経路のクリーンアップが観測済みPPPデバイスの既存デフォルト経路を保持しますが、デーモン設定は経路を追加しません。FreeBSD手動renderは追加指示を出します。実動作で自動追加が必要ならIPv4Routeを明示してください。 |
| `spec.usePeerDNS` | *任意* | boolean | 既定falseですが、主デーモン経路は参照しません。LinuxPeerは常にusepeerdnsを出し、取得DNSはPPP状態に公開します。この項目でrouterdのDNS転送・resolv.confへの適用は制御できません。 |
| `spec.persist` | *任意* | boolean | 任意の真偽値ですが、現行の監視デーモン設定では使いません。省略時にリソース独自の再試行ポリシーが決まるものではなく、監視処理のライフサイクルは別です。 |
| `spec.lcpInterval` | *任意* | integer; minimum=0 | 旧形式の0以上の間隔で、FreeBSD手動renderだけが使い、省略・0は20秒です。主デーモンは無視し、lcpEchoIntervalを使用します。 |
| `spec.lcpFailure` | *任意* | integer; minimum=0 | 旧形式の0以上の失敗回数で、FreeBSD手動renderだけが使い、省略・0は3です。主デーモンは無視し、lcpEchoFailureを使用します。 |
| `spec.lcpEchoInterval` | *任意* | integer; minimum=0 | 0以上のLCP echo間隔（秒）です。省略・0は主デーモンの既定30秒で、echo無効化ではありません。FreeBSD手動renderはこの項目を使いません。 |
| `spec.lcpEchoFailure` | *任意* | integer; minimum=0 | 0以上のLCP echo失敗回数です。省略・0は主デーモンの既定4です。Linuxはlcp-echo-failureに渡し、FreeBSDデーモンはkeep-aliveの間隔と間隔×回数を出力します。 |
| `spec.ipv6` | *任意* | boolean | 既定falseです。現行の監視PPPデーモンには渡さず、その生成設定にもIPv6交渉オプションを出しません。このフラグでPPP上のIPv6を有効化できるとは扱わないでください。 |
| `spec.managed` | *任意* | boolean | 既定falseですが、主監視処理はこの値に関係なくenabledなセッションを起動します。FreeBSD手動renderだけでは、trueの有効セッションをmpd5の既定load一覧に追加します。実動作の観測専用指定ではありません。 |
| `spec.secretEncoding` | *任意* | string; enum=["plain"] | 省略またはplainのみ指定できます。password・passwordFileのBase64デコードや暗号化は行いません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_wan.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_wan.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/controller/chain/phase_status.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/phase_status.go) · [pkg/controller/chain/ipv4_main_routes.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/ipv4_main_routes.go) · [pkg/controller/pppoesession/controller.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/pppoesession/controller.go) · [cmd/routerd-pppoe-client/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-pppoe-client/main.go) · [pkg/pppoeclient/client.go](https://github.com/imksoo/routerd/blob/main/pkg/pppoeclient/client.go) · [pkg/render/freebsd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd.go) · [cmd/routerd/cmd_render.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd/cmd_render.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `interface`, `device`, `currentAddress`, `peerAddress`, `gateway`, `dnsServers`, `connectedAt`, `dryRun`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/example-pppoe-ipv4-nat.yaml)
