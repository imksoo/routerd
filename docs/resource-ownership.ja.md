---
title: リソース所有
slug: /reference/resource-ownership
---

# リソース所有と反映モデル

routerd はルータのカーネルネットワーク状態、dnsmasq の設定、nftables のテーブル、systemd ユニットなど、ホスト側の構成物を変更します。それを安全に行うために、ホスト上のひとつひとつの構成物について次の 3 点をはっきりさせておく必要があります。

1. この構成物は、どの routerd リソースが所有しているのか。
2. それを routerd が作ったのか、明示的に取り込んだのか。
3. リソースが YAML から消えたとき、その構成物をホストから削除してよいのか。

このページは、その 3 点に対する routerd の答えを説明します。ローカル所有台帳は 2 番目に対する永続的な記録であり、単なるキャッシュではありません。破壊的なクリーンアップを安全に行うための判断材料そのものです。

## 真実の元 (source of truth)

routerd は、ファイルを真実の元としてルーターへ反映する仕組みです。基本の操作経路は
次のとおりです。

1. YAML 設定を変更する。
2. 検証と計画表示で内容を確認する。
3. `routerd apply --once` を実行する。常駐デーモンも同じ反映経路を使う。
4. 必要に応じて `routerctl describe` / `routerctl show`、サービス状態、
   パケット取得で確認する。

これは意図的な設計制約です。運用者が何をしようとしたのかは、git の履歴、
ファイル差分、apply の出力、イベント、ローカル状態データベースから追えるべきです。
そのため、生成ファイル、サービス再起動、DHCP クライアントの挙動、インターフェース
変更は、ルータ設定として残す限り routerd のリソースと apply 経路を通します。

一方で、サービスの監視と起動停止は OS の役割です。routerd は具体的な操作に、
systemd ホストでは `systemctl`、FreeBSD では `service` / rc.d を使います。
デーモンへ HUP などのシグナルを直接送る方法は切り分け用の一時的な観察であり、
設定変更の手順ではありません。調査中に直接シグナルを使った場合でも、最後は YAML と
`routerd apply` で同じ状態を再現してから有効な結果として扱います。

## 反映の流れ

1 回の反映処理は次の手順で進みます。

1. ホストの現在の構成を読み出す。
2. routerd の各リソースが「この構成物が存在してほしい」「この構成物を観測したい」という *管理意図* を 1 件以上出す。
3. 望む状態と観測状態を比較する。
4. 一致しているものは維持する。
5. 足りないもの、ずれているものは作成または更新する。
6. routerd の管理対象とわかっていて、現在のリソースから意図が出ていない構成物は残置物として削除する（できるものだけ）。

これは Kubernetes のコントローラが取っている所有モデルに近い考え方です。Kubernetes では、生成されたオブジェクトに owner reference が付き、削除前のクリーンアップは finalizer で行い、フィールド単位の所有者は Server-Side Apply で記録します。routerd には API サーバが無いので、所有モデルをリコンサイラとディスク上の JSON 台帳で表現しています。

## 反映失敗時の考え方

routerd は、ホスト上のすべての操作を「全部成功するか、全部戻すか」の一括取引として扱う約束はしません。Linux の経路表、DHCP のリース、nftables の状態、systemd ユニット、FreeBSD の rc.conf は、ひとつの共通した取引管理機構の上にありません。そこを無理に一括取引のように見せると、失敗時の理解が難しくなります。

代わりに、routerd は次の 3 つを素直に扱います。

- **今回の反映**: 1 回の反映処理で実行しようとした作業。
- **所有台帳**: routerd が作った、または取り込んだ構成物のローカル記録。
- **保護された管理経路**: SSH やローカル制御 API のために残すべきインターフェースやファイアウォールゾーン。

トップレベルの `spec.reconcile` で、反映時の厳しさを選べます。

```yaml
spec:
  reconcile:
    mode: progressive
    protectedInterfaces:
      - mgmt
    protectedZones:
      - mgmt
```

`mode: strict` は、どこかで失敗したらそこで止まります。`mode: progressive` は、独立して進められる段階をできるだけ続け、失敗した段階を警告として残し、結果を `Degraded` にします。途中で失敗した場合、破壊的な残置物削除と新しい所有台帳への記録は行いません。部分的な反映を「きれいに確定した世代」と誤認しないためです。

