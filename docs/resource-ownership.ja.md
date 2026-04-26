---
title: リソース所有
slug: /reference/resource-ownership
---

# リソース所有と reconcile

routerd は、宣言されたリソースを実機上の artifact に反映します。artifact とは
Linux の link、address、routing rule、routing table、nftables table、systemd
unit、管理対象の設定ファイルなどです。

reconcile の考え方は次の通りです。

1. 実機の現在状態を集める。
2. 各 routerd リソースが、自分の管理したい artifact intent を出す。
3. desired artifact と actual artifact を比較する。
4. 一致しているものは維持する。
5. 足りないもの、ずれているものは作成または更新する。
6. routerd 管理対象なのに、どのリソースからも intent が出ていないものは orphan として削除する。

これは Kubernetes controller の所有関係に近い考え方です。Kubernetes では
生成された子 object に owner reference を付け、削除前の外部 cleanup は
finalizer で扱い、Server-Side Apply では field owner を記録します。routerd
には API server がないので、この所有関係を reconciler とローカル台帳で管理
します。

## Artifact Intent

各リソースは、ひとつ以上の artifact intent を出します。

- `kind`: `linux.ipv4.fwmarkRule` や `nft.table` のような安定した artifact 種別
- `name`: その種別内での安定した識別名
- `owner`: routerd resource ID
- `action`: `ensure`、`delete`、`observe`
- `applyWith`: 差分を是正するモジュールまたはコマンド系統

例えば `IPv4DefaultRoutePolicy` の候補は次の artifact を所有します。

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_default_route`

`IPv4PolicyRouteSet` は次の artifact を所有します。

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_policy`

## Orphan

artifact は次の条件を満たすと orphan です。

- routerd 管理対象の名前空間または範囲にある。
- 実機上に存在する。
- 現在の routerd リソースのどれも、その artifact に intent を出していない。

Linux policy routing では、現時点で fwmark `0x100-0x1ff` を routerd 管理範囲
として扱います。古い DS-Lite route set が残した rule がこの範囲にあれば、
reconcile は stale な `ip rule` を削除し、その routing table が現在のどの
リソースからも使われていなければ table も flush します。

routerd 管理範囲外の artifact は、ローカル台帳で routerd が作成したと判断
できるまでは external として扱います。破壊的な orphan cleanup は、ヒューリ
スティックより台帳上の所有関係を優先します。fwmark `0x100-0x1ff` のような
名前空間や範囲は追加の安全柵として使えますが、nftables table、file、service、
route table のような広い artifact 種別では、それだけを長期的な所有判定にして
はいけません。

## 現在の状態

基礎は入っています。

- `pkg/resource` が artifact、intent、orphan 判定を持つ。
- `pkg/resource` がローカル ownership ledger を持つ。
- `pkg/reconcile` が全リソース種別について artifact intent を宣言する。
- `routerd plan` がリソースごとの artifact intent を表示する。
- `routerd adopt --candidates` が、実機上に存在する desired artifact のうち
  ローカル台帳に未記録のものを表示する。候補検出の実機 inventory は、現時点で
  policy routing、nftables table、一部の systemd service、管理対象 file、
  sysctl key、hostname、link、address、IP-in-IPv6 tunnel を対象にする。
- `routerd adopt --apply` は、実機状態を変更せず、一致している取り込み候補だけを
  ローカル台帳へ記録する。観測値が desired state と違う候補がある場合は拒否する。
- `routerd reconcile --once` が成功した場合、routerd が所有する inventory 可能な
  artifact をローカル台帳へ記録する。
- IPv4 fwmark rule は共通 orphan 判定を使う。cleanup は明示的な routerd
  fwmark 範囲に限定している。

次は、各 apply 処理をコマンド別の手続きから artifact 別 reconciler へ移して
いきます。これにより、新しいリソースが増えても cleanup の考え方を統一できます。

## 対応しているリソース

現在あるすべてのリソース種別は、自分が観測または管理する実機上の artifact を
宣言します。既知のリソース種別が artifact intent をひとつも出さない場合は、
単体テストで失敗します。

| リソース | 実機上の artifact |
| --- | --- |
| `LogSink` | routerd のログ出力先 |
| `Sysctl` | Linux sysctl key |
| `NTPClient` | systemd-timesyncd 設定 |
| `Interface` | Linux link |
| `PPPoEInterface` | PPP interface、routerd PPPoE systemd unit、PPP secrets file |
| `IPv4StaticAddress` | Linux IPv4 address |
| `IPv4DHCPAddress` | DHCPv4 client binding |
| `IPv4DHCPServer` | dnsmasq 設定と service |
| `IPv4DHCPScope` | dnsmasq DHCPv4 scope |
| `IPv6DHCPAddress` | DHCPv6 client binding |
| `IPv6PrefixDelegation` | DHCPv6 prefix delegation binding |
| `IPv6DelegatedAddress` | Linux IPv6 address |
| `IPv6DHCPServer` | dnsmasq 設定と service |
| `IPv6DHCPScope` | dnsmasq DHCPv6 scope |
| `SelfAddressPolicy` | routerd の自分自身のアドレス選択方針 |
| `DNSConditionalForwarder` | dnsmasq 条件付き転送設定 |
| `DSLiteTunnel` | Linux IP-in-IPv6 tunnel |
| `HealthCheck` | routerd scheduler の疎通確認 |
| `IPv4DefaultRoutePolicy` | nftables の mark table、IPv4 route table、IPv4 fwmark rule |
| `IPv4SourceNAT` | nftables NAT table |
| `IPv4PolicyRoute` | IPv4 route table と fwmark rule |
| `IPv4PolicyRouteSet` | nftables policy table、IPv4 route table、IPv4 fwmark rule |
| `IPv4ReversePathFilter` | Linux rp_filter sysctl key |
| `PathMTUPolicy` | nftables MSS table、dnsmasq RA MTU option |
| `Zone` | routerd firewall zone |
| `FirewallPolicy` | nftables filter table |
| `ExposeService` | nftables DNAT table |
| `Hostname` | system hostname |

## 既存設定の取り込み手順

routerd の古い版や手作業で作られた可能性がある実機リソースを、routerd の管理下
に入れる前に候補を確認します。

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --candidates
```

このコマンドは読み取り専用です。設定上は必要で、実機上にも存在するが、
`/var/lib/routerd/artifacts.json` の台帳にまだ記録されていない artifact を
表示します。

候補が正しく、観測値と desired state の差分がないことを確認できたら、ローカル
台帳に記録します。

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --apply
```

`adopt --apply` は kernel、nftables、systemd、file state を変更しません。
所有関係の台帳だけを書きます。候補の観測値が desired state と違う場合は、
先に reconcile するか設定を直してから、もう一度取り込みます。

dry-run ではない `reconcile` が成功した場合も、routerd は inventory できる
owned artifact を台帳へ記録します。まだ安定した実機 identity として照合できない
派生 artifact は、意図的に台帳から外しています。

長期的なルールは保守的にします。file、service、nftables table、一般的な route
table のような広い種別は、ローカル台帳で所有が分かるまで破壊的には削除しません。
名前や番号の範囲は安全柵として使えますが、それだけを所有の根拠にはしません。
