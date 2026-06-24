---
title: MAC アドレスでゲスト端末を隔離する
---

# MAC アドレスでゲスト端末を隔離する

![ClientPolicy guest mode が MAC address を分類し、nftables set を生成し、zone matrix の前に LAN や management access を拒否する流れ](/img/diagrams/how-to-guest-mode.png)

`ClientPolicy` は routerd のゲストモードです。
同じ LAN 上の端末を MAC アドレスで分類し、通常のゾーン間ファイアウォールマトリクスより先に、より厳しい転送方針を適用します。

VLAN をまだ分けていない構成でも、信頼済みの端末と、インターネットだけを使わせたい端末との境界を明確にできます。

## ユースケース

代表的な用途は次の通りです。

- 家庭内で、来客のスマートフォン、ゲーム機、家電、持ち込み PC を、管理網や自宅サーバーへ届かせたくない場合。
- 集合住宅や共有住宅で、既定をゲストとし、明示した端末だけを信頼済みにしたい場合。
- カメラ、HEMS、テレビ、スピーカーなどの IoT 端末を隔離する場合。DNS、DHCP、NTP、インターネットは使わせますが、横方向の通信は不要です。
- 小規模オフィスの来客用ネットワークで、物理 LAN は共有しつつ、RFC 1918 や ULA 宛ての通信を遮断したい場合。

