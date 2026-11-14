# WireGuardInterface

[リソース別の設定](../resources.md#wireguardinterface) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

WireGuardデバイスを作成・設定し、wg setconfでピア設定全体を適用します。アドレス割当と通常の経路設定は別リソースです。

## 指定条件と参照関係

- 実行時の適用にはprivateKeyまたはprivateKeyFileが必要で、どちらもなければPending/PrivateKeyMissingです。排他ではなくprivateKeyを優先しますが、併記したprivateKeyFileも適用時に確認・生成します。
- 静的WireGuardPeer.interfaceはifnameではなく本リソースのmetadata.nameを参照します。取り込むピアはKind/nameで統合し、後の参照元が先の生成ピアを、明示的なWireGuardPeerが同名の生成ピアを置き換えます。
- 登録に基づくdirect SAMPeerGroupはpeersFromの最後に置き、その前にoptionalでないSAMRRSetが必要です。実行時は同じ登録ポリシーの利用可能なRRピアも必要で、direct側が利用不能・非互換でもRRピアを抑止しません。
- listenPortは他の設定済み待受と重複できません。Linuxでは正のポート指定時にINPUTのUDP許可規則も追加します。Upはインターフェース設定を示し、ピアのハンドシェイク成功を保証しません。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardInterface
  metadata:
    name: "<name>"
  spec:
    [ ifname: <string> ]
    [ listenPort: <integer> ]
    [ mtu: <integer> ]
    [ peersFrom:
        -
          [ optional: <boolean> ]
          resource: <string>
    ]
    [ privateKey: <string> ]
    [ privateKeyFile: <string> ]
    [ selfNodeRef: <string> ]
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.ifname` | *任意* | string | OSデバイス名です。既定はmetadata.nameです。現行検証はifname指定時もmetadata.nameの空白・スラッシュ・15文字上限を確認し、ifname自体には同じ検査を行いません。 |
| `spec.selfNodeRef` | *任意* | string | 取り込みから除外する自身のSAMノード識別子です。既定はRouter.metadata.nameで、Kind/name形式の状態参照ではなくnodeRefの値です。 |
| `spec.privateKey` | **条件付き必須** | string | WireGuard秘密鍵の直接指定です。既定は空でprivateKeyFileより優先します。WireGuardのBase64鍵形式を使います。設定検証だけでは鍵の正当性を保証せず、適用時のwgやファイル鍵検証で失敗する場合があります。 |
| `spec.privateKeyFile` | **条件付き必須** | string | ルーター上の鍵ファイルのパスです。実適用時にファイルがなければ0600で生成し、新規親ディレクトリは0700にします。既存ファイルは32バイトの有効なBase64秘密鍵が必要で、空・読取不能なら上書きせず失敗します。環境変数ファイルではありません。 |
| `spec.listenPort` | *任意* | integer; minimum=1; maximum=65535 | UDP待受ポート1～65535です。省略・0ではwg設定にListenPortを出力せず、WireGuard側の選択・保持に任せます。0の場合、routerdは明示的なポート許可を追加しません。 |
| `spec.mtu` | *任意* | integer; minimum=576; maximum=9216 | 任意のMTUで576～9216です。省略・0なら既存値・OS作成時の既定値を使い、この処理ではwg-quickのMTU自動計算を行いません。 |
| `spec.peersFrom` | *任意* | array | 順序付きSAMピア参照元一覧です。既定は空です。optionalでない参照元が見つからないとPending/PeersFromPendingになり、静的ピアがあっても適用を保留します。解決済みの空リストと参照元欠落は区別します。 |
| `spec.peersFrom[].resource` | **条件付き必須** | string | SAMNodeSet/name、SAMPeerGroup/name、SAMEnrollmentPolicy/name、SAMRRSet/nameの参照が必須です。field指定はありません。構文検証後、ピア解決時に参照元を検索します。自身とWireGuard情報のないノードは除外します。 |
| `spec.peersFrom[].optional` | *任意* | boolean | 既定はfalseです。trueなら参照元欠落だけではインターフェースを保留しませんが、不正な参照元のエラー全般を抑止するものではありません。登録directグループには前述のRR維持規則も適用します。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/index.go](https://github.com/imksoo/routerd/blob/main/pkg/config/index.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/render/firewall_holes.go](https://github.com/imksoo/routerd/blob/main/pkg/render/firewall_holes.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `publicKey`, `selfNodeRef`, `listenPort`, `fwmark`, `peerCount`, `hostFirewall`, `peersFrom`, `pendingSources`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/cloudedge-aws-rr-dynamic-leaf.yaml)
