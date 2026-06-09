---
title: 選択的アドレス移動性
---

# 選択的アドレス移動性

これは L2 延伸の仕組みではありません。routerd CloudEdge はイーサネットセグメントをパブリッククラウドへ延伸しません。パブリッククラウドのファブリックは運用者が制御できるブロードキャストドメインを提供せず、経路とアドレス所有権のモデルもプロバイダーごとに異なります。

選択的アドレス移動性は、選択した IPv4 `/32` アドレスだけを片側で捕捉し、routerd 間のオーバーレイ経由で所有者側へパケットを配送する抽象化です。TCP/IP の送信元アドレスと宛先アドレスはそのまま保持されます。ファイアウォールと NAT は routerd の別レイヤーであり、モビリティリソースのフィールドではありません。

![MobilityPool と SAMTransportProfile を記述面とし、生成された IPIP 配送、BGP ピア、ECMP ネクストホップ、セカンダリ IP または proxy ARP による捕捉を示す SAM トランスポート図](/img/diagrams/cloudedge-sam-ipip.png)

## リソースモデル

CloudEdge モビリティコントロールプレーンにおいて、運用者が記述するモビリティインテントは `MobilityPool` だけです。論理 IPv4 プール、読み取る EventGroup、メンバーノードとサイト、BGP 配送モード、捕捉ポリシー、プロバイダートラップの配置を宣言します。メンバーリストは BGP ピアリストと同様に扱います。各ノードは他のノードの識別情報・サイト・ロール・配置を知る必要がありますが、リモートノードの NIC ID、プロバイダーリソース名、サブネット ID などの実装詳細を知る必要はありません。

目標とする設定の構造は次のとおりです。

- **自サイト**は、捕捉とプロバイダーディスカバリーの詳細まで完全に宣言する。
- **リモートサイト**は、識別情報のみのメンバー（`nodeRef`、`site`、`role`、任意で `placement` / `maintenance`）として宣言する。
- 規模の大きなファブリックでは、共有する識別情報のみのメンバーリストを `MobilityMemberSet` に置き、`MobilityPool.spec.membersFrom` で取り込む。
- ローカルのクラウド捕捉に関する再利用可能な詳細は `profiles.cloudCaptures` に置く。
- 秘密でないノードローカルな値は `spec.values` に置き、`capture.targetFrom` と `ownershipDiscovery.subnetRefFrom` で参照する。

`MobilityMemberSet` はモビリティ側における `SAMPeerGroup` に相当するリソースです。含めるのは共有されるメンバー識別情報フィールド（`nodeRef`、`site`、`role`、任意で `placement` / `maintenance`）だけです。`capture`、`ownershipDiscovery`、`profileRef`、配送関連フィールド、静的所有アドレスは意図的に含めません。これらはそれを必要とするノードの `MobilityPool` 側にローカル設定として残します。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityMemberSet
metadata: { name: svnet1-members }
spec:
  members:
    - nodeRef: pve-rt01
      site: pve01
      role: onprem
    - nodeRef: pve-rt02
      site: pve02
      role: onprem
    - nodeRef: rr01
      site: backbone
      role: cloud
```

プールは 1 つ以上のメンバーセットを取り込めます。取り込んだメンバーを先に追加し、ローカルの `spec.members` を `nodeRef` 単位で後から上書きします。そのためリーフは、共有トポロジーをメンバーセットから受け取りつつ、自分自身の捕捉/ディスカバリーの詳細だけをローカルに記述できます。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: { name: svnet1 }
spec:
  prefix: 10.88.60.0/24
  groupRef: svnet1
  membersFrom:
    - resource: MobilityMemberSet/svnet1-members
  members:
    - nodeRef: pve-rt01
      site: pve01
      role: onprem
      capture:
        type: proxy-arp
        interface: vmbr0
      ownershipDiscovery:
        mode: onprem-l2
        sources:
          - type: pve-svnet
            bridge: vmbr0
```

必須の `membersFrom` ソースがまだ届いていない場合、プールは `Pending` になります。ブートストラップ中に部分的なローカルメンバーリストで動作させてよい場合にのみ `optional: true` を指定してください。

