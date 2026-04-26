---
title: リソース所有
slug: /reference/resource-ownership
---

# リソース所有と reconcile

routerd は、宣言されたリソースを実機上の artifact に反映します。artifact とは
Linux の link、address、routing rule、routing table、nftables table、systemd
unit、管理対象の設定ファイルなどです。

このページは運用上かなり重要です。routerd はルーターの kernel networking state
を変更できるため、常に次の3点を明確にしておく必要があります。

1. この実機上の artifact は、どの desired resource が所有しているのか。
2. それは routerd が作ったものか、明示的に取り込んだものか。
3. config から resource が消えたとき、その実機 artifact を削除してよいのか。

ローカル台帳は2番目への永続的な答えです。単なる cache ではありません。破壊的な
cleanup を安全に行うための判断材料です。

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

intent は YAML と実機状態をつなぐものです。リソース固有の処理が、artifact
intent を宣言せずに実機状態をこっそり作ってはいけません。新しいリソース種別や
新しい実機上の object を追加するときは、artifact intent とこの所有権ドキュメント
を一緒に更新します。

`action` には意味があります。

- `ensure`: routerd はこの artifact が存在することを望み、管理対象にできます。
- `observe`: routerd は検出や alias 解決のために見るだけで、cleanup 対象としては
  所有しません。
- `delete`: 明示的な削除フロー用に予約しています。

現時点では、実機 inventory と照合できる `ensure` artifact だけを台帳へ記録します。
あとで安定した実機 identity として照合できない object を記録してしまうことを避ける
ためです。

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

orphan の判断材料は2種類あります。

- **名前空間または範囲による判断**: fwmark `0x100-0x1ff` や `routerd_*` という
  nftables table のように、routerd 用と決めた範囲にある。
- **台帳による判断**: `/var/lib/routerd/artifacts.json` に、過去に routerd
  resource の所有物として記録されている。

台帳による判断の方が強いです。名前空間や番号範囲は安全柵として有用ですが、それだけで
広い破壊的 cleanup の根拠にしてはいけません。たとえば `routerd_foo` という table は
おそらく routerd のものですが、台帳にある場合は、この routerd installation がその
具体的な artifact の所有を受け入れたことを示せます。

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
- `routerd reconcile --once` は、ローカル台帳で所有が分かり、現在の desired
  state から外れた `linux.ipip6.tunnel`、`nft.table`、`systemd.service` を
  削除する。
- IPv4 fwmark rule は共通 orphan 判定を使う。cleanup は明示的な routerd
  fwmark 範囲に限定している。

次は、各 apply 処理をコマンド別の手続きから artifact 別 reconciler へ移して
いきます。これにより、新しいリソースが増えても cleanup の考え方を統一できます。

## Desired と Observed

`desired` は router YAML から導かれる、そうなっていてほしい状態です。`observed` は
実機から読み取った現在状態です。adopt はこの両方を使います。

実機上に artifact が存在するが台帳にない場合、`adopt --candidates` はそれを候補に
出します。ただし `desired` と `observed` の属性が違う場合、それは単純な所有権の
引き継ぎではありません。運用者が先にどれかを選ぶ必要があります。

- reconcile して実機を YAML に合わせる。
- YAML を修正して desired を実機に合わせる。
- routerd 管理下に入れない。

`adopt --apply` は drift している候補を拒否します。違いが分かっている artifact を
そのまま所有済みとして記録すると、本来判断すべき設定差分を隠してしまうためです。

例:

```json
{
  "kind": "linux.hostname",
  "name": "system",
  "desired": {"hostname": "router03.example.net"},
  "observed": {"hostname": "router03"}
}
```

これは実機 object は存在するが、まだ desired state ではない、という意味です。
adopt する前に reconcile するか、config を修正します。

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

既に設定済みのルーターで初回導入するときの典型的な流れは次の通りです。

1. `routerd plan` で drift を確認する。
2. `routerd adopt --candidates` で取り込み候補を見る。
3. reconcile するか YAML を直して drift をなくす。
4. `routerd adopt --apply` で、既存の一致している artifact を台帳へ記録する。
5. `routerd reconcile --once --dry-run` で想定外の orphan がないことを確認する。
6. `routerd reconcile --once` を実行する。

routerd だけで新規構築したルーターでは、reconcile 成功時に owned artifact を自動的に
台帳へ記録するため、4番は不要なことが多いです。

## Cleanup Policy

routerd は、削除操作の範囲が狭く、所有関係を確認できる artifact だけを破壊的な
cleanup の対象にします。

- 明示的な routerd fwmark 範囲にある `linux.ipv4.fwmarkRule`
- ローカル台帳に記録された `linux.ipip6.tunnel`
- ローカル台帳に記録され、名前が `routerd_*` の `nft.table`
- ローカル台帳に記録され、名前が `routerd-*.service` の `systemd.service`

cleanup の具体的な処理は次の通りです。

- `linux.ipip6.tunnel`: `ip -6 tunnel del <name>` で削除する。
- `nft.table`: ledger-owned かつ `routerd_*` の table だけ
  `nft delete table <family> <name>` で削除する。
- `systemd.service`: ledger-owned かつ `routerd-*.service` の unit だけ
  `systemctl disable --now` で停止・無効化し、対応する
  `/etc/systemd/system/routerd-*.service` を削除して systemd を reload する。

明示的に cleanup しないものもあります。

- `linux.link`: orphan cleanup では削除しません。物理 NIC、hypervisor NIC、VLAN、
  bridge、その他の software link は routerd 外に所有者がいる可能性があります。
- `file`: 管理対象 file を丸ごとは削除しません。安全に触れる可能性があるのは、
  file 内の routerd-owned block だけです。
- `linux.ipv4.address` / `linux.ipv6.address`: address cleanup は別途扱います。
  古い address は別 interface へ移すときに邪魔になりますが、誤って消すと管理経路を
  落とすためです。
- `linux.sysctl` と `linux.hostname`: これは単独で削除できる object ではなく、
  host global state です。desired value へ reconcile できますが、orphan cleanup
  として削除はしません。

長期的なルールは保守的にします。file、service、nftables table、一般的な route
table のような広い種別は、ローカル台帳で所有が分かるまで破壊的には削除しません。
名前や番号の範囲は安全柵として使えますが、それだけを所有の根拠にはしません。

## 実装ルール

リソース種別を追加または変更するときは、次を守ります。

1. そのリソースが作る、または参照する実機 artifact をすべて宣言する。
2. 各 artifact を `ensure`、`observe`、明示的な `delete` のどれにするか決める。
3. 台帳へ記録する前に、実機 inventory と照合できるようにする。
4. 削除範囲が狭く、十分に戻せて、ledger または明示的な routerd 名前空間で所有が
   証明できる場合だけ cleanup を追加する。
5. cleanup するもの、しないものをドキュメントに書く。
6. 既知のリソース種別が artifact intent を出さない場合に落ちるテストを追加する。

このルールは、reconcile を場当たり的な cleanup 処理の集まりにしないためのものです。
目標は、すべての実機 object について、宣言された owner、観測方法、意図的に選ばれた
cleanup policy がある状態にすることです。