完全な例は [examples/guest-mode.yaml](https://github.com/imksoo/routerd/blob/main/examples/guest-mode.yaml) を参照してください。

## 仕組み

Linux では、routerd は `ClientPolicy` を nftables の `inet routerd_filter` テーブルに生成します。

各方針から、次の規則を生成します。

- `client_policy_guest_devices` のような nftables の `ether_addr` set。
- `mode: include` 用の `ether saddr @set` 照合。
- `mode: exclude` 用の `ether saddr != @set` 照合。
- 選択したルーター内サービスへの、self 向け許可規則。
- プライベート IPv4 宛てと ULA IPv6 宛ての転送拒否規則。
- 拒否規則より前に置く、任意の許可規則。

生成した規則は、`input` チェーンと `forward` チェーンの早い位置に入ります。
そのため、通常なら `trust -> self` や `trust -> trust` の役割マトリクスで許可される通信でも、ゲスト端末の通信は先に絞り込まれます。

`ClientPolicy` は `FirewallZone` の代替ではありません。
通常のモデルはそのまま使います。

- `FirewallZone` は、インターフェースの通常の役割を決めます。
- `FirewallPolicy` は、拒否ログなどの共通動作を決めます。
- `FirewallRule` は、明示的な例外を追加します。
- `ClientPolicy` は、LAN の中に端末単位の制限を重ねます。

MAC 照合は、Ethernet の送信元アドレスを直接見ます。
そのため、DHCP リースは必須ではありません。
ただし `DHCPv4Reservation` を併用すると、端末の IPv4 アドレス、名前、Web 管理画面での表示が安定します。

## 仕様

`ClientPolicy` は `firewall.routerd.net/v1alpha1` に属します。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    macs:
      - "18:ec:e7:33:12:6c"
    isolation:
      lanInternet: allow
      lanLAN: deny
      lanMgmt: deny
      mDNSBroadcast: deny
```

| フィールド | 必須 | 意味 |
| --- | --- | --- |
| `mode` | はい | `include` または `exclude` です。 |
| `interfaces` | いいえ | 方針を適用する LAN 側の `Interface` 参照です。`Interface/lan` と `lan` は同じインターフェースを指します。省略時は `trust` の `FirewallZone` に属する全インターフェースへ適用します。 |
| `macs` | いいえ | 短縮形の MAC 一覧です。include モードではゲスト、exclude モードでは信頼済みとして扱います。 |
| `isolation` | いいえ | ゲストの意図を表します。`lanInternet`、`lanLAN`、`lanMgmt`、`mDNSBroadcast` に `allow` または `deny` を指定できます。 |
| `classification` | いいえ | 構造化したクライアント分類のエントリです。 |
| `classification[].mode` | はい | `trusted`、`guest`、`isolated` のいずれかです。 |
| `classification[].match.macs` | いいえ | 端末の MAC アドレスです。routerd は生成前に正規化します。 |
| `classification[].match.ouiPrefixes` | いいえ | `18:ec:e7` のようなベンダーの OUI プレフィックスです。 |
| `classification[].match.hostnamePatterns` | いいえ | 観測した DHCP ホスト名に対する glob パターンです。 |
| `classification[].match.dhcpFingerprints` | いいえ | routerd が観測した DHCP フィンガープリントのラベルです。 |
| `classification[].name` | いいえ | 人が読むための端末名です。現時点では説明用の値です。 |
| `classification[].ipv4Reservation` | いいえ | `DHCPv4Reservation` の名前です。`DHCPv4Reservation/aiseg2` ではなく `aiseg2` と書きます。 |
| `guestServices` | いいえ | ゲスト端末に許可するルーター内サービスです。既定値は `dhcp`、`dns`、`ntp` です。指定できる値は `dhcp`、`dns`、`ntp`、`mdns`、`ssdp` です。 |
| `guestEgressDeny` | いいえ | ゲスト端末の転送先として拒否する CIDR です。省略時は RFC 1918 と ULA を拒否します。 |
| `guestEgressAllow` | いいえ | 拒否規則より先に許可する CIDR です。 |

既定の `guestEgressDeny` は次の通りです。

- `10.0.0.0/8`
- `172.16.0.0/12`
- `192.168.0.0/16`
- `fc00::/7`

`isolation.mDNSBroadcast: deny` を指定すると、ゲストからの mDNS、SSDP、NetBIOS による探索の転送も拒否します。ゲスト端末がマルチキャストやブロードキャストの探索で LAN 内の端末を見つける挙動を抑えます。

許可規則は、拒否規則より先に生成されます。
プリンターやキャプティブポータルの補助サーバーなど、狭い例外を作るときに使えます。

## 例 1: 最小の include モード

1 つの MAC アドレスだけをゲストとして扱います。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - mode: guest
        match:
          macs:
            - "18:ec:e7:33:12:6c"
        name: aiseg2
```

## 例 2: 複数端末の include モード

複数の端末を同じゲスト規則で扱えます。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: household-guests
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - mode: guest
        match:
          macs:
            - "18:ec:e7:33:12:6c"
        name: aiseg2
        ipv4Reservation: aiseg2
      - mode: guest
        match:
          macs:
            - "7c:2f:80:11:22:33"
        name: guest-phone
      - mode: guest
        match:
          macs:
            - "90:09:d0:44:55:66"
        name: smart-tv
```

## 例 3: BYOD 向けの exclude モード

対象インターフェース上の端末を、既定でゲストとして扱います。
一覧に書いた MAC アドレスだけを信頼済みとして扱います。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: byod-default-guest
  spec:
    mode: exclude
    interfaces:
      - Interface/lan
    classification:
      - mode: trusted
        match:
          macs:
            - "bc:24:11:e0:8e:3a"
        name: admin-laptop
      - mode: trusted
        match:
          macs:
            - "4e:20:15:aa:e0:67"
        name: owner-phone
```

## 例 4: 拒否と許可のカスタム CIDR

既定のプライベート宛て拒否を保ちつつ、1 台のプリンターだけ許可します。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-with-printer
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestEgressAllow:
      - 172.18.20.10/32
    guestEgressDeny:
      - 10.0.0.0/8
      - 172.16.0.0/12
      - 192.168.0.0/16
      - fc00::/7
    classification:
      - mode: guest
        match:
          macs:
            - "7c:2f:80:11:22:33"
        name: guest-phone
```

## 例 5: ローカル探索サービス

既定では、ゲスト端末に DHCP、DNS、NTP を許可します。
ルーター上でローカル探索のプロキシや中継を動かす場合は、`mdns` や `ssdp` を明示的に追加します。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: media-guests
  spec:
    mode: include
    interfaces:
      - Interface/lan
    guestServices:
      - dhcp
      - dns
      - ntp
      - mdns
      - ssdp
    classification:
      - mode: guest
        match:
          macs:
            - "90:09:d0:44:55:66"
        name: smart-tv
```

探索サービスは、公開される情報を理解したうえで有効にしてください。
mDNS や SSDP は便利ですが、端末名やサービス情報を見せてしまうことがあります。

## 例 6: IoT の隔離と固定割り当て

固定割り当てがあると、調査しやすくなります。
Web 管理画面の端末一覧や DNS レコードも分かりやすくなります。

```yaml
- apiVersion: net.routerd.net/v1alpha1
  kind: DHCPv4Reservation
  metadata:
    name: thermostat
  spec:
    server: lan-v4
    macAddress: "02:11:22:33:44:55"
    hostname: thermostat
    ipAddress: 172.18.0.151

- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: iot-isolation
  spec:
    mode: include
    interfaces:
      - Interface/lan
    classification:
      - mode: guest
        match:
          macs:
            - "02:11:22:33:44:55"
        name: thermostat
        ipv4Reservation: thermostat
```

## DHCPv4Reservation との連携

`classification[].ipv4Reservation` は参照の検証用です。
routerd は、指定した `DHCPv4Reservation` が存在することを確認します。
ファイアウォールの照合は、リースされた IP アドレスではなく MAC アドレスで行います。

この分離は意図的です。

- MAC 照合により、IP 層の判断より前に端末を分類できます。
- IPv4 の固定リースにより、DNS と Web 管理画面の表示が安定します。
- 端末の IP アドレスが変わっても、ゲスト隔離は MAC アドレスに追従します。

端末がランダム MAC アドレスを使う場合は、その SSID または有線セグメントで実際に見えている MAC アドレスを登録してください。

## 生成規則の確認

設定を生成するか、稼働中の nftables テーブルを確認します。

```sh
routerd render nftables --config /usr/local/etc/routerd/router.yaml
sudo nft list table inet routerd_filter
```

次のような規則を確認します。

```nft
set client_policy_guest_devices {
  type ether_addr
  elements = { 18:ec:e7:33:12:6c }
}

iifname "ens19" ether saddr @client_policy_guest_devices udp dport 53 counter accept
iifname "ens19" ether saddr @client_policy_guest_devices ip daddr 10.0.0.0/8 counter log prefix "routerd client-policy guest-devices deny " drop
```

## ゲスト端末からの動作確認

ゲスト端末から確認します。

```sh
curl -4 https://www.google.com/generate_204
curl -4 --connect-timeout 3 http://192.168.1.1/
curl -4 --connect-timeout 3 http://172.18.0.1:8080/
```

期待する結果は次の通りです。

- インターネットへの通信は成功します。
- プライベート宛ては、タイムアウトするか失敗します。
- DNS、DHCP、NTP は動き続けます。

ルーター側では、tcpdump でパケットの経路を確認します。

```sh
sudo tcpdump -ni ens19 ether host 18:ec:e7:33:12:6c
sudo nft list chain inet routerd_filter forward
```

プライベート宛てが拒否されると、生成された `ClientPolicy` 規則のカウンターが増えます。

## トラブルシューティング

### MAC アドレスが一致しない

ルーターから見えている MAC アドレスを確認します。

```sh
ip neigh show dev ens19
sudo tcpdump -eni ens19
```

無線端末は、SSID ごとに異なる MAC アドレスを使うことがあります。
スマートフォンやノート PC は、ランダム MAC アドレスを使うこともあります。
端末に印字された MAC アドレスではなく、ルーター側の LAN で見えているアドレスを使ってください。

### ゲスト端末からプライベートネットワークに届いてしまう

次を確認してください。

- 方針が正しい `Interface` を参照している。
- パケットがそのインターフェースから入っている。
- `routerctl apply` で最新の nftables テーブルが反映されている。
- `guestEgressAllow` に広いプライベートプレフィックスが入っていない。
- 端末側の VPN クライアントなど、ルーターを迂回する経路がない。

### ゲスト端末からインターネットに出られない

`ClientPolicy` は、プライベート宛てと self 宛てを絞ります。
インターネットへの疎通が失敗する場合は、経路方針、NAT44、DS-Lite、DNS、IP forwarding を確認します。

```sh
routerctl status
sysctl net.ipv4.ip_forward
sudo nft list table ip routerd_nat
```

### guestServices の役割

`guestServices` は、ルーター自身のローカルサービスへのアクセスだけを制御します。
プライベートサブネットへの転送許可ではありません。
転送の例外は `guestEgressAllow` で表します。

## セキュリティ上の注意

MAC アドレスによる隔離は実用的ですが、暗号学的な識別ではありません。
悪意ある利用者は、端末を操作できれば信頼済みの MAC アドレスを偽装できます。

`ClientPolicy` は、家庭や小規模オフィス向けの実用的な制御として使ってください。
敵対的な利用者に対する唯一の境界にはしないでください。
より強い設計には、次があります。

- VLAN または SSID の分離。
- WPA3 Enterprise または 802.1X。
- スイッチのポート分離。
- 端末ごとの資格情報。
- 専用のゲスト bridge または VRF。

これらと組み合わせても、`ClientPolicy` は有用です。
端末分類の意図を routerd のリソースモデルに残せるためです。

## OS 対応

Linux の nftables に対応しています。

FreeBSD の pf は、routerd が `FirewallZone` と `FirewallRule` で使うルーティング経路上のフィルタリングに、同じ MAC ベースの分類モデルを持ちません。
そのため routerd は、FreeBSD では `ClientPolicy` を明示的に未対応として扱います。何の効果も生まないまま黙って適用する、という方針はとりません。

将来の FreeBSD 対応としては、ブリッジ階層のフィルターや、専用のレイヤー 2 分離リソースが考えられます。
ただし、ルーティング層の pf 規則とは等価ではないため、別の設計として扱うべきです。

## 関連リソース

- `FirewallZone`: インターフェースを `trust`、`untrust`、`mgmt` へ割り当てます。
- `FirewallPolicy`: 拒否ログなどの共通動作を有効にします。
- `FirewallRule`: MAC 分類に紐付かない例外を表します。
- `DHCPv4Reservation`: 分類済みの端末へ、安定した IPv4 アドレスとホスト名を与えます。
- トンネルから自動導出される TCP MSS clamp は、ファイアウォールのゾーンとトンネル経路が一致するゲスト転送にも適用されます。ゲスト隔離が MSS clamp を迂回することはありません。
