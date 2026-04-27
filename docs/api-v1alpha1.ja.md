---
title: リソース API v1alpha1
slug: /reference/api-v1alpha1
---

# リソース API v1alpha1

routerd の設定は宣言的なリソースの集まりです。ひとつひとつのリソースは「ルータがこう振る舞ってほしい」という意図を表します。インターフェースを上げる、アドレスプールを配る、AFTR へトンネルを張る、健全な上流にデフォルト経路を載せる、といった粒度です。`reconcile` は、その意図とホストの現状を比べて差分を縮める作業を行います。

リソースの形は Kubernetes 風で揃えています。

- `apiVersion`
- `kind`
- `metadata.name`
- `spec`
- 必要に応じて `status`

このページでは、各リソースを書くとルータがどう振る舞うか、設定値を変えると何が変わるか、ホスト側にどんな構成物が現れるかを順に説明します。

## API グループ

- `routerd.net/v1alpha1`: トップレベルの `Router`
- `net.routerd.net/v1alpha1`: インターフェース、アドレッシング、DNS、経路ポリシー、トンネル
- `firewall.routerd.net/v1alpha1`: ファイアウォールゾーンとポリシー
- `system.routerd.net/v1alpha1`: ホスト名、sysctl、NTP クライアント、routerd の内部イベント送出先
- `plugin.routerd.net/v1alpha1`: プラグインマニフェスト

## 用意されているリソース

ネットワーク関連:
`Interface`、`PPPoEInterface`、`IPv4StaticAddress`、`IPv4DHCPAddress`、`IPv4DHCPServer`、`IPv4DHCPScope`、`IPv6DHCPAddress`、`IPv6PrefixDelegation`、`IPv6DelegatedAddress`、`IPv6DHCPServer`、`IPv6DHCPScope`、`SelfAddressPolicy`、`DNSConditionalForwarder`、`DSLiteTunnel`、`StatePolicy`、`HealthCheck`、`IPv4DefaultRoutePolicy`、`IPv4SourceNAT`、`IPv4PolicyRoute`、`IPv4PolicyRouteSet`、`IPv4ReversePathFilter`、`PathMTUPolicy`。

ファイアウォール:
`Zone`、`FirewallPolicy`、`ExposeService`。

システム:
`Hostname`、`Sysctl`、`NTPClient`、`NixOSHost`、`LogSink`。

種類は意識的に絞っています。汎用プラットフォームではなく、ルータとして新しい振る舞いが必要になったときだけ種類を増やします。

## 反映方針

トップレベルの `spec.reconcile` は、反映中に一部が失敗したときの動きを決めます。

```yaml
apiVersion: routerd.net/v1alpha1
kind: Router
metadata:
  name: lab-router
spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
      - mgmt
    protectedZones:
      - mgmt
```

- `mode: strict` が既定値です。どこかで反映に失敗したら、そこで止まってエラーを返します。
- `mode: progressive` は、独立して進められる反映段階をできるだけ続けます。失敗した段階は警告として残し、全体の結果は `Degraded` になります。途中で失敗した場合、残置物の削除と所有台帳への記録は行いません。
- `protectedInterfaces` は、管理経路を運ぶインターフェースを指定します。routerd は、失敗後に処理を続けてよいか判断するとき、このインターフェースを安全上の支点として扱います。
- `protectedZones` は、ルータ自身へのアクセスを残すべきファイアウォールゾーンを指定します。nftables の出力では、ファイアウォールポリシーに明示し忘れても、このゾーンからの SSH を開けます。

これは、ホスト上のすべての操作を完全な一括取引にする仕組みではありません。routerd に「管理経路は残す」「安全に進められるものは進める」「失敗したデータ転送側の作業は次回の反映で見える状態に残す」という明確な規則を与えるためのものです。

## 状態と条件

### StatePolicy

`StatePolicy` は、ホストの観測結果を名前付きの状態変数として記録します。各状態変数には次の 3 つのステータスがあります。

- `unknown`: routerd がまだ評価していない、または観測自体に失敗している状態。
- `unset`: 評価した結果、値が無いことが確定している状態。
- `set`: 評価した結果、具体的な値が記録されている状態。

空文字を値とした `Set` 呼び出しは `unset` に正規化します。`unset`（値が無いことが確定）と `unknown`（未評価）は意味が異なるため、一度 `set` か `unset` になった変数を `unknown` に戻すには、明示的な reset / forget 操作が必要です。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: StatePolicy
metadata:
  name: wan-ipv6-mode
spec:
  variable: wan.ipv6.mode
  values:
    - value: pd-ready
      when:
        ipv6PrefixDelegation:
          resource: wan-pd
          available: true
    - value: address-only
      when:
        ipv6PrefixDelegation:
          resource: wan-pd
          available: false
          unavailableFor: 180s
        ipv6Address:
          interface: wan
          global: true
        dnsResolve:
          name: gw.transix.jp
          type: AAAA
          upstreamSource: static
          upstreamServers:
            - 2404:1a8:7f01:a::3
            - 2404:1a8:7f01:b::3
```

`spec.when` を指定したリソースは、その条件が真と評価されたときだけ反映対象になります。通常の比較では `unknown` と `unset` はどちらも偽として扱います。`unset` 自体を条件にしたい場合は `exists: false` を、`unknown` も含めてステータスを直接条件にしたい場合は `status` を使い、明示的に指定してください。

```yaml
when:
  state:
    wan.ipv6.mode:
      in:
        - pd-ready
        - address-only
