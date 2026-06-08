---
title: 選択的アドレス移動性
---

# 選択的アドレス移動性

これは L2 セグメント全体を延伸する仕組みではありません。routerd CloudEdge は public
cloud に Ethernet segment を延伸しません。public cloud fabric は運用者が
制御できる broadcast domain を提供せず、経路とアドレス所有権のモデルも
provider ごとに異なります。

選択的アドレス移動性は、選択した IPv4 `/32` アドレスだけを片側で
capture し、routerd 間 overlay 経由で所有者側へ届ける抽象です。
TCP/IP の送信元 / 宛先アドレスは保持されます。ファイアウォールと NAT は
別の routerd レイヤーであり、mobility resource の field ではありません。

![MobilityPool と SAMTransportProfile を authoring surface とし、生成された IPIP delivery、BGP peer、ECMP next hop、secondary IP または proxy ARP capture を示す SAM transport 図](/img/diagrams/cloudedge-sam-ipip.png)

## リソースモデル

CloudEdge Mobility control plane では、運用者が書く mobility intent は
`MobilityPool` だけです。論理 IPv4 pool、読み取る EventGroup、member node と
site、BGP delivery mode、capture policy、provider trap placement を宣言します。
member list は BGP peer list に近いものとして扱います。各 node は他 node の
identity、site、role、placement を知る必要がありますが、remote node の NIC ID、
provider resource 名、subnet ID などの実装詳細を知る必要はありません。

north-star の config shape は次の通りです。

- **自 site** は capture と provider discovery の詳細まで完全に宣言します。
- **remote site** は identity-only member（`nodeRef`、`site`、`role`、必要なら
  `placement` / `maintenance`)として宣言します。
- 大きめの fabric では、共有の identity-only member list を `MobilityMemberSet`
  に置き、`MobilityPool.spec.membersFrom` で import します。
- local cloud capture の再利用可能な詳細は `profiles.cloudCaptures` に置きます。
- secret ではない node-local 値は `spec.values` に置き、`capture.targetFrom` と
  `ownershipDiscovery.subnetRefFrom` で参照します。

`MobilityMemberSet` は mobility 側の `SAMPeerGroup` に相当する resource です。
含めるのは共有される member identity fields（`nodeRef`、`site`、`role`、必要なら
`placement` / `maintenance`）だけです。`capture`、`ownershipDiscovery`、
`profileRef`、delivery fields、static owned addresses は含めません。これらは必要な
node の `MobilityPool` 側に local 設定として残します。

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

pool は 1 つ以上の member set を import できます。import された member を先に追加
し、`nodeRef` 単位で local の `spec.members` を後から重ねます。そのため leaf は
共有 topology を member set から受け取りつつ、自分自身の capture/discovery だけを
local に書けます。

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

必須の `membersFrom` source がまだ届いていない場合、pool は `Pending` になります。
bootstrap 中に partial な local member list で動かしてよい場合だけ
`optional: true` を指定します。

例えば AWS router 上の config は次のようになります。

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
          scope:
            includePrimary: false
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

オンプレミス node では逆に、on-prem member が完全な自己宣言になります。
通常は `staticOwnedAddresses` と、`activeWhen.type: vrrp-master` で gate した
`proxy-arp` capture を持ちます。cloud member は identity-only のままです。
つまり local router が local implementation detail を持ち、remote member は
peer identity だけを持つ、という境界です。

routerd は federation や provider discovery の観測事実から、所有中の `/32`
path を BGP で advertise します。運用者は `MobilityPool` だけを編集し、
address ごとの advertisement と provider trap action plan は controller が導出します。

同一 provider の cloud router maintenance では、`members[].placement.group`
内の drain されていない member から `priority`、次に `nodeRef` の順で active capture
member を選びます。`members[].maintenance.drain: true` にすると、その member は
active 選出から外れ、planner が生成済み capture claim と provider action plan
を次の候補へ移します。placement projection を deterministic に保つため、pool の
全 node に同じ `MobilityPool` config を配ります。

