---
title: CloudEdge SAM トランスポートレイヤリング
---

# CloudEdge SAM トランスポートレイヤリング

CloudEdge SAM では、アドレスモビリティのルートを WireGuard の `AllowedIPs` に入れません。

![SAMTransportProfile が生成する IPIP 配送面をエンドポイント専用 WireGuard アンダーレイの上に載せ、モバイル /32 は BGP とカーネル FIB が扱うことを示す CloudEdge SAM トランスポート図](/img/diagrams/cloudedge-sam-ipip.png)

WireGuard は `AllowedIPs` を cryptokey routing の状態として扱います。送信パケットは内側の宛先アドレスからピアを選び、受信パケットは復号後の内側ソースアドレスがそのピアに許可されている場合のみ受け入れます。これは WireGuard としては正しい動作ですが、BGP やルートリフレクター、ECMP によって `/32` がピア間を移動する SAM モビリティとは相容れません。

## 推奨レイヤ構成

信頼済みのオンプレミスやホームアンダーレイでは、IPIP または GRE を直接使えます。

```text
physical underlay
  IPIP or GRE tunnel
    SAM overlay packets
```

暗号化が必要な場合は、WireGuard をエンドポイント専用レイヤとして配置し、その上に IPIP または GRE を載せます。

```text
physical underlay
  WireGuard endpoint transport
    IPIP or GRE tunnel
      SAM overlay packets
```

この構成では、WireGuard ピアの `AllowedIPs` には `10.252.0.2/32` のようなルーター間エンドポイントプレフィックスだけを入れます。`192.168.123.10/32` のような SAM プレフィックスは BGP、カーネル FIB、SAM リソースが扱います。

## プロトコル選択

SAM が IPv4 モビリティプレフィックスを運ぶ場合は、まず IPIP を使ってください。現在の `SAMTransportProfile` の例では IPIP がデフォルトの配送面です。トンネルオーバーヘッドが最小で、WireGuard の cryptokey routing と SAM のルートモビリティの分離を維持できるためです。暗号化が必要な場合は、`AllowedIPs` にトランスポートエンドポイントアドレスのみを含む WireGuard インターフェースの上で IPIP を動かしてください。

GRE は、IPv4 以外のプロトコル識別、GRE キー、またはより強力な FreeBSD 相互運用性が必要な場合に使います。

VXLAN、Geneve、GRETAP は、L2 セマンティクスが明示的に必要でない限り避けてください。SAM は選択的な L3 アドレスモビリティなので、L2 オーバーレイヘッダは通常不要です。

FOU と GUE は物理アンダーレイで UDP カプセル化が有用な場合に役立ちますが、WireGuard の内側で使うと、物理ネットワークが見るのは WireGuard の UDP だけなので、物理アンダーレイの負荷分散を改善せずにオーバーヘッドだけが増えます。

## 設定のエルゴノミクス

低レベルリソースは DHCP/IPAM セーフなエンドポイントに対応できなければなりません。`TunnelInterface` は `local` や `remote` をリソースの status から導出でき、運用者が DHCP 管理のアドレスを静的設定に重複して書く必要がないようにすべきです。アンダーレイアドレスが routerd の外で管理されている場合（ライブイメージの DHCP クライアントなど）は、採用済みインターフェースの status を使います。

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: TunnelInterface
metadata:
  name: tun-k8s-rt02
spec:
  mode: ipip
  localFrom:
    resource: Interface/mgmt
    field: primaryIPv4
  remote: 192.168.1.53
  trustedUnderlay: true