```

状態条件で使えるマッチ演算子:

- `exists: true`: 変数が `set` のとき真。
- `exists: false`: 変数が `unset` のとき真。`unknown` は真になりません。
- `equals`: 変数が `set` で、値が指定値と一致するとき真。
- `in`: 変数が `set` で、値が候補のいずれかに一致するとき真。
- `contains`: 変数が `set` で、値が指定した文字列を含むとき真。
- `status`: ステータスそのもの（`set` / `unset` / `unknown`）を直接指定してマッチさせる。
- `for`: 上記でマッチした状態または値が、指定した時間以上継続しているときに限り真。

同一マッチ内で指定した複数のフィールド（`equals`、`for` など）は AND で結合し、`spec.when.state` に複数の変数を並べた場合も、すべての変数のマッチを AND で評価します。`spec.when` には OR 演算子はありません。同じ変数の値に対する OR は `in: [a, b, c]` で表現できますが、それを超える OR が必要な場合は、`StatePolicy` で合成した状態変数を経由して表現します。

`StatePolicy.values` は上から順に評価され、最初にマッチしたエントリの値で `Set` されます（どれもマッチしないときは `unset`）。同じ値を記録するエントリを複数並べると、それぞれの `when` 条件の OR と等価になります。

```yaml
kind: StatePolicy
spec:
  variable: wan.ready
  values:
    - value: ready
      when:
        ipv6PrefixDelegation:
          resource: wan-pd
          available: true
    - value: ready
      when:
        ipv6Address:
          interface: wan
          global: true
```

参照側は `when: { state: { wan.ready: { equals: ready } } }` と書けば、上のいずれかの条件が成立したときにマッチします。

現時点で `spec.when` を指定できるリソースは、DHCP スコープ、IPv6 委譲アドレス、DS-Lite トンネル、ヘルスチェック、IPv4 NAT、IPv4 ポリシー経路セット、IPv4 デフォルト経路候補です。

## インターフェース

### Interface

`Interface` は routerd が知っておく（必要に応じて管理する）ネットワークインターフェースを宣言します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: Interface
metadata:
  name: lan
spec:
  ifname: ens19
  adminUp: true
  managed: true
```

ルータの振る舞い:

- `spec.ifname` は、別名 `lan` をホスト上の実際のリンクに結び付けます。他のリソースは常に `lan` を参照し、カーネル側の名前を直接書きません。
- `spec.adminUp: true` で管理状態を up に保ちます。
- `spec.managed: true` のとき、routerd はリンクとアドレスの状態を変更できます。ただし cloud-init や netplan が既に握っている場合は奪わず、計画上で「取り込み待ち」として表示します。
- `spec.managed: false` の場合は観測専用です。別名解決はしますが、リンクとアドレスは触りません。

ホスト側の所有関係や、`/var/lib/routerd/artifacts.json` のローカル台帳の扱いは [リソース所有と反映モデル](resource-ownership.ja.md) を参照してください。

### PPPoEInterface

`PPPoEInterface` は、別の `Interface` の上に PPPoE セッションを張ります。Linux では pppd / rp-pppoe の peer 設定、CHAP/PAP secret、systemd ユニットを routerd が管理します。FreeBSD では `mpd5` の設定を出力し、管理対象のセッションがある場合は `mpd5` の rc.d サービスを起動します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PPPoEInterface
metadata:
  name: wan-ppp
spec:
  interface: wan-ether
  ifname: ppp0
  username: user@example.jp
  passwordFile: /usr/local/etc/routerd/pppoe-password
  defaultRoute: true
  usePeerDNS: true
  managed: true
  mtu: 1492
  mru: 1492
```

ルータの振る舞い:

- `spec.interface` は土台になる Ethernet の `Interface` を参照します。
- `spec.ifname` を省略すると `ppp-<metadata.name>` になります。Linux の制限により 15 文字以内である必要があります。
- 認証情報は `spec.password` か `spec.passwordFile` のどちらか一方を指定します。本体 YAML に秘密情報を残さないよう、通常は `passwordFile` を推奨します。
- `spec.managed: true` のとき、Linux では `routerd-pppoe-<name>.service` を有効化して起動します。FreeBSD では、生成した `mpd5` の既定ラベルからそのセッションを読み込みます。`managed: false` のときは設定だけを残し、自動接続しません。
- `spec.defaultRoute: true` で pppd が PPP リンク経由のデフォルト経路を入れます。複数の上流を併用する場合は `IPv4DefaultRoutePolicy` と組み合わせます。
- `spec.usePeerDNS: true` で、PPP 対向が広告した DNS サーバを受け入れます。
- `spec.mtu` と `spec.mru` は、上流が 1500 より小さい MTU を要求するときに指定します。PPPoE では通常 1492 になります。

## IPv4 アドレッシング

### IPv4StaticAddress

`IPv4StaticAddress` は固定の IPv4 プレフィックスをインターフェースに載せます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4StaticAddress
metadata:
  name: lan-ipv4
spec:
  interface: lan
  address: 192.168.10.3/24
  exclusive: true
```

ルータの振る舞い:

- routerd は `192.168.10.3/24` を LAN インターフェースに割り当て、ルータ自身のアドレスとして扱います。
- `spec.exclusive: true` のとき、反映時にそのインターフェース上の他の静的 IPv4 アドレスを取り除きます。番号付け替え後に古い設定が残ったまま重複するのを防ぐためです。
- 計画段階で、宣言した静的アドレスと他のインターフェースで観測しているプレフィックスが照合されます。別インターフェースに重複するプレフィックスがあると既定では拒否されます。NAT 検証や HA、ラボ用途で意図的な重複を許可する場合は明示します。

  ```yaml
  spec:
    interface: lan
    address: 192.168.10.3/24
    allowOverlap: true
    allowOverlapReason: overlapping customer network for NAT lab
  ```