### 目標フィールドリファレンス

`spec.values`
: この node の config normalize 時に使う secret ではない local 値です。region、
  compartment ID、resource group、subnet ID、NIC name などに使います。credential、
  token、private key、account secret は置かないでください。

`spec.profiles.cloudCaptures.<name>.capture`
: local cloud の `provider-secondary-ip` capture に使う再利用可能な default です。
  member は `members[].profileRef` で参照できます。member 側の明示 field が
  profile より優先されます。

`spec.profiles.cloudCaptures.<name>.ownershipDiscovery`
: provider private-IP inventory scan の再利用可能な default です。
  `ownershipDiscovery.providerRef` が空の場合は、有効な `capture.providerRef` を
  継承します。

`members[].profileRef`
: 名前付き cloud capture profile を member に適用します。通常は local self
  member にだけ使い、remote member では省略します。

`members[].capture.targetFrom`
: 生成される provider action の target key を `spec.values` の key に対応させます。
  同じ key が `capture.target` にもある場合は、明示 `capture.target` が勝ちます。

`members[].ownershipDiscovery.subnetRefFrom`
: `ownershipDiscovery.subnetRef` が空の場合に `spec.values` から値を解決します。

`members[].placement`
: deterministic な active/standby capture placement を宣言します。identity-only
  の remote cloud member にも placement は有効です。他 node が同一 site のどの
  member が active かを同じように判断するためです。

古い "remote-full inline" style、つまり各 node が remote member の provider
詳細まで繰り返し書く形は、pre-release 期間の互換として引き続き受け付けます。
ただし deprecated です。remote member が local capture/discovery detail を持つ
場合、`routerctl validate`、plan、apply は warning を表示します。将来の
pre-release では remote member を identity-only にすることを必須化する可能性が
あります。

`AddressMobilityDomain` と `RemoteAddressClaim` は低位の SAM 表現です。既存の
hand-authored SAM config は引き続きサポートしますが、CloudEdge MobilityPool の
本線は generated SAM claim ではなく BGP `/32` advertisement を使います。

`AddressMobilityDomain` は mobile address が属する IPv4 prefix を定義します。
`mode` は `selective-address` のみです。

`RemoteAddressClaim` は 1 つの mobile `/32`、owner side、capture mechanism、
overlay peer への route delivery を宣言します。

`AddressMobilityDomain.spec.peerRef` は domain-level の default/documentation
peer で、grouping metadata として扱います。MVP dataplane が実際の delivery
peer として使うのは `RemoteAddressClaim.spec.delivery.peerRef` であり、claim
ごとに必須です。

`CloudProviderProfile` は provider capability と external-command auth を
記述します。Mobility planner は provider API を直接呼びません。Cloud capture
では `assign-secondary-ip` や `ensure-forwarding-enabled` の dry-run
`ActionPlan` を生成し、別の provider-action executor 経路が
`ProviderActionPolicy` で明示的に許可された場合だけ import/execution します。

`OverlayPeer` は remote routerd peer と underlay を表します。`HybridRoute` は
通常の L3 remote-prefix routing のために残り、address mobility は prefix
route ではなく per-address forwarding の抽象です。

## capture と delivery

サポートする capture type は次の通りです。

| Type | 意味 |
| --- | --- |
| `provider-secondary-ip` | Provider-owned secondary address object などで cloud fabric が `/32` を capture します。 |
| `proxy-arp` | site router が選択された address に対して local に ARP 応答します。 |

MVP validation で拒否される予約済み capture type は次の通りです。

| Type | 状態 |
| --- | --- |
| `static-host-route` | 将来の dataplane design 用に予約されています。 |
| `garp` | 将来の dataplane design 用に予約されています。 |