たとえば AWS ルーター上の設定は次のようになります。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: MobilityPool
metadata: { name: lab-same-subnet }
spec:
  prefix: 10.0.0.0/24
  groupRef: cloudedge
  values:
    self.region: ap-northeast-1
    self.subnetRef: subnet-0123456789abcdef0
  profiles:
    cloudCaptures:
      aws-self:
        capture:
          type: provider-secondary-ip
          providerRef: aws-lab
          providerMode: eni-secondary-ip
          nicRef: eni-0123456789abcdef0
          configureOSAddress: false
          targetFrom:
            region: self.region
        ownershipDiscovery:
          mode: provider-private-ip
          scanInterval: 60s
          subnetRefFrom: self.subnetRef
  members:
    - nodeRef: onprem-router
      site: onprem
      role: onprem
    - nodeRef: cloud-router
      site: aws
      role: cloud
      profileRef: aws-self
      placement:
        group: aws-edge
        priority: 10
      maintenance:
        drain: false
    - nodeRef: azure-router
      site: azure
      role: cloud
      placement:
        group: azure-edge
        priority: 10
    - nodeRef: oci-router
      site: oci
      role: cloud
      placement:
        group: oci-edge
        priority: 10
  deliveryPolicy:
    mode: bgp
  capturePolicy:
    mode: all-non-owner-sites