### IPv4DHCPAddress

`IPv4DHCPAddress` は、上流の DHCPv4 から IPv4 アドレスを取得します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPAddress
metadata:
  name: wan-dhcp4
spec:
  interface: wan
  client: dhclient
  required: true
```

ルータの振る舞い:

- `interface` 上の DHCPv4 クライアントを routerd が管理します。`spec.client` でクライアント実装を選びます（現状は `dhclient`）。
- `spec.required: true` のとき、リースが取れない場合は反映が失敗します。WAN アドレスの取得を前提に他の設定が成り立つ場合に有効です。
- `spec.useRoutes: false` を指定すると、対応している出力先では DHCP で配られた経路を使いません。`spec.useDNS: false` は DHCP で配られた DNS サーバーを使いません。管理用インターフェースで、IPAM からアドレスだけを受け取り、デフォルト経路やリゾルバーを変えたくない場合に使います。
- `spec.routeMetric` は、DHCP で配られた IPv4 経路を使う場合のメトリックを指定します。

管理用インターフェースの例:

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPAddress
metadata:
  name: mgmt-dhcp4
spec:
  interface: mgmt
  client: networkd
  required: false
  useRoutes: false
  useDNS: false
```

## IPv4 DHCP と DNS の提供

### IPv4DHCPServer と IPv4DHCPScope

DHCPv4 サービスは、サーバを表すリソースと、そのサーバを 1 本のインターフェースに紐付けるスコープに分かれています。サーバはひとつの dnsmasq インスタンスに対応します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPServer
metadata:
  name: dhcp4
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
  dns:
    enabled: true
    upstreamSource: dhcp4
    upstreamInterface: wan
    cacheSize: 1000
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DHCPScope
metadata:
  name: lan-dhcp4
spec:
  server: dhcp4
  interface: lan
  rangeStart: 192.168.10.100
  rangeEnd: 192.168.10.199
  leaseTime: 12h
  routerSource: interfaceAddress
  dnsSource: self
  authoritative: true
```

ルータの振る舞い:

- `spec.listenInterfaces` は dnsmasq に対する許可リストです。スコープは、対応するサーバの `listenInterfaces` に含まれるインターフェースにしか紐付けられません。リストに無いインターフェースは `except-interface` として書き出すため、明示しない限り WAN にサービスを出してしまうことはありません。
- `IPv4DHCPScope.routerSource` はゲートウェイオプションの出し方を決めます。`interfaceAddress` ならルータの LAN アドレス、`static` なら `spec.router` の値、`none` ならオプション自体を出しません。
- `IPv4DHCPScope.dnsSource` は DNS サーバオプションの出し方を決めます。
  - `dhcp4` と `static` は、DHCPv4 オプションに DNS サーバを直接書き込みます。このスコープでは dnsmasq が 53 番ポートを開く必要はありません。
  - `self` のとき、ルータ自身の LAN IPv4 アドレスを DNS サーバとして広告し、dnsmasq を DNS フォワーダ兼キャッシュとして動かします。フォワード先は `IPv4DHCPServer.spec.dns` で決まります。
    - `upstreamSource: dhcp4` は、`upstreamInterface` で DHCPv4 から学習した DNS サーバへ転送します。
    - `upstreamSource: static` は `upstreamServers` を使います。
    - `upstreamSource: system` はホストのリゾルバ設定に従います。
    - `upstreamSource: none` は上流フォワーダを持たずに動かします。
  - `none` は DNS オプションそのものを出しません。
- `spec.interface` がまだ取り込み待ちの状態（cloud-init などが握っている）にある場合、その上で DHCP を提供すると競合するため、計画段階で DHCP スコープも止めます。

## IPv6 アドレッシングとプレフィックス委譲

### IPv6PrefixDelegation

`IPv6PrefixDelegation` は、上流側のインターフェースで IPv6 プレフィックスの委譲を要求します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6PrefixDelegation
metadata:
  name: wan-pd
spec:
  interface: wan
  client: networkd
  profile: ntt-hgw-lan-pd
  prefixLength: 60
  convergenceTimeout: 5m
  iaid: ca53095a
  duidType: link-layer
  duidRawData: 00:01:02:00:5e:10:20:30
```

ルータの振る舞い:

- routerd は systemd-networkd の追加設定を `/etc/systemd/network/10-netplan-<ifname>.network.d/` 配下に書き出し、WAN 側で DHCPv6-PD を要求します。
- `spec.profile` は既知の上流環境向けにパラメータを切り替えます。
  - `default`: 一般的な DHCPv6-PD。
  - `ntt-ngn-direct-hikari-denwa`: NTT NGN/ONU に直結し、ひかり電話契約を使う構成。
  - `ntt-hgw-lan-pd`: NTT のホームゲートウェイの LAN 側につなぎ、`/60` 単位で再委譲を受ける構成。

  どちらの NTT 系プロファイルも IA_PD のみを要求し、rapid commit を無効化、リンクレイヤ DUID を使用、必要に応じて DHCPv6 Solicit を強制し、`prefixLength` を明示しなければ `/60` をヒントにします。