保護されたインターフェースとゾーンは、安全上の支点です。「絶対に触らない」という意味ではありません。運用者がルータを直すために使う経路を、気軽に消したり塞いだりしないという意味です。ファイアウォールの出力では、保護ゾーンからの SSH を常に開けます。将来の残置物削除や巻き戻し処理も、保護された構成物をまず残す前提で扱います。

運用上の規則は単純です。管理経路を残し、安全に進められる独立した変更は進め、失敗したデータ転送側の作業は次の plan や反映で見える状態に残します。

## 構成物の管理意図

管理意図は、YAML のリソースとホスト上の構成物をつなぐ宣言です。

- `kind`: 構成物の安定した種別。たとえば `linux.ipv4.fwmarkRule` や `nft.table`。
- `name`: その種別内での安定した識別子。
- `owner`: 意図を出した routerd リソース ID。
- `action`: `ensure`、`delete`、`observe` のいずれか。
- `applyWith`: 差分を是正できるレンダラやコマンド系統。

リソース実装は、対応する管理意図を出さずにホスト上の状態をこっそり作ってはいけません。新しいリソース種別がホスト側の新しい構成物を扱うようになったときは、管理意図一覧とこのドキュメントをまとめて更新します。

`action` の意味:

- `ensure`: routerd はこの構成物が存在することを望み、必要があれば管理する。
- `observe`: 検出や別名解決のために観測するだけで、削除側の所有はしない。
- `delete`: 明示的な削除フロー用に予約済み。

台帳に記録するのは、ホスト在庫と照合できる `ensure` の構成物のみです。あとから安定した識別子で照合できない構成物まで台帳に残すと、所有関係を誤判定する原因になります。