```

オンプレミスノードでは逆に、オンプレミスメンバーが完全な自己宣言になります。通常は `staticOwnedAddresses` と、`activeWhen.type` でゲートした `proxy-arp` 捕捉を持ちます。サイト内にルーターが 1 台なら `single-router`、HA ペアで VRRP マスター状態で捕捉をゲートする場合は `vrrp-master` を使います。クラウドメンバーは識別情報のみのままです。どの方向でも同じ原則が成り立ちます。ローカルルーターがローカルの実装詳細を保持し、リモートメンバーはピア識別情報だけを持ちます。

routerd はフェデレーションやプロバイダーディスカバリーで得た観測事実から、所有中の `/32` パスを BGP で広告します。運用者は `MobilityPool` だけを編集してコントロールプレーンを宣言的に保ちます。アドレスごとの広告やプロバイダートラップのアクションプランはコントローラーが導出します。

同一プロバイダーのクラウドルーター保守では、`members[].placement.group` 内の `drain` されていないメンバーから `priority` 順、次に `nodeRef` 順でアクティブ捕捉メンバーを選出します。`members[].maintenance.drain: true` にすると、そのメンバーはアクティブ選出から外れます。アクティブメンバーだけがプロバイダートラップアクションを発行し、全メンバーが BGP スタンバイパスを広告できます。配置の導出を決定的に保つため、プール内の全ノードに同じ `MobilityPool` 設定を配布してください。

### 目標フィールドリファレンス

`spec.values`
: このノードの設定を正規化する際に使う、秘密でないローカル値です。リージョン名、コンパートメント ID、リソースグループ名、サブネット ID、NIC 名などの識別子に使います。認証情報、トークン、秘密鍵、アカウントシークレットは置かないでください。

`spec.profiles.cloudCaptures.<name>.capture`
: ローカルのクラウド `provider-secondary-ip` 捕捉に使う再利用可能な既定値です。メンバーは `members[].profileRef` で参照できます。メンバー側の明示フィールドがプロファイルより優先されます。

`spec.profiles.cloudCaptures.<name>.ownershipDiscovery`
: プロバイダーのプライベート IP 棚卸しスキャン用の再利用可能な既定値です。`ownershipDiscovery.providerRef` が省略された場合、有効な `capture.providerRef` を継承します。

`members[].profileRef`
: 名前付きクラウド捕捉プロファイルをメンバーに適用します。通常はローカルの自メンバーにだけ使い、リモートメンバーでは省略します。

`members[].capture.targetFrom`
: 生成されるプロバイダーアクションのターゲットキーを `spec.values` のキーに対応づけます。同じキーが `capture.target` にもある場合は、明示的な `capture.target` が優先されます。

`members[].ownershipDiscovery.subnetRefFrom`
: `ownershipDiscovery.subnetRef` が空の場合に `spec.values` から値を解決します。

`members[].placement`
: 決定的なアクティブ/スタンバイ捕捉の配置を宣言します。識別情報のみのリモートクラウドメンバーにも配置は有効です。他のノードが同一サイト内のどのメンバーがアクティブかを同じように判断するためです。

古い「リモート完全インライン」方式、つまり各ノードがリモートメンバーのプロバイダー詳細まで繰り返し記述する形式は、プレリリース期間の互換性のために引き続き受け付けます。ただし非推奨です。リモートメンバーがローカルの捕捉/ディスカバリー詳細を持っている場合、`routerctl validate`・plan・apply は警告を表示します。将来のプレリリースでは、リモートメンバーを識別情報のみにすることを必須とする可能性があります。

## トランスポートプロファイル

`SAMTransportProfile` は BGP モード SAM の上位トランスポートプロファイルです。モビリティパスを運ぶピアごとの `TunnelInterface`、エンドポイント `/32` の `IPv4Route`、`BGPPeer` を導出します。現在の CloudEdge 構成例では IPIP を SAM 配送面のデフォルトとしています。WireGuard が存在する場合は暗号化アンダーレイとしてのみ使用します。生成または手書きの WireGuard ピアの `AllowedIPs` はトランスポートエンドポイントプレフィックスに限定し、モビリティ `/32` は含めないでください。

各ルーターは `spec.selfNodeRef` を明示する必要があります。routerd はホスト名や BGP ルーター ID からローカルノードの識別情報を推測しません。

`spec.addressingMode` は `/31` スロットの導出方法を制御します。

- `edge-index`（デフォルト）: ピアが複数あるプロファイルでは、トランスポートドメイン内の全ルーターで同じ `spec.topologyNodeRefs` リストを宣言する必要があります。コントローラーはこの共有ノードリストをソートし、順序なしノードペアの順位から `spec.innerPrefix` 内の `/31` を割り当てます。
- `pair-stable`: 各ピアエッジが安定ハッシュからスロットを導出するため、リーフ/ルータープロファイルはグローバルな `topologyNodeRefs` を省略できます。衝突検出は現在プロファイルローカル（1 つのプロファイルの `spec.peers` リスト内）です。衝突が発生した場合は、該当ピアの `override.localInner` と `override.remoteInner` の両方を設定して明示的にアドレスを予約してください。

本番ファブリックでは可能な限り `/20` 以上の `innerPrefix` を推奨します。`/24`（128 個の `/31` スロット）のように小さなプールはハッシュ＋剰余割り当てで衝突しやすくなります。

`SAMPeerGroup` は再利用可能なトランスポートピアをまとめるリソースです。プロファイルは `spec.peersFrom` に 1 つ以上の `SAMPeerGroup/<name>` 参照を指定できます。コントローラーはリコンサイル時にグループのピアを先に追加し、その後にプロファイル直下の `spec.peers` を重ねます。同じ `nodeRef` が両方にある場合は `spec.peers` が優先されるため、リーフ側に静的なブートストラップ用ピアやローカルのオーバーライドを残せます。必須の `peersFrom` グループが未到着の場合、プロファイルは `Pending` になります。`optional: true` のソースは到着するまで無視されます。

スパイン/ルートリフレクター側のプロファイルでは `spec.publishPeerGroup: true` を指定できます。この場合、routerd はプロファイルの `selfNodeRef` と具体的なローカルエンドポイントから `SAMPeerGroup` を生成し、DynamicConfigPart として公開します。`localEndpointFrom` は公開前に解決されるため、リーフには直接使える `remoteEndpoint` が配布されます。

`publishPeerGroup: true` を持つノードで `routerd serve` が動作している場合、routerd は公開済みピアグループをトランスポートネットワーク上の TCP ポート `19652`（`GET /v1/peer-groups`）でも返します。リーフ側で必須の `peersFrom` グループが見つからない場合、`spec.underlayInterface` から到達できる WireGuard ピアへ問い合わせ、名前が一致するグループを `peer-group-sync/<group-name>` としてローカルに保存します。この DynamicConfigPart は通常の TTL で期限切れになり、パブリッシャーが消えた場合はリーフが `Pending` に戻ります。

MobilityPool のメンバーシップについても同様に、ルートリフレクター側の正規プールに `spec.publishMemberSet: true` を指定できます。routerd はローカル専用のメンバーフィールドを取り除き、ソース `mobility-member-set/<pool>` の `MobilityMemberSet` DynamicConfigPart を公開し、同じ TCP ポートの `GET /v1/member-sets` で返します。リーフ側で必須の `membersFrom` ソースが見つからない場合、取得したセットを `member-set-sync/<set-name>` として保存します。

```yaml
apiVersion: mobility.routerd.net/v1alpha1
kind: SAMTransportProfile
metadata: { name: cloudedge-transport }
spec:
  selfNodeRef: aws-router-a
  mode: ipip
  encryption: wireguard
  innerPrefix: 10.255.0.0/24
  topologyNodeRefs:
    - onprem-router
    - aws-router-a
    - azure-router
  underlayInterface: wg-hybrid
  localEndpointFrom:
    resource: Interface/wg-hybrid
    field: primaryIPv4
  bgp:
    routerRef: BGPRouter/mobility
    peerASN: 64512
    timersPreset: fast
  peers:
    - nodeRef: onprem-router
      remoteEndpoint: 10.252.0.1