- `spec.convergenceTimeout` は、過去に見えていた委譲プレフィックスを「なくなった」と判断するまで routerd が待つ時間です。DHCPv6 クライアント自身のパケット再送間隔を変えるものではありません。通常の既定値は `2m`、NTT 系プロファイルでは `5m` です。ホームゲートウェイの再起動直後や、古いリースを覚えている状態では収束に時間がかかることがあるためです。
- routerd は反映のたびに、観測できたプレフィックス委譲の状態をローカルの状態保存領域に記録します。キーは `ipv6PrefixDelegation.<name>.currentPrefix`、`ipv6PrefixDelegation.<name>.lastPrefix`、`ipv6PrefixDelegation.<name>.uplinkIfname`、`ipv6PrefixDelegation.<name>.downstreamIfname`、`ipv6PrefixDelegation.<name>.prefixLength` です。有効な待ち時間は `ipv6PrefixDelegation.<name>.convergenceTimeout` にも記録します。下流側の委譲プレフィックスが見えなくなった場合でも、待ち時間のあいだは `currentPrefix` を維持します。待ち時間を過ぎても見えない場合は `currentPrefix` を消しますが、`lastPrefix` は残します。これにより、既知の機器を新規クライアントではなく既存リースの更新相手として扱う上流機器に対応するための足場を残せます。
- systemd-networkd と FreeBSD の `dhcp6c` では、取得できる範囲で DHCP の識別情報も記録します。キーは `ipv6PrefixDelegation.<name>.iaid`、`ipv6PrefixDelegation.<name>.duid`、`ipv6PrefixDelegation.<name>.duidText`、`ipv6PrefixDelegation.<name>.identitySource` です。`dhcp6c` では `/var/db/dhcp6c_duid` から DUID を読み取り、IAID は設定された `iaid`、または `dhcp6c` の既定値である `0` から決めます。NTT 系プロファイルでは、上流インターフェースの MAC アドレスから DHCPv6 のリンクレイヤ DUID を計算し、`ipv6PrefixDelegation.<name>.expectedDUID` に残します。これらは望ましい設定ではなく、観測した状態の記憶です。将来の再試行処理では、この情報を使って、ホームゲートウェイが以前のリースを覚えている場合に更新に近い動きを優先できます。
- リース期限が切れる前の Renew/Rebind は、OS 側の DHCPv6 クライアントの責務です。routerd は通常の反映でこのクライアントを再起動しないようにします。再起動すると、更新として続けられたはずの処理が新規 Solicit や Release に変わることがあるためです。
- `spec.iaid` は DHCPv6 の IAID を固定します。10 進数、`0x` 付きの 16 進数、または 8 桁の 16 進数で書けます。systemd-networkd では 10 進数の `IAID=` として出力し、FreeBSD の `dhcp6c` では `ia-pd` / `id-assoc pd` の識別子として使います。
- `spec.duidType` と `spec.duidRawData` は systemd-networkd の DUID 設定を固定します。`duidRawData` は `00:01:...` のようなバイト列表記でも、区切りなしの 16 進数でも書けます。現時点ではこの2つは systemd-networkd 向けです。FreeBSD の `dhcp6c` が持つ DUID ファイルの管理は、まだリソースとして扱っていません。

NTT のホームゲートウェイには、IPv6 を RA/SLAAC のみで配布し DHCPv6-PD に応答しないモードもあります。これは `IPv6PrefixDelegation` ではモデル化できないため、別途 RA/SLAAC のリソース設計を行う必要があります。

### IPv6DelegatedAddress

`IPv6DelegatedAddress` は、委譲されたプレフィックスから下流のサブネットを切り出し、その中にルータの安定したアドレスを置きます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DelegatedAddress
metadata:
  name: lan-ipv6-pd-address
spec:
  prefixDelegation: wan-pd
  interface: lan
  subnetID: "0"
  addressSuffix: "::3"
  sendRA: true
  announce: true
```

ルータの振る舞い:

- 委譲されたサブネットと固定サフィックスを組み合わせて LAN 側のアドレスを決めます。systemd-networkd ではサフィックスが `Token=` として書き出されるため、`::3` は委譲プレフィックス内のホスト識別子 `::3` を持つアドレスになります。
- `spec.sendRA: true` のとき、dnsmasq から RA としてプレフィックスを広告します。
- `spec.announce: true` のとき、`dnsSource: self` や DS-Lite の local アドレス選定でこのアドレスを候補として扱います。

### IPv6DHCPAddress

`IPv6DHCPAddress` は上流側のインターフェースで DHCPv6 クライアントを動かし、IA_NA アドレスを取得します。プレフィックス委譲は `IPv6PrefixDelegation` 側の責務で、こちらとは独立しています。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DHCPAddress
metadata:
  name: wan-dhcp6
spec:
  interface: wan
  client: networkd
  required: true
```

### IPv6DHCPServer と IPv6DHCPScope

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DHCPServer
metadata:
  name: dhcp6
spec:
  server: dnsmasq
  managed: true
  listenInterfaces:
    - lan
```

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv6DHCPScope
metadata:
  name: lan-dhcp6
spec:
  server: dhcp6
  delegatedAddress: lan-ipv6-pd-address
  mode: stateless
  leaseTime: 12h
  defaultRoute: true
  dnsSource: self
```

ルータの振る舞い:

