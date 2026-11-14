# TailscaleNode

[リソース別の設定](../resources.md#tailscalenode) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

Linuxのsystemd oneshotまたはFreeBSDのrc.dスクリプトを生成してTailscale登録オプションを適用し、tailscale status --jsonで状態を取得します。Tailscale・Headscaleサーバー側の承認は別操作です。

## 指定条件と参照関係

- authKeyはauthKeyEnv・authKeyFileと排他です。authKeyEnvとauthKeyFileは併用でき、ファイルから読む変数名をauthKeyEnvで選びます。ファイルだけならTS_AUTHKEYです。
- authKeyFileは生の鍵ではなく、TS_AUTHKEY=tskey-...（またはauthKeyEnvで指定した変数）の環境変数代入ファイルです。FreeBSDでは引用符なしの代入を1行に1件書き、対象変数の欠落・重複・空値は失敗します。LinuxではsystemdのEnvironmentFileとして読みます。
- presentのノードはUDP 41641を予約し、他の設定済み待受との競合を拒否します。生成コマンドはtailscale up --timeout=30sです。Applied・Renderedはサービス適用を示し、RunningはBackendState=Runningを観測した場合です。いずれも広告したサブネットの疎通を保証しません。
- FreeBSDのrc.dはrouterdの管理マーカーがない既存tailscaledプロセスの変更を拒否します。Linuxは共有のtailscaled.serviceとrouterd-tailscale-<name>.serviceを使います。state: absentは登録用ユニットを除去する指定で、パッケージのアンインストールやtailnet上のノード削除ではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: TailscaleNode
  metadata:
    name: "<name>"
  spec:
    acceptDNS: true # 任意; boolean
    acceptRoutes: true # 任意; boolean
    advertiseExitNode: true # 任意; boolean
    advertiseRoutes: # 任意; array
      - "<string>" # string
    advertiseTags: # 任意; array
      - "<string>" # string
    authKey: "<string>" # 任意; string
    authKeyEnv: "<string>" # 任意; string
    authKeyFile: "<string>" # 任意; string
    hostname: "<string>" # 任意; string
    loginServer: "<string>" # 任意; string
    shieldsUp: true # 任意; boolean
    ssh: true # 任意; boolean
    state: "" # 任意; string; enum=["", "present", "absent"]
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
| `spec.state` | 既定はpresentです。absentは管理する登録サービスを除去します。この項目でパッケージ除去・管理サーバー上のノード削除は行いません。 |
| `spec.hostname` | 任意のtailscale up --hostname値です。Router.metadata.nameではなく、OSのホスト名も変更しません。省略すると引数を出しません。 |
| `spec.loginServer` | --login-serverに渡す管理サーバーURLです。Headscale等を指定します。省略時はクライアントの設定・既定に任せ、routerd独自のURL既定値・リソース検索はありません。 |
| `spec.authKey` | 任意の登録鍵の直接指定です。authKeyEnv・authKeyFileとは排他です。他の鍵参照もなければ鍵引数を渡さず、既存ログイン状態やクライアント側の登録要件に従います。 |
| `spec.authKeyEnv` | 登録鍵を格納する環境変数名です。鍵そのものではありません。authKeyFileから選ぶ変数名にも使います。Linuxでは生成サービスの環境を使い、routerdプロセスの環境を自動で引き継ぐ指定ではありません。 |
| `spec.authKeyFile` | ルーター上の絶対パスの環境変数ファイルです。読む変数の既定はTS_AUTHKEYです。Linux・FreeBSD共通なら引用符なしのNAME=value形式にします。Secretリソース参照ではありません。 |
| `spec.advertiseExitNode` | 既定falseです。trueなら--advertise-exit-nodeを出力します。管理サーバー上の承認や、他の出口ノードを本機で利用する設定は行いません。 |
| `spec.advertiseRoutes` | IPv4・IPv6 CIDR一覧をカンマ区切りで--advertise-routesへ渡します。空なら引数自体を省略し、空リストを明示送信するものではありません。管理サーバー上の承認は別です。 |
| `spec.advertiseTags` | タグ文字列をカンマ区切りで--advertise-tagsへ渡します。検証は空・空白入りを拒否しますが、tag:接頭辞やサーバーACL権限までは確認しません。空なら引数を省略します。 |
| `spec.acceptRoutes` | --accept-routesの任意の真偽値です。省略時は引数を出さずクライアントに任せます。falseの明示は省略とは異なり、引数として出力します。 |
| `spec.acceptDNS` | --accept-dnsの任意の真偽値です。省略時はtrue・falseのどちらにも固定せず、明示値だけを渡します。routerdのDNSResolver・DNSForwarderを設定する項目ではありません。 |
| `spec.ssh` | 既定falseなら引数を省略し、trueならTailscale SSHを要求する--sshを出します。OSのsshdやrouterdの管理アクセス設定とは別です。 |
| `spec.shieldsUp` | --shields-upの任意の真偽値です。省略時は引数を出さず、true・falseを明示した場合だけ出力します。 |
| `spec.when` | 共通の有効化条件です。省略時は無条件です。state・all・anyとフィールド参照は共通項を参照してください。生成サービスを制御する条件で、tailnetノードの削除ではありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/render/tailscale_systemd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/tailscale_systemd.go) · [pkg/render/freebsd_rcd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/freebsd_rcd.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/tailscale/tailscale.go](https://github.com/imksoo/routerd/blob/main/pkg/tailscale/tailscale.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/tailscale-exit-subnet.yaml)