```

コアルーターでは `spec.bgp.routeReflectorClient` と `spec.bgp.routeReflectorClusterID` を設定できます。これらは生成される各 `BGPPeer` にコピーされます。エッジルーターでは未指定のまま通常の iBGP セッションとして使えます。

ピアをプロファイルから削除すると、そのプロファイルが生成した `DynamicConfigPart` は新しいリソースセットで置き換えられます。プロファイル自体を削除した場合は、古いパートが空のアクティブパートで置き換えられ、実効設定から生成済みのトンネル、BGP ピア、エンドポイント経路が消えます。生成されたリソースの具体的な後片付けは、通常のオーナー参照 GC とリソース固有のティアダウンに委ねます。

## 低レベル互換リソース

`AddressMobilityDomain` と `RemoteAddressClaim` は SAM の低レベル表現です。既存の手書き SAM 設定はプレリリース期間の互換性のために引き続きサポートしますが、CloudEdge モビリティの主要な記述面ではありません。アドレス所有権と捕捉のインテントには `MobilityPool` を、トランスポート/BGP の生成には `SAMTransportProfile` を使ってください。

`AddressMobilityDomain` は選択されたアドレスが移動しうる IPv4 プレフィックスを定義します。

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: AddressMobilityDomain
metadata: { name: lab-same-subnet }
spec:
  prefix: 10.0.0.0/24
  mode: selective-address
  peerRef: cloud-main
```

`RemoteAddressClaim` は 1 つのモバイル `/32`、捕捉方法、配送方法を宣言します。

```yaml
apiVersion: hybrid.routerd.net/v1alpha1
kind: RemoteAddressClaim
metadata: { name: onprem-vm-10-0-0-9 }
spec:
  domainRef: lab-same-subnet
  address: 10.0.0.9/32
  ownerSide: onprem
  capture:
    type: provider-secondary-ip
    providerRef: azure-lab
    providerMode: nic-secondary-ip
    nicRef: /subscriptions/.../networkInterfaces/routerd-nic
    configureOSAddress: false
  delivery:
    peerRef: cloud-main
    mode: route
    tunnelInterface: wg-hybrid
```

`AddressMobilityDomain.spec.peerRef` はドメインレベルのデフォルト/ドキュメント用ピアで、グルーピングメタデータとして扱います。MVP のデータプレーンが実際の配送ピアとして使うのは `RemoteAddressClaim.spec.delivery.peerRef` であり、各クレームに必須です。