- スコープは `IPv6DelegatedAddress` に紐付くので、WAN 側で受けた DHCPv6-PD の結果に LAN 側のプレフィックスが自動で追従します。
- `spec.mode: stateless` のとき、クライアントは SLAAC でアドレスを決め、DHCPv6 からは DNS などのオプションだけを受け取ります。
- `spec.mode: ra-only` は、DHCPv6 のアドレス払い出しを行わず RA だけを送ります。
- IPv6 のデフォルト経路は RA で広告します。DHCPv6 自体にデフォルトゲートウェイのオプションはありません。
- 委譲された LAN 側プレフィックスをまだ観測できない場合、routerd は
  dnsmasq の IPv6 スコープを一時的に出力しません。DHCPv6-PD の収束を
  待っている間も、IPv4 DHCP と DNS は動かし続けられます。
- `spec.dnsSource: self` のとき、ルータの LAN 側 IPv6 アドレス（例: `pd-prefix::3`）を DNS サーバとして広告します。`dnsSource: static` と `dnsServers` の組み合わせでは、固定の DNS サーバ一覧を広告します。
- dnsmasq の RA が有効な場合、routerd は DHCPv6 と RA RDNSS の両方に同じ IPv6 DNS サーバ一覧を流します。Android のように DHCPv6 を使わず SLAAC/RDNSS で動くクライアントを想定するため重要です。

### SelfAddressPolicy

`SelfAddressPolicy` は、`dnsSource: self` がローカルアドレスを選ぶときの優先順位を決めます。同じインターフェースに、委譲由来の LAN アドレスと DS-Lite 用の補助アドレスのように複数の IPv6 アドレスがある場合に使います。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: SelfAddressPolicy
metadata:
  name: lan-ipv6-self
spec:
  addressFamily: ipv6
  candidates:
    - source: delegatedAddress
      delegatedAddress: lan-ipv6-pd-address
      addressSuffix: "::3"
    - source: interfaceAddress
      interface: lan
      matchSuffix: "::3"
    - source: interfaceAddress
      interface: lan
      ordinal: 1
```

`IPv6DHCPScope` の `spec.selfAddressPolicy` から参照します。候補は上から順に評価され、最初に解決できたものが採用されます。ポリシーを参照しない場合は、委譲アドレスと `IPv6DelegatedAddress.addressSuffix` の組、サフィックス一致の観測アドレス、観測されたグローバルアドレスの先頭、の順で代替する標準動作になります。

### DNSConditionalForwarder

`DNSConditionalForwarder` は、特定のドメインだけ別の DNS サーバへ転送します。dnsmasq では `server=/domain/upstream` として書き出されます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DNSConditionalForwarder
metadata:
  name: transix-aftr
spec:
  domain: gw.transix.jp
  upstreamSource: static
  upstreamServers:
    - 2404:1a8:7f01:a::3
    - 2404:1a8:7f01:b::3
```

`upstreamSource` で転送先の決め方を選びます。

- `static`: `upstreamServers` を使います。
- `dhcp4`: `upstreamInterface` で DHCPv4 から学習した DNS サーバを使います。
- `dhcp6`: `upstreamInterface` で DHCPv6 から学習した DNS サーバを使います。

これにより、全体の DNS は広告ブロック用の上流に向けつつ、DS-Lite の AFTR FQDN のように事業者の DNS でしか正しい AAAA が返らない名前だけを事業者 DNS に流す、といった構成が書けます。

## DS-Lite

### DSLiteTunnel

`DSLiteTunnel` は AFTR に向けた DS-Lite B4 トンネルを宣言します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: DSLiteTunnel
metadata:
  name: transix
spec:
  interface: wan
  tunnelName: ds-transix
  aftrFQDN: gw.transix.jp
  aftrDNSServers:
    - 2404:1a8:7f01:a::3
    - 2404:1a8:7f01:b::3
  aftrAddressOrdinal: 1
  aftrAddressSelection: ordinalModulo
  localAddressSource: delegatedAddress
  localDelegatedAddress: lan-ipv6-pd-address
  localAddressSuffix: "::100"
  defaultRoute: true
  routeMetric: 50
  mtu: 1454
```

ルータの振る舞い:

- routerd は `ipip6` トンネルを作り、IPv6 の下層は `spec.interface` を経由して AFTR に到達させます。
- `spec.remoteAddress` を省略すると `aftrFQDN` の AAAA を引きます。`aftrDNSServers` は、特定の DNS でしか AFTR の AAAA が返らない事業者向けに使います。AAAA の応答は文字列として昇順に並べ替え、`aftrAddressOrdinal` の番号（1 始まり）で1つ選びます。
- `aftrAddressSelection` は、現在の AAAA 件数より大きい番号を指定したときの挙動を決めます。
  - `ordinal`: そのトンネルの反映を失敗させます。
  - `ordinalModulo`: 件数で折り返して選びます。
- `localAddressSource` はトンネルの IPv6 ローカルアドレスの決め方です。
  - `interface`: `spec.interface` の最初のグローバル IPv6 アドレスを使います。
  - `static`: `localAddress` を使います。
  - `delegatedAddress`: `localDelegatedAddress` で参照する `IPv6DelegatedAddress` から導出します。`localAddressSuffix` を指定するとそのサフィックスで上書きします。

  `delegatedAddress` を選んだ場合、導出したローカルアドレスが該当インターフェースに無ければ `/128` で追加します。これにより、DS-Lite の下層は WAN を使いつつ、複数のトンネルがそれぞれ違う LAN 委譲由来のソースアドレスを使えます。
- `defaultRoute: true` でトンネル経由の IPv4 デフォルト経路を入れ、`routeMetric` で複数上流間の優先度を決めます。

`ordinalModulo` で複数のトンネルを並べる場合は、トンネルごとに `localAddressSuffix` を変えてください。AFTR の数が減ってもトンネルが重複せず維持できます。健全性に応じた切替には別途 `HealthCheck` が必要です。

## IPv4 経路

### HealthCheck

`HealthCheck` は1件の到達性チェックを宣言します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: HealthCheck
metadata:
  name: dslite-v4
spec:
  type: ping
  role: next-hop
  targetSource: dsliteRemote
  interface: transix-a
```

