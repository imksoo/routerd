# TailscaleNode

[リソース別の設定](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Linuxのsystemd oneshotまたはFreeBSDのrc.dスクリプトを生成してTailscale登録オプションを適用し、tailscale status --jsonで状態を取得します。Tailscale・Headscaleサーバー側の承認は別操作です。

## 指定条件と参照関係

- authKeyはauthKeyEnv・authKeyFileと排他です。authKeyEnvとauthKeyFileは併用でき、ファイルから読む変数名をauthKeyEnvで選びます。ファイルだけならTS_AUTHKEYです。
- authKeyFileは生の鍵ではなく、TS_AUTHKEY=tskey-...（またはauthKeyEnvで指定した変数）の環境変数代入ファイルです。FreeBSDでは引用符なしの代入を1行に1件書き、対象変数の欠落・重複・空値は失敗します。LinuxではsystemdのEnvironmentFileとして読みます。
- presentのノードはUDP 41641を予約し、他の設定済み待受との競合を拒否します。生成コマンドはtailscale up --timeout=30sです。Applied・Renderedはサービス適用を示し、RunningはBackendState=Runningを観測した場合です。いずれも広告したサブネットの疎通を保証しません。
- FreeBSDのrc.dはrouterdの管理マーカーがない既存tailscaledプロセスの変更を拒否します。Linuxは共有のtailscaled.serviceとrouterd-tailscale-<name>.serviceを使います。state: absentは登録用ユニットを除去する指定で、パッケージのアンインストールやtailnet上のノード削除ではありません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    [ acceptDNS: <boolean> ]
    [ acceptRoutes: <boolean> ]
    [ advertiseExitNode: <boolean> ]
    [ advertiseRoutes:
        - <string>
    ]
    [ advertiseTags:
        - <string>
    ]
    [ authKey: <string> ]
    [ authKeyEnv: <string> ]
    [ authKeyFile: <string> ]
    [ hostname: <string> ]
    [ loginServer: <string> ]
    [ shieldsUp: <boolean> ]
    [ ssh: <boolean> ]
    [ state: ("" | "present" | "absent") ]
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
| `spec.state` | *任意* | string; enum=["", "present", "absent"] | 既定はpresentです。absentは管理する登録サービスを除去します。この項目でパッケージ除去・管理サーバー上のノード削除は行いません。 |
| `spec.hostname` | *任意* | string | 任意のtailscale up --hostname値です。Router.metadata.nameではなく、OSのホスト名も変更しません。省略すると引数を出しません。 |
| `spec.loginServer` | *任意* | string | --login-serverに渡す管理サーバーURLです。Headscale等を指定します。省略時はクライアントの設定・既定に任せ、routerd独自のURL既定値・リソース検索はありません。 |
| `spec.authKey` | *任意* | string | 任意の登録鍵の直接指定です。authKeyEnv・authKeyFileとは排他です。他の鍵参照もなければ鍵引数を渡さず、既存ログイン状態やクライアント側の登録要件に従います。 |
| `spec.authKeyEnv` | *任意* | string | 登録鍵を格納する環境変数名です。鍵そのものではありません。authKeyFileから選ぶ変数名にも使います。Linuxでは生成サービスの環境を使い、routerdプロセスの環境を自動で引き継ぐ指定ではありません。 |
| `spec.authKeyFile` | *任意* | string | ルーター上の絶対パスの環境変数ファイルです。読む変数の既定はTS_AUTHKEYです。Linux・FreeBSD共通なら引用符なしのNAME=value形式にします。Secretリソース参照ではありません。 |
| `spec.advertiseExitNode` | *任意* | boolean | 既定falseです。trueなら--advertise-exit-nodeを出力します。管理サーバー上の承認や、他の出口ノードを本機で利用する設定は行いません。 |
| `spec.advertiseRoutes` | *任意* | array | IPv4・IPv6 CIDR一覧をカンマ区切りで--advertise-routesへ渡します。空なら引数自体を省略し、空リストを明示送信するものではありません。管理サーバー上の承認は別です。 |
| `spec.advertiseTags` | *任意* | array | タグ文字列をカンマ区切りで--advertise-tagsへ渡します。検証は空・空白入りを拒否しますが、tag:接頭辞やサーバーACL権限までは確認しません。空なら引数を省略します。 |
| `spec.acceptRoutes` | *任意* | boolean | --accept-routesの任意の真偽値です。省略時は引数を出さずクライアントに任せます。falseの明示は省略とは異なり、引数として出力します。 |
| `spec.acceptDNS` | *任意* | boolean | --accept-dnsの任意の真偽値です。省略時はtrue・falseのどちらにも固定せず、明示値だけを渡します。routerdのDNSResolver・DNSForwarderを設定する項目ではありません。 |
| `spec.ssh` | *任意* | boolean | 既定falseなら引数を省略し、trueならTailscale SSHを要求する--sshを出します。OSのsshdやrouterdの管理アクセス設定とは別です。 |
| `spec.shieldsUp` | *任意* | boolean | --shields-upの任意の真偽値です。省略時は引数を出さず、true・falseを明示した場合だけ出力します。 |
| `spec.when` | *任意* | object | 共通の有効化条件です。省略時は無条件です。state・all・anyとフィールド参照は共通項を参照してください。生成サービスを制御する条件で、tailnetノードの削除ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/tailscale_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/tailscale_systemd.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/tailscale/tailscale.go](https://github.com/imksoo/routerd/blob/main/pkg/tailscale/tailscale.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `tailnetName`, `peerCount`, `advertiseRoutes`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