`MobilityPool` の delivery mode は BGP です。owned address は IPv4 unicast
`/32` path として advertise され、non-owner は BGP best path を local FIB に
import し、選ばれた overlay next hop へ届けます。`deliveryPolicy.mode: bgp` が
default であり、現在の MobilityPool control plane で唯一サポートされる delivery
mode です。古い route-lowered SAM delivery は、hand-authored
`RemoteAddressClaim` 互換 config のためだけに残っています。

`SAMTransportProfile` は BGP mode SAM の上位 transport profile です。mobility
path を運ぶ peer ごとの `TunnelInterface`、endpoint `/32` `IPv4Route`、
`BGPPeer` を導出します。各 router は `spec.selfNodeRef` を明示する必要があり、
routerd は hostname や BGP router ID から local node identity を推測しません。
profile が複数 peer を持つ場合は、同じ transport domain にいる全 router で同じ
`spec.topologyNodeRefs` を宣言する必要があります。controller はその共有 node list
を sort し、unordered node pair の順位から `spec.innerPrefix` 内の `/31` を割り
当てます。これにより、hub/spoke で各 router の local peer list が異なる場合でも
両端は同じ edge を local/remote が反転した形で導出します。

`SAMPeerGroup` は再利用する transport peer をまとめる resource です。
`SAMTransportProfile.spec.peersFrom` には 1 つ以上の `SAMPeerGroup/<name>` 参照を
指定できます。controller は reconcile 時に group の peer を先に追加し、その後に
profile 直下の `spec.peers` を重ねます。同じ `nodeRef` が両方にある場合は
`spec.peers` が優先されるため、leaf 側に静的 bootstrap 用 peer や local override
を残せます。必須の `peersFrom` が未到着の場合、profile は `Pending` になります。
`optional: true` の source は到着するまで無視されます。

spine/RR 側の profile では `spec.publishPeerGroup: true` を指定できます。この場合
routerd は profile の `selfNodeRef` と concrete local endpoint から `SAMPeerGroup`
を生成し、DynamicConfigPart として publish します。`localEndpointFrom` は publish
前に解決されるため、leaf には直接使える `remoteEndpoint` が配布されます。

`publishPeerGroup: true` を持つ node で `routerd serve` が動いている場合、routerd
は publish 済み peer group を transport network 上の TCP port `19652`
（`GET /v1/peer-groups`）でも返します。leaf 側で必須の `peersFrom` group が
見つからない場合、`spec.underlayInterface` から到達できる WireGuard peer へ
問い合わせ、名前が一致する group を `peer-group-sync/<group-name>` として local
store に保存します。この DynamicConfigPart は通常の TTL で期限切れになり、
publisher が消えた場合は leaf が `Pending` に戻ります。

MobilityPool membership では、RR 側の canonical pool に
`spec.publishMemberSet: true` を指定できます。routerd は local-only member fields
を取り除き、source `mobility-member-set/<pool>` の `MobilityMemberSet`
DynamicConfigPart を publish し、同じ TCP port で `GET /v1/member-sets` として
返します。leaf 側で必須の `membersFrom` source が見つからない場合、取得した set
を `member-set-sync/<set-name>` として保存します。

core router では `spec.bgp.routeReflectorClient` と
`spec.bgp.routeReflectorClusterID` を設定できます。これらは生成される各
`BGPPeer` にコピーされます。edge router では未指定のまま通常の iBGP session と
して使えます。

peer を profile から外すと、その profile の `DynamicConfigPart` は新しい生成
resource set で置き換えられます。profile 削除時は古い part を空の active part
で置き換え、effective config から生成済み tunnel、BGP peer、endpoint route を
消します。具体的な OS cleanup は既存の `TunnelInterface`、`BGPPeer`、
`IPv4Route` controller の stale-resource cleanup に委ねます。

`members[].capture.target` は生成される provider `ActionPlan.target` へコピーする
secret ではない provider target hint です。region、compartment ID、resource
group、NIC name、IP config name などの識別子だけを置き、credential、token、
private key は provider auth mechanism 側に置きます。

