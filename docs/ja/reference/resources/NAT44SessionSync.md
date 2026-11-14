# NAT44SessionSync

[リソース別の設定](../resources.md#nat44sessionsync) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

SSHイベントストリーム、または常時稼働のconntrackd FTFWリンクでLinux conntrack状態を複製します。FreeBSD PFの状態同期ではありません。

## 指定条件と参照関係

- modeの既定はevent-streamです。両方式ともYAML検証でsnatAddresses・natRulesのいずれか1件以上が必要です。event-streamはtargets、conntrackdはconntrackd.interface・localAddress・peerAddressと空のwhenが必須です。
- event-streamは最初にダンプを整合し、その後conntrack -E extendedを読み、通常最大5秒・512操作でまとめてSSH経由で追加・削除します。IPv4 TCP・UDP・ICMPに対応し、状態のないTCP追加は飛ばします。TCPウィンドウ・シーケンス追跡全体は複製しないため、確立済みTCPの完全な引継ぎ方式ではありません。
- conntrackd方式は両系で常時複製し、旧SSH処理を止めます。snatAddresses・natRules・excludeNatRulesは検証されてもconntrackdのフィルターに使いません。描画はTCP・UDP・ICMPを許可し、ループバック・ignoreIPv4を除外します。targets・conntrackCommand・intervalはconntrackdを制御しません。
- Linux conntrackdは追加で、net.netfilter.nf_conntrack_tcp_be_liberalの明示的かつoptionalでないSysctlを要求します。value=1・実効expectedValue=1・runtime有効が必要で、SysctlProfileだけでは満たしません。通信・除外アドレスは裸のIPv4で、相手検出・鍵配布は行いません。
- conntrackdは1リソースにしてください。生成プロセスは/run/routerd/conntrackd.ctl・ロックパスを共有します。現行routerd-vrrp-vmacの昇格連携は/etc/conntrackd/routerd-dslite-sessions.confを明示参照するため、連携にはmetadata.name=dslite-sessionsを想定し、任意名で自動連携するわけではありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: NAT44SessionSync
  metadata:
    name: "<name>"
  spec:
    conntrackCommand: "<string>" # 任意; string
    conntrackd: # 任意; object
      ignoreIPv4: # 任意; array
        - "<string>" # string
      interface: "<string>" # 必須; string
      localAddress: "<string>" # 必須; string
      peerAddress: "<string>" # 必須; string
      port: 1 # 任意; integer; minimum=1; maximum=65535
    excludeNatRules: # 任意; array
      - "<string>" # string
    interval: "<string>" # 任意; string
    mode: "event-stream" # 任意; string; enum=["event-stream", "conntrackd"]
    natRules: # 任意; array
      - "<string>" # string
    snatAddresses: # 任意; array
      - "<string>" # string
    targets: # 任意; array
      - # object
        host: "<string>" # 必須; string
        name: "<string>" # 任意; string
        restoreCommand: # 任意; array
          - "<string>" # string
        sshOptions: # 任意; array
          - "<string>" # string
        user: "<string>" # 任意; string
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
| `spec.mode` | event-stream（既定）・conntrackdです。前者は選択した組をSSH転送し、後者はLinuxのsystemdでFTFW複製を動かします。 |
| `spec.interval` | ポーリング間隔の設定はありません。event-streamは非空を拒否し、conntrackdは受理しても使いません。 |
| `spec.conntrackCommand` | event-streamの実行ファイル名・パスで既定conntrackです。シェルコマンド行でなく1つの実行名として扱い、conntrackdでは無視します。 |
| `spec.snatAddresses` | event-streamで選ぶ裸のIPv4一覧です。既定は空で、natRulesからの解決値と合わせ重複排除・ソートします。本一覧またはnatRulesが1件以上必要ですが、conntrackdの複製フィルターにはなりません。 |
| `spec.natRules` | NAT44Rule/nameまたは裸の名前です。存在でなく参照形式だけを検証します。event-streamは宣言snatAddress、次にstatus.snatAddressを読み、除外以外の未解決が1件でもあればPendingです。translation.address・poolは解決しません。 |
| `spec.excludeNatRules` | natRules解決時に飛ばすNAT44Rule/nameまたは裸の名前です。既定は空で、明示snatAddressesの除外・conntrackdの絞り込みにはなりません。 |
| `spec.targets` | event-streamで1件以上必須です。各宛先へSSHで復元操作を送り、conntrackdは使いません。 |
| `spec.targets[].name` | 任意の説明・状態表示名で既定は空です。実際の宛先はhostで決まります。 |
| `spec.targets[].host` | 必須の空でないSSHホスト名・SSH別名です。相手ユーザーにconntrack操作権限が必要で、認証情報は自動準備しません。 |
| `spec.targets[].user` | 任意の相手SSHユーザーです。既定の空はSSH設定に従い、非空はuser@hostにします。 |
| `spec.targets[].sshOptions` | SSH引数の一覧です。明示上書きがなければBatchMode=yes・ConnectTimeout=10を追加します。各要素は1引数で改行は禁止です。操作には実装上の2分タイムアウトもあります。 |
| `spec.targets[].restoreCommand` | 相手conntrack実行argvで、既定は[conntrack]です。[sudo, -n, conntrack]のように権限コマンドを別引数で指定できます。定期的な完全状態転送コマンドではありません。 |
| `spec.conntrackd` | conntrackd方式で必須、event-streamでは使いません。両HAノードで常時稼働し、複製リンクを許可する必要があります。 |
| `spec.conntrackd.interface` | 必須の複製インターフェースです。設定別名があれば解決し、なければOS名として使います。通常の存在参照検証はなく、描画では空白を拒否します。 |
| `spec.conntrackd.localAddress` | 必須の裸の自IPv4複製アドレスです。ホストへの事前設定が必要で、アドレス割当ではありません。 |
| `spec.conntrackd.peerAddress` | 必須の裸の相手IPv4複製先です。相手検出・マルチキャスト参加は設定しません。 |
| `spec.conntrackd.port` | UDP複製ポートです。省略・0は描画時3780にします。正数は1～65535です。Go実行は0を既定として受け付けますがJSON Schemaは最小1です。 |
| `spec.conntrackd.ignoreIPv4` | 任意の裸のIPv4除外一覧です。YAMLの既定は空ですが、描画は常に127.0.0.1とIPv6 ::1を除外し、IPv4を重複排除・ソートして不正値を拒否します。 |
| `spec.when` | event-stream用の共通有効化条件で、既定は無条件です。SSH処理を制御できます。conntrackdは待機側も複製するため完全に空である必要があります。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_firewall.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_firewall.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/nat44sessionsync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/nat44sessionsync.go) · [pkg/controller/chain/filesync.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/filesync.go) · [pkg/controller/chain/system.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/system.go) · [pkg/controller/chain/chain.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/chain.go) · [pkg/render/conntrackd.go](https://github.com/imksoo/routerd/blob/main/pkg/render/conntrackd.go) · [cmd/routerd-vrrp-vmac/main.go](https://github.com/imksoo/routerd/blob/main/cmd/routerd-vrrp-vmac/main.go)