ルータの振る舞い:

- `spec.interval` を省略した場合は 60 秒です。経路切替が過度に敏感にならないよう、短い間隔は明示したときだけ使う前提です。
- `spec.target` を明示しないときは `targetSource: auto` で近傍の確認先を自動で選びます。DS-Lite トンネルは AFTR の IPv6 アドレスを、通常のインターフェースや PPPoE はそのインターフェースの IPv4 デフォルトゲートウェイを確認します。
- `spec.role` は、その確認が運用上どの種類の依存性を見ているかを示します。これ自体は送出するパケットを変えませんが、経路ポリシーや状態出力を読みやすくします。
  - `link`: インターフェースの存在、キャリア、管理状態。
  - `next-hop`: ゲートウェイ、AFTR、トンネル端点など近傍の転送依存。省略時の既定値です。
  - `internet`: 公開先までの到達性。たとえば公開アドレスへの ping や TCP 接続。
  - `service`: DNS 解決、DHCP、AFTR FQDN の解決、PPPoE セッションなど、サービス固有の依存。
  - `policy`: 経路候補を選んでよいかの集約結果。将来用。

IPv4 のインターネット全体の到達性を確認したい場合は、`role: next-hop` の確認に詰め込まず、明示的な静的 IPv4 ターゲットで `role: internet` を別に立てるのが安全です。

### IPv4DefaultRoutePolicy

`IPv4DefaultRoutePolicy` は、IPv4 のデフォルト経路をどの上流に載せるかを決めます。健全な候補のうち `priority` が最小のものが現役になります。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4DefaultRoutePolicy
metadata:
  name: default-v4
spec:
  mode: priority
  sourceCIDRs:
    - 192.168.10.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  candidates:
    - name: dslite
      routeSet: lan-dslite-balance
      priority: 10
      healthCheck: dslite-v4
    - name: pppoe
      interface: wan-pppoe
      gatewaySource: none
      priority: 20
      table: 111
      mark: 273
      routeMetric: 60
      healthCheck: pppoe-v4
    - name: dhcp4
      interface: wan
      gatewaySource: dhcp4
      priority: 30
      table: 112
      mark: 274
      routeMetric: 100
      healthCheck: wan-dhcp4-v4
```

ルータの振る舞い:

- 候補はインターフェースを直接指すか、`routeSet` で `IPv4PolicyRouteSet` を参照します。
- 直接型の候補には専用のルーティングテーブルとファイアウォールマークが割り当てられます。新規フローには現役候補のマークを付けます。既存フローはコネクション追跡のマークで同じ候補を維持し、現役候補が不健全になったら、そのフローのマークを現在の現役候補へ付け替えます。
- ルートセット型の候補が現役のときは、新規フローにマークを付けず、参照先の `IPv4PolicyRouteSet` がハッシュで宛先を選べるようにします。健全な宛先のコネクション追跡マークはそのまま維持します。失敗した候補に紐付くマークは消し、ルートセットに再選択させます。
- 候補に `healthCheck` を指定しないときは常に up 扱いです。

IPv6 のデフォルト経路はあとで設計するため、現状はこのリソースの対象外です。

### IPv4SourceNAT

`IPv4SourceNAT` は、送信元レンジと変換方法を指定して下りの IPv4 を NAT します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4SourceNAT
metadata:
  name: lan-to-wan
spec:
  outboundInterface: transix
  sourceCIDRs:
    - 192.168.10.0/24
  translation:
    type: interfaceAddress
    portMapping:
      type: range
      start: 1024
      end: 65535
```

ルータの振る舞い:

- `outboundInterface` は `Interface`、`PPPoEInterface`、`DSLiteTunnel` を参照できます。
- `translation.type: interfaceAddress` は、送出インターフェースに現在乗っている IPv4 アドレスへ変換します。Linux では masquerade として書き出されます。
- `translation.type: address` は、固定の 1 アドレスへ変換します。

  ```yaml
  translation:
    type: address
    address: 203.0.113.10
  ```
- `translation.type: pool` は、複数のアドレスに分散します。

  ```yaml
  translation:
    type: pool
    addresses:
      - 203.0.113.10
      - 203.0.113.11
  ```
- `translation.portMapping`:
  - `auto`: 送信元ポートはプラットフォームに任せます。
  - `preserve`: 可能な限り元のポート番号を維持します。
  - `range`: `start` から `end` の範囲に収めます。

### IPv4PolicyRoute

`IPv4PolicyRoute` は、条件に合う転送トラフィックを特定の出口に流します。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4PolicyRoute
metadata:
  name: lan-via-transix
spec:
  outboundInterface: transix
  table: 100
  priority: 10000
  mark: 256
  sourceCIDRs:
    - 192.168.10.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  routeMetric: 50