`CloudProviderProfile` はプロバイダーの機能と外部ツールの認証方法を記述します。モビリティプランナーはプロバイダー API を直接呼びません。クラウド捕捉では `assign-secondary-ip` や `ensure-forwarding-enabled` のドライラン `ActionPlan` レコードを生成し、別のプロバイダーアクション実行パスが `ProviderActionPolicy` で明示的に許可された場合にのみインポート/実行します。

`OverlayPeer` はレガシーの経路低レベル化設定におけるリモート routerd ピアとアンダーレイを表します。`HybridRoute` は通常の L3 リモートプレフィックスルーティング用に残ります。新しい CloudEdge モビリティ設定では `OverlayPeer` でモビリティ `/32` を運ばず、`SAMTransportProfile` による BGP 配送を使ってください。

## 捕捉と配送

サポートする捕捉タイプは次のとおりです。

| タイプ | 意味 |
| --- | --- |
| `provider-secondary-ip` | プロバイダーが所有するセカンダリアドレスオブジェクト等を通じて、クラウドファブリックが `/32` を捕捉します。 |
| `proxy-arp` | サイトルーターが選択されたアドレスに対してローカルに ARP 応答します。 |

MVP のバリデーションで拒否される予約済み捕捉タイプは次のとおりです。

| タイプ | 状態 |
| --- | --- |
| `static-host-route` | 将来のデータプレーン設計用に予約されています。 |
| `garp` | 将来のデータプレーン設計用に予約されています。 |

`MobilityPool` の配送モードは BGP です。所有アドレスは IPv4 ユニキャスト `/32` パスとして広告され、非所有者は BGP ベストパスをローカル FIB にインポートして、選択されたオーバーレイのネクストホップへ届けます。`deliveryPolicy.mode: bgp` がデフォルトであり、現在の MobilityPool コントロールプレーンで唯一サポートされる配送モードです。古い経路低レベル化 SAM 配送は、手書きの `RemoteAddressClaim` 互換設定のためにのみ残っています。

`members[].capture.target` は、生成されるプロバイダー `ActionPlan.target` の値にコピーされる、秘密でないプロバイダーターゲットヒントです。リージョン、コンパートメント ID、リソースグループ、NIC 名、IP 設定名などの識別子だけを置き、認証情報、トークン、秘密鍵はプロバイダー認証の仕組み側に置きます。

BGP モードのオンプレミス `proxy-arp` 捕捉では、`members[].capture.sourceAddress` で捕捉インターフェース上のルーターのローカル送信元アドレスを任意に宣言できます。routerd はこれを `IPv4StaticAddress` `/32` に低レベル化し、捕捉プレフィックス経路の優先送信元として使います。捕捉インターフェースに IPv4 アドレスがない場合に便利です。Linux のローカル同一サブネットクライアント向け ARP が、関係のない管理用アドレスにフォールバックする代わりに、モビリティプレフィックス内のアドレスを使うようになります。

その送信元アドレスが DHCP/IPAM など別のライフサイクルマネージャーに所有されている場合は、代わりに `members[].capture.sourceAddressFrom` を使います。たとえば `resource: DHCPv4Client/svnet1-source` と `field: currentAddress` を指定すると、リースされたアドレスを捕捉プレフィックス経路の優先送信元として使いつつ、`IPv4StaticAddress` への低レベル化を行わないため、routerd が同じアドレスの所有権を二重に持つことを避けられます。

`members[].capture.excludeAddresses` は、モビリティプレフィックス内にありながら、拡張セグメントを超えて proxy-ARP 捕捉してはならないローカル専用アドレスに使います。たとえば PVE Simple SDN では各ホストが `192.168.123.1/32` のような同一のローカルゲートウェイアドレスを持つことがあります。これを除外すると、そのアドレスに対する BGP proxy-ARP クレームの生成が抑止され、捕捉プレフィックス経路が分割されて、Linux がローカルゲートウェイの ARP を SAM 捕捉パスに送らなくなります。

