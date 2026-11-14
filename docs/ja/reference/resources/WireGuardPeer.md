# WireGuardPeer

[リソース別の設定](../resources.md#wireguardpeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: net.routerd.net/v1alpha1` · `metadata.name`

宣言済みWireGuardInterfaceのピアを定義します。AllowedIPsはWireGuard内の送信先・受信元選択を設定し、このコントローラーは通常のIP経路を自動作成しません。

## 指定条件と参照関係

- ピアは独立サービスではなく、所属インターフェースのwg setconf設定全体の一部として適用します。同じmetadata.nameの明示ピアは、所属interfaceが違ってもpeersFromによる生成ピアを置き換えます。
- presharedKeyとpresharedKeyFileは排他ではなく、直接指定を優先します。直接指定がない場合だけファイルを読み前後空白を除去します。欠落・読取不能・空ファイルは実適用で失敗します。
- Connectedはハンドシェイク時刻を観測したことを示し、その経過時間は別項目です。直近の成功や宛先疎通を保証しません。endpointがあってハンドシェイクがない場合はWaiting/NoHandshakeYetです。

## 設定構文

説明用の構文表記であり、そのままコピーするYAMLではありません。囲みのない項目は親の中で必須、`[ ... ]`は条件に応じて省略できる項目、`<型>`は差し替え値、`(a | b)`は選択肢です。任意の親にも必須の子があります。条件付き必須・排他・実効既定値は下の表を確認し、通常のYAMLはリンク先の設定例を使ってください。

```text
- apiVersion: net.routerd.net/v1alpha1
  kind: WireGuardPeer
  metadata:
    name: "<name>"
  spec:
    allowedIPs:
      - <string>
    [ endpoint: <string> ]
    interface: <string>
    [ persistentKeepalive: <integer> ]
    [ presharedKey: <string> ]
    [ presharedKeyFile: <string> ]
    publicKey: <string>
```

## 項目の意味と既定値

任意の親の必須項目は、親を使う場合に必要です。それ以外の具体的な条件は説明欄に記載します。`[ ... ]`で囲まれた選択肢も、いずれか一方が必要な場合があります。

| YAML | 指定要否 | 型・制約 | 項目の意味と既定値 |
| --- | --- | --- | --- |
| `spec.interface` | **必須** | string | 同じRouter内の既存WireGuardInterfaceのmetadata.nameが必須です。OS上のifnameだけでは参照できません。 |
| `spec.publicKey` | **必須** | string | WireGuardのBase64形式の相手公開鍵が必須です。設定検証は非空だけを確認し、鍵長のデコード検証は行わず、適用時にwgが検証します。 |
| `spec.allowedIPs` | **必須** | array | IPv4・IPv6 CIDRの空でない一覧が必須です。送信時のピア選択と受信時に許可する送信元を設定します。プレフィックス補完・通常経路作成はありません。 |
| `spec.endpoint` | *任意* | string | 任意の相手host:UDPポートです。IPv6は[IPv6]:port形式です。空ならEndpointを省略し、受信パケットからの学習に任せます。routerdの設定検証は構文・DNS疎通を確認せず、適用時にwgが処理します。 |
| `spec.persistentKeepalive` | *任意* | integer; minimum=0; maximum=65535 | キープアライブの送信間隔（秒）で、0～65535の整数です。既定0では継続キープアライブを無効にします。ハンドシェイク成功の期限ではありません。 |
| `spec.presharedKey` | *任意* | string | 任意の追加共通鍵をWireGuardのBase64形式で指定します。既定はなしです。presharedKeyFileと併記してもエラーにはせず、こちらを優先します。 |
| `spec.presharedKeyFile` | *任意* | string | ルーター上で共通鍵そのものを格納するファイルです。環境変数ファイルではありません。欠落時の鍵生成は行わず、presharedKeyが空の場合だけ読みます。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_interface.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_interface.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/wireguard/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/wireguard/wireguard.go) · [pkg/controller/chain/wireguard.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/wireguard.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `latestEndpoint`, `latestHandshake`, `handshakeAgeSeconds`, `transferRxBytes`, `transferTxBytes`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-wireguard.yaml)