```

ルータの振る舞い: routerd は条件に合う IPv4 パケットにマークを付け、そのマーク用の `ip rule` と専用ルーティングテーブルを作って、そこにデフォルト経路を入れます。LAN の異なるプレフィックスを別々の上流に流すときの基本部品です。ハッシュ分散は別リソース `IPv4PolicyRouteSet` の役割です。

### IPv4PolicyRouteSet

`IPv4PolicyRouteSet` は、ハッシュで複数の出口候補のいずれかを選びます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4PolicyRouteSet
metadata:
  name: lan-dslite-balance
spec:
  mode: hash
  hashFields:
    - sourceAddress
    - destinationAddress
  sourceCIDRs:
    - 192.168.10.0/24
  destinationCIDRs:
    - 0.0.0.0/0
  targets:
    - name: transix-a
      outboundInterface: transix-a
      table: 100
      priority: 10000
      mark: 256
      routeMetric: 50
    - name: transix-b
      outboundInterface: transix-b
      table: 101
      priority: 10001
      mark: 257
      routeMetric: 50
```

ルータの振る舞い:

- routerd は nftables のルールとして、既存のコネクション追跡マークを復元し、新規フローには `jhash` でマークを選び、選んだマークをコネクション追跡側に保存し、各候補ごとに `ip rule` とルーティングテーブルを 1 組ずつ用意します。
- 確立済みのフローは、そのコネクション追跡マークによって同じ候補に固定されます。
- `hashFields` は現状 `sourceAddress` と `destinationAddress` に対応します。
- ローカル IPv6 アドレスを使い分けた複数の DS-Lite トンネルに分散する用途を主に想定しています。各候補は通常、別々の `DSLiteTunnel` を指します。

### IPv4ReversePathFilter

`IPv4ReversePathFilter` は Linux の `rp_filter` を制御します。ポリシールーティングや複数 DS-Lite トンネルでは、戻り通信の経路チェックに引っかかって正規のトラフィックが落ちることがあるためです。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: IPv4ReversePathFilter
metadata:
  name: rp-filter-transix-a
spec:
  target: interface
  interface: transix-a
  mode: disabled
```

`spec.target` は `all`、`default`、`interface` のいずれかです。`target: interface` の場合、`spec.interface` は `Interface`、`PPPoEInterface`、`DSLiteTunnel` を参照できます。`spec.mode` は `disabled`、`strict`、`loose` で、Linux の値 0、1、2 に対応します。

## PathMTUPolicy

`PathMTUPolicy` は、ある下流インターフェースから複数の上流インターフェースへ抜ける通信の実効 MTU を計算し、その値を IPv6 RA で広告したり、転送 TCP の MSS をクランプして端末同士の MSS を揃えます。

```yaml
apiVersion: net.routerd.net/v1alpha1
kind: PathMTUPolicy
metadata:
  name: lan-wan-mtu
spec:
  fromInterface: lan
  toInterfaces:
    - wan
    - transix
  mtu:
    source: minInterface
  ipv6RA:
    enabled: true
    scope: lan-dhcp6
  tcpMSSClamp:
    enabled: true
    families:
      - ipv4
      - ipv6
```

ルータの振る舞い:

- `mtu.source: minInterface` は `toInterfaces` に並べたインターフェースの設定 MTU の最小値を採用します。`Interface` の標準は 1500、`PPPoEInterface` の標準は 1492、`DSLiteTunnel` の標準は 1454 です。各リソースで `spec.mtu` が指定されていればそちらを優先します。
- `mtu.source: static` は `mtu.value` をそのまま使います。
- `ipv6RA.enabled: true` のとき、参照した `IPv6DHCPScope` を経由して RA で MTU を広告します。dnsmasq では `ra-param=ens19,1454` のように書き出されます。
- `tcpMSSClamp.enabled: true` のとき、nftables の forward チェインに MSS クランプのルールを入れます。MSS は実効 MTU から計算し、IPv4 で 40 バイト、IPv6 で 60 バイトを引きます。`families` を省略すると IPv4 / IPv6 の両方を有効にします。

## ファイアウォール

最初のファイアウォール API は汎用ルール言語ではなく、家庭用ルータの安全な既定動作と、必要なサービス公開だけに絞っています。

### Zone

`Zone` はルータのインターフェースの集合に名前を付けます。

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: Zone
metadata:
  name: lan
spec:
  interfaces:
    - lan
---
apiVersion: firewall.routerd.net/v1alpha1
kind: Zone
metadata:
  name: wan
spec:
  interfaces:
    - wan-pppoe
```

### FirewallPolicy

`FirewallPolicy` はプリセットとチェインの既定値を宣言します。

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: FirewallPolicy
metadata:
  name: default-home
spec:
  preset: home-router
  input:
    default: drop
  forward:
    default: drop
  routerAccess:
    ssh:
      fromZones:
        - lan
      wan:
        enabled: false
    dns:
      fromZones:
        - lan
    dhcp:
      fromZones:
        - lan
```

`home-router` プリセットを選ぶと次の規則が入ります。

- input と forward の既定をいずれも drop にする。
- invalid を drop、established/related を accept、loopback への input を accept する。
- ルータ自身の IPv6 制御通信として、ICMPv6 と、WAN 側で受ける DHCPv6
  クライアント応答を許可する。DHCPv6 は UDP 宛先ポート 546 だけを見て、
  送信元ポートは縛らない。一部のホームゲートウェイがエフェメラルポート
  から応答するため。
- `lan` と `wan` の両ゾーンが存在する場合、LAN から WAN への forward を許可する。
- ルータ自身への SSH / DNS / DHCP は、`routerAccess` で指定したゾーンからのみ許可する。WAN からの SSH は `routerAccess.ssh.wan` で別管理にする。

### ExposeService

`ExposeService` は内部の IPv4 サービスを 1 件公開します。

```yaml
apiVersion: firewall.routerd.net/v1alpha1
kind: ExposeService
metadata:
  name: nas-https