SAM は透過的な DHCP ブロードキャスト延伸を提供しません。DHCP の所有権はローカルファブリック、VPC/VNet/VCN、または PVE IPAM に任せてください。`sourceAddressFrom` で使う `DHCPv4Client` は、捕捉インターフェースの送信元アドレスを学習するためだけに存在する場合、通常 `useRoutes: false` と `useDNS: false` を設定します。DHCP リース観測は所有権ディスカバリーに参加できますが、IPAM ソースが routerd 外にある場合は `arp-observer`、`on-demand-arp`、または PVE svnet 観測と組み合わせてください。

`on-demand-arp` はモビリティプレフィックスの保守的な能動スイープも行います。ソースの `scanInterval` ごとに 1 つの ARP ターゲットを探査し、オンデマンドトリガーの探査と同じ `probeTimeout`、`probeRetries`、`probeCooldown`、`sourceAddressFrom` 設定を使います。これにより、すでに起動済みで通信していない L2 クライアントも、所有者側から手動で `arping` や ping を打たなくても観測済みクライアントとして収束できます。広いプレフィックスでは `scanInterval` を控えめにしてください。`/24` のラボ検証では `1s` 程度にすると、1 秒 1 探査の範囲で素早く確認できます。

Linux での `proxy-arp` 捕捉では、routerd は以下を行います。

- 通常の sysctl コントローラーで `net.ipv4.conf.<capture-interface>.proxy_arp=1` を有効化する。
- `ip neigh add proxy <address> dev <capture-interface>` 相当のプロキシネイバーエントリを netlink で追加する。
- 通常の sysctl コントローラーで `net.ipv4.ip_forward=1` を有効化する。

`provider-secondary-ip` では、プロバイダーファブリックがアドレス捕捉を担当します。`configureOSAddress: false` の場合、routerd はモバイルアドレスをローカル OS のアドレスとして設定しません。Linux では cloud-init、netplan、ゲストエージェントなどがそのアドレスを戻した場合でも、その特定のアドレスだけをローカルインターフェースから削除します。そのうえで IPv4 フォワーディングを確保し、オーバーレイへの `/32` 配送経路は BGP ベストパスのインポートから得ます。捕捉を削除しても routerd はアドレスを戻しません。ゲスト OS へのアドレス割り当ては routerd が所有していないためです。

ステータスではこれを `captureOSAddressAbsence` として報告します。`enforced: true` は、routerd が捕捉されたアドレスをローカル OS インターフェースに存在させないことを継続的に適用していることを示す監査フラグです。`lastReconcileRemoved: true` は、直近のリコンサイルで実際にそのアドレスを削除したことを示します。アドレスがすでに存在しない定常状態では通常 `false` です。

FreeBSD など Linux 以外のホストでは、ライブ SAM 捕捉は未対応です。コントローラーはホストを変更せず、`SAM capture not implemented on this OS` と報告します。

Linux のライブデータプレーンは Azure + PVE 同一サブネットのラボでスモークテスト済みです。ただしプレリリースの動作であるため、本番利用前にプロバイダーとファイアウォールポリシーの実構成で検証してください。

## 逆方向パスフィルタリング

厳格な逆方向パスフィルタリングは、SAM で転送されたトラフィックを破棄する可能性があります。モバイル `/32` が直接接続サブネットに属して見える一方で、戻り経路がオーバーレイになるためです。routerd は SAM のために `rp_filter` を黙って変更しません。これはインターフェースポリシーとして影響が大きいためです。

`routerctl doctor hybrid` はホストチェックが有効な場合に `net.ipv4.conf.<capture-or-tunnel-interface>.rp_filter` を読みます。値が厳格（`1`）の場合は警告を出し、対象インターフェースでの緩和モード（`2`）の検討を促す改善策を表示します。

## プロバイダーの機能

| プロバイダー | MVP 機能の説明 |
| --- | --- |
| Azure | NIC セカンダリ IP とルーター NIC の IP フォワーディング有効化。 |
| AWS | ENI セカンダリプライベート IPv4 と送信元/宛先チェックの無効化。 |
| OCI | VNIC プライベート IP オブジェクトと送信元/宛先チェックの無効化。 |
| GCP | エイリアス IP またはルート機能。宣言されたプロバイダープロファイルの機能でゲートされます。 |