Linux の `proxy-arp` capture では、routerd は通常の sysctl controller で
`net.ipv4.conf.<capture-interface>.proxy_arp=1` を有効化し、
`ip neigh add proxy <address> dev <capture-interface>` 相当の proxy neighbor
entry を netlink で追加し、通常の sysctl controller で
`net.ipv4.ip_forward=1` を有効化します。

`provider-secondary-ip` では provider fabric が address capture を担当します。
`configureOSAddress: false` の場合、routerd は mobile address を local OS
address として設定しません。Linux では、cloud-init、netplan、guest agent など
がその address を戻した場合でも、その特定 address だけを local interface から
削除します。そのうえで IPv4 forwarding と overlay への `/32` delivery route を
管理します。overlay への `/32` delivery route は BGP best-path import から
得られます。capture を削除しても routerd は address を戻しません。Guest OS への
address assignment は routerd が所有していないためです。

status ではこれを `captureOSAddressAbsence` として報告します。
`enforced: true` は、routerd が captured address を local OS interface から
無くすことを継続的に enforcement している、という audit flag です。
`lastReconcileRemoved: true` は、直近の reconcile が実際にその address を削除した
ことを示します。address がすでに無い steady state では通常 `false` です。

FreeBSD など Linux 以外の host では live SAM capture は未対応です。controller は
host を変更せず、`SAM capture not implemented on this OS` と報告します。

Linux live dataplane は Azure + PVE same-subnet lab で smoke test 済みです。
ただし pre-release behavior なので、本番利用前に provider と firewall
policy の実構成で検証してください。

## 逆方向パスフィルタリング

strict reverse-path filtering は SAM forwarded traffic を drop する可能性が
あります。mobile `/32` が直接接続 subnet に属して見える一方で、return path が
overlay になるためです。routerd は SAM のために `rp_filter` を黙って変更しませ
ん。これは interface policy として影響が大きいためです。

`routerctl doctor hybrid` は host check が有効な場合に
`net.ipv4.conf.<capture-or-tunnel-interface>.rp_filter` を読みます。値が strict
(`1`) の場合は warning を出し、対象 interface で loose mode (`2`) を検討する
remedy を表示します。

## provider capability

| Provider | MVP capability descriptor |
| --- | --- |
| Azure | NIC secondary IP と router NIC の IP forwarding。 |
| AWS | ENI secondary private IPv4 と source/destination check disabled。 |
| OCI | VNIC private IP object と source/destination check disabled。 |
| GCP | Alias IP または route capability。provider profile の capability で gate します。 |

profile は宣言的な descriptor です。Mobility planner は provider `ActionPlan`
を生成できますが、address assignment や NIC flag 変更は provider-action
execution policy と executor plugin によって gate されます。planner 自身は
provider state を変更しません。

## same-subnet フロー

on-prem `proxy-ARP` capture で `on-demand-arp` source を使う場合、routerd は
source の `scanInterval` ごとに mobility prefix 内の 1 IP だけを能動 ARP
probe します。これにより、既に起動済みで静かな L2 client も、owner 側から
手動で `arping` や ping を打たなくても observed client として収束できます。
広い prefix では `scanInterval` を保守的にし、`/24` lab では `1s` 程度にすると
1 秒 1 probe の範囲で素早く確認できます。

`10.0.0.0/24` lab では、`10.0.0.7/32` が cloud VM の address、
`10.0.0.9/32` が on-prem/PVE VM の address です。目的は、cloud VM
`10.0.0.7` から on-prem VM `10.0.0.9` へ TCP connection を開始し、両方の VM
の default gateway は local のまま、NAT なしで通信させることです。

1. Cloud VM が `10.0.0.9` へ送信します。
2. Azure NIC secondary IP capture が `10.0.0.9/32` 宛の packet を cloud
   routerd node へ届けます。
3. cloud routerd node は packet を `wg-hybrid` 経由で on-prem routerd peer
   へ delivery します。