たとえば `IPv4DefaultRoutePolicy` の候補は次を所有します。

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_default_route`

`IPv4PolicyRouteSet` は次を所有します。

- `linux.ipv4.fwmarkRule`
- `linux.ipv4.routeTable`
- `nft.table/routerd_policy`

## 残置物

次の条件をすべて満たす構成物を残置物として扱います。

- routerd 管理対象の名前空間または番号範囲に含まれる。
- ホスト上に存在する。
- 現在の routerd リソースのどれもこの構成物に意図を出していない。

残置物の判断材料は 2 種類あります。

- **名前空間 / 範囲による判断**: fwmark `0x100-0x1ff` や `routerd_*` という名前の nftables テーブルなど、routerd 用と決めた範囲にある。
- **台帳による判断**: `/var/lib/routerd/routerd.db` の `artifacts` テーブル に、過去に routerd リソースの所有物として記録されている。

判断材料としては台帳のほうが強力です。名前空間や番号範囲は安全柵として有用ですが、それだけで広い範囲の破壊的クリーンアップを許すには弱いです。`routerd_foo` というテーブルはおそらく routerd 由来ですが、台帳に載っているということは、この routerd インストール自身がその構成物の所有を受け入れた証拠になります。

Linux のポリシールーティングについては、現状 fwmark `0x100-0x1ff` を routerd 管理範囲として扱います。削除済みの DS-Lite ルートセットがこの範囲に古い rule を残していたら、反映時に取り除き、参照していたルーティングテーブルがどのリソースからも望まれていなければそのテーブルもフラッシュします。

routerd 管理範囲外の構成物は、台帳で routerd の所有が確認できるまでは外部のものとして扱います。長期的なルールとして、ファイル、サービス、nftables テーブル、汎用ルーティングテーブルといった広い種別の破壊的削除は、名前一致だけではなく台帳に基づく所有確認を必須にします。

## 望む状態と観測状態

望む状態（desired）は YAML から導かれる「こうあってほしい」の状態、観測状態（observed）はホストから読み取った現状です。取り込みはこの両方を使います。

ホスト上に構成物があるが台帳には無いとき、`routerd adopt --candidates` がその差を候補として表示します。望む状態と観測状態の属性が違う場合、それは単純な所有権の引き継ぎではなく、運用者が次のいずれかを先に選ぶ必要があります。

- 反映してホストを YAML に合わせる。
- YAML を直して望む状態をホストに合わせる。
- routerd の管理下に入れない。

`adopt --apply` は、観測値が望む状態と違う候補を拒否します。違いがわかっている構成物をそのまま所有として記録すると、本来判断すべき設定差分を隠してしまうためです。

差分のある候補の例:

```json
{
  "kind": "host.hostname",
  "name": "system",
  "desired": {"hostname": "router03.example.net"},
  "observed": {"hostname": "router03"}
}
```

ホスト上の構成物は存在しますが、まだ望む状態にはなっていません。所有を記録する前に、差分を解消します。

## 対応しているリソース

現在ある全リソース種別は、自分が観測または管理するホスト上の構成物を必ず宣言します。既知のリソース種別が管理意図を 1 件も出さない場合、単体テストで失敗します。

| リソース | ホスト上の構成物 |
| --- | --- |
| `LogSink` | routerd のログ出力先 |
| `Sysctl` | ホストの sysctl キー |
| `NTPClient` | systemd-timesyncd の設定 |
| `Interface` | ネットワークリンク |
| `PPPoEInterface` | PPP インターフェース、Linux の PPPoE systemd ユニットと PPP secret ファイル、または FreeBSD の mpd5 設定と `mpd5` サービス |
| `IPv4StaticAddress` | IPv4 アドレス |
| `IPv4DHCPAddress` | DHCPv4 クライアントのバインディングと、出力先ごとの経路/DNS 採用設定 |
| `IPv4DHCPServer` | dnsmasq の設定とサービス。Linux では `routerd-dnsmasq.service`、FreeBSD では `/usr/local/etc/rc.d/routerd_dnsmasq` を使う |
| `IPv4DHCPScope` | dnsmasq の DHCPv4 スコープ |
| `DHCPv4HostReservation` | dnsmasq の DHCPv4 固定割当 |
| `IPv6DHCPAddress` | DHCPv6 クライアントのバインディング |
| `IPv6PrefixDelegation` | DHCPv6 プレフィックス委譲のバインディング。FreeBSD KAME `dhcp6c` で NTT 系リンクレイヤ DUID を使う場合は DUID ファイル |
| `IPv6DelegatedAddress` | IPv6 アドレス。FreeBSD では、現在観測できている委譲プレフィックスから LAN 側の別名アドレスを追加する |
| `IPv6DHCPServer` | dnsmasq の設定とサービス。Linux では `routerd-dnsmasq.service`、FreeBSD では `/usr/local/etc/rc.d/routerd_dnsmasq` を使う |
| `IPv6DHCPScope` | dnsmasq の DHCPv6 スコープ |
| `SelfAddressPolicy` | routerd の自分自身のアドレス選定方針 |
| `DNSConditionalForwarder` | dnsmasq の条件付き転送設定 |
| `DSLiteTunnel` | Linux の IP-in-IPv6 トンネル |
| `HealthCheck` | routerd スケジューラの到達性チェック |
| `IPv4DefaultRoutePolicy` | nftables の mark テーブル、IPv4 ルーティングテーブル、IPv4 fwmark ルール |
| `IPv4SourceNAT` | nftables の NAT テーブル |
| `IPv4PolicyRoute` | IPv4 ルーティングテーブルと fwmark ルール |
| `IPv4PolicyRouteSet` | nftables の policy テーブル、IPv4 ルーティングテーブル、IPv4 fwmark ルール |
| `IPv4ReversePathFilter` | Linux の rp_filter sysctl キー |
| `PathMTUPolicy` | nftables の MSS テーブル、dnsmasq の RA MTU オプション |
| `Zone` | routerd のファイアウォールゾーン |
| `FirewallPolicy` | nftables の filter テーブル |
| `ExposeService` | nftables の DNAT テーブル |
| `Hostname` | システムのホスト名 |

## 取り込みの手順

取り込み（adopt）は、「この構成物はホストに既にある」という状態を「routerd が所有している」状態へ整える操作です。古い routerd や手作業で作られた可能性がある構成物を routerd の管理下に置く前に使います。

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --candidates
```

このコマンドは読み取り専用です。設定上は必要で、ホストにも存在するが、`/var/lib/routerd/routerd.db` の `artifacts` テーブル にまだ記録されていない構成物を一覧します。