spec:
  family: ipv4
  fromZone: wan
  viaInterface: wan-pppoe
  protocol: tcp
  externalPort: 443
  internalAddress: 192.168.10.20
  internalPort: 443
  sources:
    - 203.0.113.0/24
  hairpin: true
```

ルータの振る舞い: routerd は DNAT のルールと、対応する forward 許可を入れます。`spec.sources` を指定すると、そのプレフィックスからの接続だけを許可します。`spec.hairpin` はリソース形状としては受け付けますが、外側アドレスの選定モデルがまだ整っていないため、現状の出力にはヘアピン用ルールは含まれません。

## システムリソース

### NixOSHost

`NixOSHost` は `routerd render nixos` 向けに NixOS のホスト設定を宣言
するリソースです。`routerd serve` の実行時調整（reconcile）の対象には
含めず、ホストへの反映は Nix 経由で行います。具体的には、生成された
`routerd-generated.nix` を最小限の手書き `configuration.nix` から
import し、`nixos-rebuild switch` で適用します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: NixOSHost
metadata:
  name: router02
spec:
  hostname: router02
  domain: example.net
  stateVersion: "25.11"
  boot:
    loader: grub
    grubDevice: /dev/sda
  routerdService:
    enabled: true
    binaryPath: /usr/local/sbin/routerd
    configFile: /usr/local/etc/routerd/router.yaml
    reconcileInterval: 60s
  debugSystemPackages: true
  ssh:
    enabled: true
    passwordAuthentication: true
    permitRootLogin: "no"
  sudo:
    wheelNeedsPassword: false
  users:
    - name: admin
      groups:
        - wheel
      initialPassword: change-me
      sshAuthorizedKeys:
        - ssh-ed25519 AAAA...
```

ルータの振る舞い:

- `spec.hostname` と `spec.domain` から `networking.hostName` および
  `networking.domain` を生成します。
- `spec.boot.loader: grub` と `spec.boot.grubDevice` から、最小限の
  GRUB ブートローダー設定を生成します。
- `spec.users` から `users.users.<name>` を生成し、併せて SSH 公開鍵も
  配置します。
- `spec.ssh` と `spec.sudo` から OpenSSH と sudo の設定を生成します。
- `spec.routerdService.enabled: true` のときは、`routerd serve` を
  起動するローカルの systemd ユニットを生成します。flake の NixOS
  モジュールを取り込まず、`/usr/local/sbin/routerd` に置いた
  バイナリでまず動かすような単純なホスト向けです。既定では
  `/usr/local/sbin/routerd`、`/usr/local/etc/routerd/router.yaml`、
  `/run/routerd/routerd.sock`、反映周期 `60s` を使います。
- `spec.debugSystemPackages` を有効にすると、運用時の動作確認に使う
  ツール一式を `environment.systemPackages` に追加します。追加する
  パッケージはリソースから導き、`dnsmasq`、`nftables`、`ppp`、
  `iproute2` など必要なものを含めます。
- `persistent: true` の `Sysctl` リソースは `boot.kernel.sysctl` として
  生成します。実行中のカーネルだけに反映する sysctl は、引き続き
  デーモン側の担当です。
- 上記で足りない場合は、`spec.additionalPackages` で
  `environment.systemPackages` に、`spec.additionalServicePath` で
  routerd ユニットの `PATH` に、それぞれ追加のパッケージを足せます。

### Hostname

`Hostname` はホスト名を宣言します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Hostname
metadata:
  name: system
spec:
  hostname: router03.example.net
  managed: true
```

`managed: false` のときは観測のみ、`managed: true` のときに実機のホスト名を反映します。

### Sysctl

`Sysctl` はカーネルパラメータを 1 件宣言します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: Sysctl
metadata:
  name: ipv4-forwarding
spec:
  key: net.ipv4.ip_forward
  value: "1"
  runtime: true
  persistent: false
```

`runtime: true` で、反映時に実行中のカーネルへ値を書き込みます。`persistent: true` は sysctl.d や rc.conf 相当の永続化用に予約済みで、現状は反映しません。

### NTPClient

`NTPClient` はローカルの NTP クライアントを宣言します。最初の実装は `systemd-timesyncd` と固定サーバ一覧の管理に対応します。

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: NTPClient
metadata:
  name: system-time
spec:
  provider: systemd-timesyncd
  managed: true
  source: static
  interface: wan
  servers:
    - pool.ntp.org
```

ルータの振る舞い: `interface` を指定すると、そのリンクに対する `NTP=` の追加設定を systemd-networkd 経由で書き出します。省略時は `systemd-timesyncd` のグローバルなサーバ一覧として書き出します。

### LogSink

`LogSink` は routerd の内部イベント（設定ロード、計画出力、反映結果、プラグインのエラーなど）の送出先を宣言します。

ローカルの journald / syslog に出す場合:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogSink
metadata:
  name: local-syslog
spec:
  type: syslog
  minLevel: info
  syslog:
    facility: local6
    tag: routerd
```

信頼済みのローカルプラグインに渡す場合:

```yaml
apiVersion: system.routerd.net/v1alpha1
kind: LogSink
metadata:
  name: external-log
spec:
  type: plugin
  minLevel: warning
  plugin:
    path: /usr/local/libexec/routerd/log-sinks/example
    timeout: 5s
```

省略時の既定値は `enabled: true`、`minLevel: info`、`syslog.facility: local6`、`syslog.tag: routerd` です。リモート syslog に送る場合は `syslog.network`（`udp` / `tcp` / `unix` / `unixgram`）と `syslog.address`（例: `syslog.example.net:514`）を指定します。
