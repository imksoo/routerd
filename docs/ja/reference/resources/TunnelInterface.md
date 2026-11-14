# TunnelInterface

[リソース別の設定](../resources.md#tunnelinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

非暗号化のIPv4外側トンネルと、任意の内側IPv4アドレスを生成・再調整します。metadata.nameが実OSトンネル名です。

## 指定条件と参照関係

- 両端点は固定値または共通状態参照の片方が必要でIPv4専用です。動的端点の未解決はoptional trueでも再調整待ちとなり、既存の転送は自動撤去しません。when・dependsOnはありません。
- Linuxはip link・addressとFOU待受で4modeを扱います。FreeBSDはIPIP/gifN・GRE/greNのみでTTL明示を拒否し、address指定時は内側peerAddress必須です。コントローラーはリンクをupにし、managed・enabledの抑止指定はありません。
- 設定address以外のトンネルIPv4は除去し、空なら全除去します。Linuxは組込・他設定で保持するものを除く未設定IPIP/GREと不要FOU待受も削除します。FreeBSDの撤去は記録したTunnelInterface状態と型確認を使います。変更は所有者追跡ではなく設定全体に従います。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: TunnelInterface
  metadata:
    name: "<name>"
  spec:
    address: "<string>" # 任意; string
    encapDport: 1 # 任意; integer; minimum=1; maximum=65535
    encapSport: 1 # 任意; integer; minimum=1; maximum=65535
    key: 0 # 任意; integer; minimum=0; maximum=4294967295
    local: "<string>" # 任意; string
    localFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    mode: "ipip" # 必須; string; enum=["ipip", "gre", "fou", "gue"]
    mtu: 576 # 任意; integer; minimum=576; maximum=9216
    pathMTU: # 任意; object
      forceFragmentIPv4: true # 任意; boolean
    peerAddress: "<string>" # 任意; string
    remote: "<string>" # 任意; string
    remoteFrom: # 任意; object
      field: "<string>" # 任意; string
      optional: true # 任意; boolean
      resource: "<string>" # 必須; string
    trustedUnderlay: true # 必須; boolean
    ttl: 1 # 任意; integer; minimum=1; maximum=255
    underlayInterface: "<string>" # 任意; string
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.mode` | ipip・gre・fou・gueが必須で既定なしです。LinuxのFOU/GUEはUDPカプセル化IPIPリンクです。FreeBSDはipip（gifN）・gre（greN）だけでFOU/GUEを拒否します。 |
| `spec.local` | 外側送信元の裸IPv4です。local・localFromの片方だけが必須で、固定CIDR・ホスト名・IPv6は拒否します。インターフェースアドレスの自動選択はありません。 |
| `spec.localFrom` | 既存Kind/name・提供fieldを明示する共通状態参照で、localと排他です。実行時は状態だけを読み、単一IPv4・CIDRを正規化します。optional trueでも未解決はPending/EndpointSourcePendingで、補完値はありません。 |
| `spec.remote` | 外側宛先の裸IPv4です。remote・remoteFromの片方だけ必須で、固定ホスト名・IPv6・CIDRは拒否します。 |
| `spec.remoteFrom` | remoteと排他の共通状態参照です。field必須・IPv4/CIDR正規化・optionalを待機回避に使わない点はlocalFromと同じです。未解決は再調整を見送りますが、既存トンネルを自動撤去しません。 |
| `spec.address` | 任意の内側IPv4ホスト/CIDRです。コントローラーは他のIPv4を除去するため、省略・空なら既存IPv4も除去します。内側の自動割当はなく、FreeBSDは指定時にpeerAddressも必須です。 |
| `spec.peerAddress` | FreeBSD専用の内側対向IPv4でaddressと対に指定します。片方だけは不可で、CIDRから自動導出せずLinuxでは拒否します。 |
| `spec.underlayInterface` | MTU推定用の任意リソース名（Interface・WireGuardInterface・入れ子TunnelInterface）です。ここでは必須・全体存在検証はなく、不明なら1500を補います。現行コマンドはこのデバイスへ固定せず外側経路はOSが選びます。循環参照は避けてください。 |
| `spec.mtu` | 任意の576～9216をそのまま使います。省略・0は設定上流MTU（不明なら1500）からipip 20・gre 24（keyあり+4）・fou 28・gue 32を引きます。補完既定は1480・1476・1472・1468、key付きGREは1472です。実通信PMTU探索ではありません。 |
| `spec.ttl` | Linuxの外側TTL 1～255で、省略・0は64です。FreeBSDは明示非0を拒否し、内部表示の既定64もifconfig経路には適用しません。 |
| `spec.key` | GRE専用の0～4294967295で、既定0はkeyなしです。他modeの非0は拒否し、FreeBSDはifconfig grekey、Linuxはkey/nokeyです。識別子であり暗号・認証秘密ではありません。 |
| `spec.encapSport` | fou・gueで必須のUDP送信元・ローカル受信ポート1～65535です。既定なしで、同ポートのFOU/GUE待受も作ります。ipip・greの非0は拒否します。 |
| `spec.encapDport` | fou・gueで必須の対向UDP宛先1～65535です。相手受信ポートと合わせます。既定なしでipip・greの非0は拒否します。 |
| `spec.trustedUnderlay` | trueが必須でfalse・省略は拒否します。IPIP・GRE・FOU・GUEは暗号化・相手認証を持たず、この確認指定で追加されるものでもありません。 |
| `spec.pathMTU` | 任意の転送経路設定で、既定はIPv4強制分割なしです。明示トンネルMTUとは別です。 |
| `spec.pathMTU.forceFragmentIPv4` | 既定falseです。Linuxの経路MTU方針で該当派生転送経路の過大IPv4のDFを解除できます。トンネルのignore-df設定でもIPv6機能でもありません。FreeBSDはこのnftables制御を省略し、この経路に同等処理はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/controller/chain/tunnel.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/tunnel.go) · [internal/ipiptunnel/ipip.go](https://github.com/imksoo/routerd/blob/main/internal/ipiptunnel/ipip.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