プロファイルは宣言的な記述子です。モビリティプランナーはプロバイダー `ActionPlan` レコードを生成できますが、アドレスの割り当てや NIC フラグの変更はプロバイダーアクション実行ポリシーとエグゼキュータープラグインによってゲートされます。プランナー自身がプロバイダーの状態を変更することはありません。

## 同一サブネットのフロー

`10.0.0.0/24` のラボで、`10.0.0.7/32` がクラウド VM のアドレス、`10.0.0.9/32` がオンプレミス/PVE VM のアドレスとします。目的は、クラウド VM `10.0.0.7` からオンプレミス VM `10.0.0.9` へ TCP 接続を開始し、両方の VM のデフォルトゲートウェイはローカルのまま、NAT なしで通信させることです。

1. クラウド VM が `10.0.0.9` 宛に送信します。
2. Azure NIC のセカンダリ IP 捕捉が `10.0.0.9/32` 宛のパケットをクラウド側の routerd ノードへ届けます。
3. クラウド側の routerd ノードは生成された IPIP SAM トランスポート経由でパケットを配送します。暗号化が有効な場合、この IPIP パケットはエンドポイント限定の `wg-hybrid` アンダーレイ上を通ります。
4. オンプレミス側は `10.0.0.9` の所有者へ転送します。
5. 送信元 IP と宛先 IP は元のエンドポイントアドレスのままです。

`10.0.0.7/32` の戻り経路はオンプレミス側の proxy-ARP で捕捉します。PVE LAN のホストはオンプレミス側の routerd ノード経由で `.7` に到達し、オンプレミス側の routerd ノードが同じ生成済み SAM トランスポート経由でクラウド側の routerd ノードへ配送します。

分割した設定例は次の 2 つです。

- `examples/hybrid-azure-pve-same-subnet-cloud.yaml`: クラウド側の routerd ノードに適用し、オンプレミス VM `10.0.0.9/32` のプロバイダーセカンダリ IP クレームを含みます。
- `examples/hybrid-azure-pve-same-subnet-onprem.yaml`: オンプレミス側の routerd ノードに適用し、クラウド VM `10.0.0.7/32` の proxy-ARP クレームを含みます。

## ファイアウォールと NAT の構成

選択的アドレス移動性は通常のスイッチング/フォワーディングプレーンに位置します。`nat`、`preserveSource`、ファイアウォール、ゾーンのフィールドは持ちません。アドレスの透過性は本質的に備わっており、送信元アドレスと宛先アドレスはそのまま保持されます。

モバイルアドレスにファイアウォールや NAT を適用する場合は、既存の `FirewallZone`、`FirewallRule`、`NAT44Rule` リソースでリテラルの `/32` アドレスを参照します。現在のモデルではファイアウォールや NAT の Kind から `MobilityPool` や低レベルの `RemoteAddressClaim` へのクロス Kind 参照はありません。結合は意図的にリテラルアドレスによる緩い結合としています。有用と判明すれば、名前付き参照を後日追加できます。

SAM で転送されたトラフィックは、他の転送トラフィックと同様に既存のファイアウォールと conntrack のパスを通ります。独立しているとは、モビリティリソースがファイアウォールや NAT のポリシーを設定しないという意味であり、バイパスするという意味ではありません。

特に、配送された `/32` トラフィックは捕捉インターフェースとトンネルインターフェースの間で Linux ファイアウォールの `FORWARD` チェインを通過します。デフォルト拒否のフォワーディングポリシーを持つルーターでは、捕捉されたアドレスのフォワーディングパスを明示的に許可してください。SAM 自体はファイアウォールルールを追加しません。

## クラウドノードでのオーバーレイ/フェデレーションアドレッシング