4. on-prem 側は `10.0.0.9` の owner へ forwarding します。
5. source/destination IP は元のエンドポイントアドレスのままです。

reverse path の `10.0.0.7/32` は on-prem 側の proxy-ARP で capture します。
PVE LAN host は `.7` へ on-prem routerd node 経由で到達し、on-prem routerd
node が overlay 経由で cloud routerd node へ delivery します。

分割した example config は次の 2 つです。

- `examples/hybrid-azure-pve-same-subnet-cloud.yaml`: cloud routerd node に適用し、
  on-prem VM `10.0.0.9/32` の provider-secondary-IP claim を含みます。
- `examples/hybrid-azure-pve-same-subnet-onprem.yaml`: on-prem routerd node に適用し、
  cloud VM `10.0.0.7/32` の proxy-ARP claim を含みます。

## ファイアウォールと NAT の構成

選択的アドレス移動性は通常の switching/forwarding plane にあります。
`nat`、`preserveSource`、firewall、zone field は持ちません。Address
transparency は intrinsic です。

mobile address にファイアウォールや NAT を適用する場合は、既存の `FirewallZone`、
`FirewallRule`、`NAT44Rule` resource で literal `/32` address を参照します。
MVP ではこれらの Kind から `RemoteAddressClaim` への cross-kind reference は
ありません。SAM で転送された traffic は、他の転送 traffic と同じく既存の
firewall/conntrack path を通ります。

特に、delivery された `/32` traffic は capture interface と tunnel interface
の間で Linux firewall の `FORWARD` chain を通過します。forwarding policy が
default-drop の router では、その captured address の forwarding path を明示的
に許可してください。SAM 自体は firewall rule を追加しません。

## クラウドノードでの overlay / federation アドレッシング

Event Federation の transport（`routerd-eventd` の listen address と各
`EventPeer.endpoint`）、BGP/BFD peer address、`SAMTransportProfile` が生成する
SAM transport endpoint / inner address は、全ノードで自分が end-to-end に制御できる
アドレス範囲を使ってください。WireGuard を SAM transport の下に置く場合、その
interface / peer endpoint address も同じ条件です。クラウドインスタンスでは、provider
が内部利用のために予約している範囲から overlay / BGP/BFD / federation アドレスを取っては
**いけません**。

- `169.254.0.0/16`(RFC 3927 link-local)。クラウドのインスタンスメタデータ
  (IMDS)は `169.254.169.254` にあり、イメージによってはブロック全体を予約
  します。Oracle Cloud の Linux イメージは `169.254.0.0/16` 全体を
  `InstanceServices` chain にルーティングするため、`169.254.x` の overlay
  アドレス宛 federation SYN は loopback に引き込まれて RST されます(同じ
  アドレスへの ICMP は通るのに、です)。AWS/Azure も IMDS に
  `169.254.169.254` を使います。症状: local ownership fact はあるのにノード間の
  `routerd-eventd`、BGP、BFD session が張れない。
- `100.64.0.0/10`(RFC 6598 CGNAT)。provider underlay の CGNAT や Tailscale
  (`100.x` の tailnet アドレス、MagicDNS)が使います。この範囲の overlay は
  Tailscale 参加や carrier NAT と衝突します。

SAM transport endpoint、`SAMTransportProfile.innerPrefix`、任意の WireGuard endpoint、
`routerd-eventd` の listen / `EventPeer` エンドポイント、BGP/BFD peering address には、
自分で予約した RFC 1918 の範囲を使ってください。mobility pool の `/24`（captured
address）とも、上記のクラウド予約範囲とも分離します。これは全 provider
（AWS/Azure/OCI）に当てはまり、OCI が link-local 予約を最も厳格に強制するだけです。

## 対象外

MVP は full L2 extension、EVPN、BUM forwarding、broadcast/multicast
extension、gate なしの automatic cloud API mutation、dynamic patch/replace、
自動 `rp_filter` 変更を実装しません。