候補が正しく、観測値と望む状態に差分がないことを確認できたら、台帳に記録します。

```sh
sudo routerd adopt \
  --config /usr/local/etc/routerd/router.yaml \
  --apply
```

`adopt --apply` はカーネル、nftables、systemd、ファイル状態を変更しません。所有関係の台帳だけを書き換えます。差分のある候補があれば、先に反映または設定の修正を行ってからもう一度実行します。

試行実行ではない `routerd apply` が成功した場合も、routerd は在庫照合できる所有済みの構成物を台帳に記録します。安定した識別子に紐付けられない派生構成物は、意図的に台帳の対象から外しています。

すでに設定済みのルータで初回導入するときの典型的な流れ:

1. `routerd plan` で差分を確認する。
2. `routerd adopt --candidates` で取り込み候補を見る。
3. 反映するか YAML を直して差分を解消する。
4. `routerd adopt --apply` で、一致している構成物を台帳に記録する。
5. `routerd apply --once --dry-run` で想定外の残置物が無いことを確認する。
6. `routerd apply --once` を実行する。

routerd だけで新規構築したルータでは、反映成功時に所有済みの構成物を自動で台帳に記録するため、ステップ 4 が不要になることが多いです。

## クリーンアップ方針

routerd は、削除操作の範囲が狭く、所有を確認できる構成物だけを破壊的なクリーンアップ対象にします。

- 明示的な routerd fwmark 範囲にある `linux.ipv4.fwmarkRule`。
- ローカル台帳に記録された `linux.ipip6.tunnel`。
- ローカル台帳に記録され、名前が `routerd_*` の `nft.table`。
- ローカル台帳に記録され、名前が `routerd-*.service` の `systemd.service`。

クリーンアップの中身:

- `linux.ipip6.tunnel`: `ip -6 tunnel del <name>` で削除。
- `nft.table`: 台帳に載っていて `routerd_*` のテーブルだけ `nft delete table <family> <name>` で削除。
- `systemd.service`: 台帳に載っていて `routerd-*.service` のユニットだけを対象に、`systemctl disable --now` で停止・無効化し、対応する `/etc/systemd/system/routerd-*.service` を削除して、最後に systemd を再読み込みします。

意図的に残置物クリーンアップしないもの:

- `net.link`: 物理 NIC、ハイパーバイザの NIC、VLAN、ブリッジ、その他のソフトウェアリンクは routerd 外の所有者を持ちうるため、削除しません。
- `file`: 管理対象ファイルを丸ごと削除はしません。安全に触れる可能性があるのは、ファイル内の routerd 所有ブロックだけです。
- `net.ipv4.address` / `net.ipv6.address`: アドレスのクリーンアップは別建てにしています。古いアドレスは別インターフェースへ移すときに邪魔になりますが、誤って消すと管理経路を落とすためです。
- `host.sysctl` と `host.hostname`: ホストのグローバルな状態であり、単独で削除できる構成物ではありません。望む値への反映はできますが、残置物として削除はしません。

長期方針は保守的に取ります。ファイル、サービス、nftables テーブル、汎用ルーティングテーブルなど広い種別では、台帳で所有が証明できる場合だけ削除します。名前や番号の範囲は補助的な安全柵として使い、それだけで長期の所有判定にはしません。

## 実装ルール

リソース種別を追加または変更するときは次を守ります。

1. そのリソースが作る、または参照するホスト上の構成物をすべて宣言する。
2. 各構成物に対して `ensure` / `observe` / 明示的 `delete` のどれを使うか決める。
3. 台帳に記録する前に、ホスト在庫と照合する手段を用意する。
4. 削除範囲が狭く、十分に戻せて、台帳または明確な routerd 名前空間で所有を証明できる場合に限ってクリーンアップを追加する。
5. クリーンアップする条件と、しない条件の両方をドキュメントに書く。
6. 既知のリソース種別が管理意図を出さないと落ちるテストを追加する。

これらのルールは、反映処理が場当たり的なクリーンアップ判断の寄せ集めにならないようにするためです。最終的な目標は、ホスト上の各構成物に、宣言された所有者、観測手段、意図して選ばれたクリーンアップ方針が必ず存在する状態を保つことです。
