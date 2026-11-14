# OverlayPeer

[リソース別の設定](../resources.md#overlaypeer) · [Kind/name · when · dependsOn](../common-fields.md)

`apiVersion: hybrid.routerd.net/v1alpha1` · `metadata.name`

HybridRouteと経路MTU方針の生成に使う、既存の転送手段を記述します。本リソースだけでWireGuard・Tailscale・IPsec・トンネルは構築しません。

## 指定条件と参照関係

- role・nodeID・underlay.typeは必須です。通常検証はwireguard・ipip・gre・fou・gueでunderlay.interfaceを要求し、HybridRouteへの展開時はtailscale・ipsec・routeを含む全タイプで必要です。
- ipip・gre・fou・gueの裸underlay.interfaceは宣言済みTunnelInterfaceの参照が必要です。ただしmodeとの一致は検証しません。WireGuardは外部管理デバイスも許し警告にとどめます。他のデバイスも別途利用可能にしておきます。
- nodeID・remote.nodeID・roleは記述情報で、自動検出・認証・HA選出・トポロジー自動照合はしません。when・dependsOnはありません。

## YAML定義

全フィールドを示す構造参照です。そのまま反映する設定例ではありません。プレースホルダーを実値に置換し、不要な項目を省き、両立する選択肢だけを使います。表示値は型を示す仮値であり、実効的な既定値と指定条件は下の項目説明に記載します。必須の表示はJSON Schemaに基づく親オブジェクト内での指定要否であり、意味上の検証では追加の項目や参照が必要になる場合があります。

```yaml
- apiVersion: hybrid.routerd.net/v1alpha1
  kind: OverlayPeer
  metadata:
    name: "<name>"
  spec:
    nodeID: "<string>" # 必須; string
    pathMTU: # 任意; object
      forceFragmentIPv4: true # 任意; boolean
    remote: # 任意; object
      address: "<string>" # 任意; string
      nodeID: "<string>" # 任意; string
    role: "onprem" # 必須; string; enum=["onprem", "cloud"]
    underlay: # 必須; object
      address: "<string>" # 任意; string
      interface: "<string>" # 任意; string
      type: "wireguard" # 必須; string; enum=["wireguard", "tailscale", "ipsec", "route", "ipip", "gre", "fou", "gue"]
```

## 項目の意味と既定値

| YAML | 項目の意味と既定値 |
| --- | --- |
| `spec.role` | onprem・cloudが必須です。既定値はなく、クラウド資源作成・事業者選択は行いません。 |
| `spec.nodeID` | 空でないローカルノード識別名が必須です。ホストや外部アカウントへの解決は行いません。 |
| `spec.underlay` | 転送手段の記述が必須です。実ネットワークの設定は別途必要です。 |
| `spec.underlay.type` | wireguard・tailscale・ipsec・route・ipip・gre・fou・gueが必須です。経路変換とMTU推定を選び、デーモンは作りません。 |
| `spec.underlay.interface` | 生成IPv4Routeのデバイス・論理名で、経路展開には必須です。トンネル型はTunnelInterface裸名、外部管理なら実OS名を使い、自動検出はしません。 |
| `spec.underlay.address` | 任意の裸IPv4/IPv6記述で、IPとして検証します。現行HybridRouteはこれをゲートウェイ・送信元として使わず、インターフェースへの割当もしません。 |
| `spec.remote` | 任意の相手記述で既定空です。相手登録・交渉は行いません。 |
| `spec.remote.nodeID` | 任意の相手識別名で既定空です。実行時の相手検出・認可には使いません。 |
| `spec.remote.address` | 任意IP文字列です。ipsecの経路変換だけ、解析できるIPv4をgatewayにし、不正・IPv6・空ならデバイス経路です。他型のgatewayには使わず、OverlayPeerの検証もこの値を個別IP検査しません。 |
| `spec.pathMTU` | 任意の転送経路MTU設定で、既定は強制分割要求なしです。暗号化設定ではありません。 |
| `spec.pathMTU.forceFragmentIPv4` | 既定falseで、trueはwireguard・ipip・gre・fou・gueだけです。Linux nftablesで派生転送経路に一致する過大IPv4のDF解除を要求します。トンネル全体のフラグやIPv6分割ではありません。FreeBSDはこのコントローラーを省略し、この経路に同等のDF解除はありません。 |

実装: [pkg/api/specs.go](https://github.com/imksoo/routerd/blob/main/pkg/api/specs.go) · [pkg/config/validate_hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate_hybrid.go) · [pkg/config/validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/validate.go) · [pkg/config/cluster_secret_validate.go](https://github.com/imksoo/routerd/blob/main/pkg/config/cluster_secret_validate.go) · [pkg/hybrid/hybrid.go](https://github.com/imksoo/routerd/blob/main/pkg/hybrid/hybrid.go) · [pkg/render/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/render/mtu.go) · [pkg/render/nftables.go](https://github.com/imksoo/routerd/blob/main/pkg/render/nftables.go) · [pkg/controller/chain/mtu.go](https://github.com/imksoo/routerd/blob/main/pkg/controller/chain/mtu.go)

## 状態値の参照に使えるフィールド

通常の状態値参照の検証で、このKindについて許可するフィールド名です。許可一覧にあるだけでは値の生成・取得を保証しないため、実際の状態を確認してください。YAMLのspecに設定する項目ではなく出力です。

`phase`, `nodeID`, `role`, `underlayType`, `underlayInterface`

[設定例](https://github.com/imksoo/routerd/blob/main/examples/hybrid-l3-fou.yaml)