イベントフェデレーションのトランスポート（`routerd-eventd` の待受アドレスと各 `EventPeer.endpoint`）、BGP/BFD ピアアドレス、`SAMTransportProfile` が生成する SAM トランスポートエンドポイント/内部アドレスは、全ノードで自分がエンドツーエンドに制御できるアドレス範囲を使ってください。WireGuard を SAM トランスポートの下に置く場合、そのインターフェース/ピアエンドポイントアドレスも同じ条件です。クラウドインスタンスでは、プロバイダーが内部利用のために予約している範囲からオーバーレイ、BGP/BFD、フェデレーション用のアドレスを取っては**いけません**。

- `169.254.0.0/16`（RFC 3927 リンクローカル）。クラウドのインスタンスメタデータ（IMDS）は `169.254.169.254` にあり、イメージによってはブロック全体を予約します。Oracle Cloud の Linux イメージは `169.254.0.0/16` 全体を `InstanceServices` チェインにルーティングするため、`169.254.x` のオーバーレイアドレス宛のフェデレーション SYN はループバックに引き込まれて RST されます（同じアドレスへの ICMP は通るにもかかわらず、です）。AWS と Azure も IMDS に `169.254.169.254` を使います。症状: ローカルの所有権事実はあるのに、ノード間の `routerd-eventd`、BGP、BFD セッションが確立しない。
- `100.64.0.0/10`（RFC 6598 キャリアグレード NAT）。プロバイダーアンダーレイの CGNAT や Tailscale（`100.x` の tailnet アドレス、MagicDNS）が使います。この範囲のオーバーレイは Tailscale への参加やキャリア NAT と衝突します。

SAM トランスポートエンドポイント、`SAMTransportProfile.innerPrefix`、任意の WireGuard エンドポイントアドレス、`routerd-eventd` の待受 / `EventPeer` エンドポイント、BGP/BFD ピアリングアドレスには、自分で予約した RFC 1918 の範囲を使ってください。モビリティプール `/24`（捕捉されるアドレス）とも、上記のクラウド予約範囲とも分離します。これは全プロバイダー（AWS/Azure/OCI）に当てはまります。OCI がリンクローカル予約を最も厳格に強制するだけの違いです。

## クライアントエンドポイントのアドレッシングとルーターオーバーレイ到達性

クライアントゲストの `lo`/dummy インターフェースに設定したグローバル一意の `/32` は、ゲスト OS がそのアドレスを持っているだけではクラウドファブリックを越えて到達できません。クラウドファブリック（VPC/VNet/VCN）はプロバイダーサブネット CIDR 内の宛先だけをクライアント ENI/NIC に配送します。VPC CIDR 外の宛先は、ルーター上にオーバーレイ経路があっても、ファブリックがクライアントに届ける前に破棄します。

具体的に、異なるアドレッシングの 4 サイトテストでは、

- **オーバーレイ上のルーターエンドポイント `/32`** はエンドツーエンドで到達可能です（ルーターが WireGuard 上で運びます）。ルーターエンドポイントのディスティンクトメッシュは 12 本の方向付き ping＋SSH を通過します。
- **VPC CIDR 外のクライアント dummy/lo `/32` は到達できません**。オーバーレイ経路とプロバイダーフォワーディングが有効でも、クラウドファブリックはそれらをクライアント ENI に配送しません。

したがって、ディスティンクトメッシュのショートカットエンドポイントは**ルーターエンドポイント専用**として扱ってください。クライアントにグローバル一意でファブリック越しにルーティング可能なアドレスを与えるには、プロバイダーがルーティング可能なクライアントサブネットか、プロバイダーが割り当てたクライアント IP（ファブリックが実際に配送するセカンダリ IP/捕捉アドレス）が必要です。ゲストローカルの dummy `/32` では足りません。マルチサイトラボを設計する際、ルーターオーバーレイの到達性とクライアントファブリックの到達性を混同しないでください。

## 対象外

MVP は完全な L2 延伸、EVPN、BUM 転送、ブロードキャスト/マルチキャスト延伸、ゲートなしの自動クラウド API 変更、動的パッチ/置換のセマンティクス、`rp_filter` の自動変更を実装しません。