```

`primaryIPv4` にはプレフィックス長が含まれる場合があります（例: `192.168.1.32/24`）。トンネルコントローラーは `ip tunnel` が要求するアドレス形式に解決します。`Interface/...` の status ソースを使う場合、リンクコントローラーがインターフェースの status を同じ状態データベースに公開する必要があります。通常の `routerd serve` はこのコントローラーを実行しますが、トンネルの単体テストでは `link` と `tunnel` の両方を含めてください。

## SAMTransportProfile

一般的な SAM トポロジでは `SAMTransportProfile` を使って、以下の不変条件を維持しながら低レベルの `TunnelInterface`、エンドポイント `/32` ルート、`BGPPeer` リソースを生成できます。

- WireGuard の `AllowedIPs` にはトランスポートエンドポイントプレフィックスのみを含む。
- SAM モビリティの `/32` は WireGuard ピアに注入しない。
- IPIP/GRE エンドポイントアドレスは DHCP/IPAM 由来の status フィールドから取得可能。
- MTU と MSS の挙動はトランスポートモードごとに明示的。

`spec.selfNodeRef` はすべてのルーターに必須です。決定論的な `/31` 内部アドレス導出に使う安定した識別子であり、routerd はホスト名や BGP ルーター ID からの推定は行いません。

- `addressingMode: edge-index`（デフォルト）は既存の動作を維持します。トランスポートドメイン内の全ルーターが `spec.topologyNodeRefs` を共有し、routerd がそのリストをソートし、順序なしノードペアにランクを付け、`innerPrefix` からランク順に `/31` を割り当てます。
- `addressingMode: pair-stable` は各ノードペアの安定ハッシュから `/31` スロットを導出するため、リーフノードは `topologyNodeRefs` に全リーフを列挙せずに実際のピア（例: RR ノード）だけを宣言できます。
  - 衝突チェックは現在プロファイルローカル（1 つの `SAMTransportProfile.spec.peers` リスト内）です。
  - `override.localInner` + `override.remoteInner` を指定すると、そのピアをハッシュスロット割り当てから除外し、明示的な `/31` アドレスを予約します。

本番ファブリックでは、衝突確率を考慮して `innerPrefix` のサイズを決めてください。`/24` は `/31` スロットが 128 個しかなく、ハッシュ + mod 方式では中程度のエッジ数で衝突する可能性があります。可能であれば `/20` 以上を使ってください。

`MobilityPool.spec.members` はモビリティの所有権/捕捉/配置の意図を表すものであり、SAM トランスポートの BGP ピアトポロジではありません。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMTransportProfile
metadata:
  name: lab-sam-transport
spec:
  selfNodeRef: pve-rt
  mode: ipip
  innerPrefix: 10.255.1.0/24
  addressingMode: pair-stable
  underlayInterface: wg-hybrid
  localEndpointFrom:
    resource: Interface/wg-hybrid
    field: primaryIPv4
  bgp:
    routerRef: BGPRouter/mobility
    peerASN: 64512
    timersPreset: fast
    # ルートリフレクターのコアルーターでのみ設定してください。
    routeReflectorClient: true
    routeReflectorClusterID: 192.168.1.38
  peers:
    - nodeRef: k8s-rt
      remoteEndpoint: 10.99.0.2
```

明示的なピアオーバーライドで、生成されるリソース名、ピアごとのアンダーレイインターフェース、ローカル/リモートの内部アドレスを固定できます。`localInner` または `remoteInner` のいずれかをオーバーライドする場合は両方を指定し、そのペアが `innerPrefix` 内の有効な `/31` でなければなりません。

## クリーンアップ

プロファイルは self node ごとに `DynamicConfigPart` を 1 つ書きます。ピアの削除はその part を新しい生成リソースセットに置き換えます。プロファイルを削除すると空のアクティブ part になり、生成されたトンネル、BGP ピア、エンドポイントルートが実効設定から消えます。

クリーンアップは通常のライフサイクル GC パスが担当します。desired set は動的 SAM 生成後の実効ビューなので、プロファイルが存在する限り生成されたトランスポートリソースは保持され、プロファイルの出力から消えた後にのみ GC 対象になります。生成された `TunnelInterface`、`BGPPeer`、`IPv4Route` リソースは独自の owner/status レコードを保持し、汎用 GC プランナーとリソース固有の teardown 契約を通じて解体されます。

## 関連 Issue

- #194: SAM モビリティプレフィックスを WireGuard `AllowedIPs` から切り離す。
- #196: `TunnelInterface` エンドポイントをリソースの status から取得可能にする。
- #197: コンパクトな SAM アンダーレイトランスポートプロファイルを追加する。

## 参考資料

- WireGuard の概念と cryptokey routing: https://www.wireguard.com/
- Linux トンネルリンクタイプ: https://man7.org/linux/man-pages/man8/ip-link.8.html
- FreeBSD GRE: https://man.freebsd.org/gre/4
