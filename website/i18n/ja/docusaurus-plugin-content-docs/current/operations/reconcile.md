---
title: Reconcile と削除
---

# Reconcile と削除

routerd は、YAML が宣言する意図とホスト現在状態を比べます。
差があれば計画 (plan) を作り、必要なら dry-run で確認してから apply します。

## 標準シーケンス

```bash
routerd validate --config router.yaml
routerd plan     --config router.yaml
routerd apply    --config router.yaml --once --dry-run
routerd apply    --config router.yaml --once
```

遠隔ルーターでは、本番 `apply` の前に管理経路 (SSH、コンソール、ハイパーバイザーコンソール) が変更を生き残ることを確認してください。

## 常駐モード

```bash
routerd serve --config router.yaml
```

serve モードでは、bus 上のイベントに反応して影響範囲のリソースだけを再評価します。
入力は DHCPv6-PD renewal、health check 結果、derived event、inotify による設定変更検知などです。

controller dry-run flag は所有範囲ごとに効きます。
`--controller runtime-dry-run-ingress=false` は IngressService controller の live health
selection と、IngressService 由来の nftables DNAT/hairpin rule の live apply を意味します。
独立した `NAT44Rule`、`IPv4SourceNAT`、`LocalServiceRedirect` は引き続き
`--controller runtime-dry-run-nat=false` で別に制御します。

`IngressService`、`PortForward`、NAT、BGP、static/policy route など転送を伴う
resource がある場合、apply と controller reconcile は runtime kernel forwarding
も収束させます。具体的には `net.ipv4.ip_forward=1` と
`net.ipv6.conf.all.forwarding=1` を適用します。明示的な `SysctlProfile` がない
Live ISO や初回起動直後の router でも、forwarding disabled のまま silently 動く
状態を避けるためです。

## drift の確認

routerd は、状態データベースだけを唯一の正として扱いません。
状態ストアには前回の apply で観測した内容を記録しますが、各 controller は
処理を省略する前に、自分が管理する実機状態も確認します。
たとえば systemd unit の enabled/active 状態、dnsmasq が期待した設定ファイルで
動いているか、DHCPv4 lease のアドレスがインターフェース上に残っているか、
管理対象の nftables table が実機に存在するかを見ます。

これは再起動後、手作業の変更に失敗した後、upgrade が途中で止まった後に重要です。
状態データベース上は Applied のままでも、OS 側の状態がずれていることがあります。
controller は前回の status 行をそのまま信じるのではなく、宣言された YAML へ
OS 状態を収束させます。

## 派生 resource

一部の host object は YAML に直接書かせず、より高レベルの intent から生成します。
たとえば `routerd.service`、`routerd-healthcheck@*.service`、firewall log daemon、
DPI helper service は派生 service unit です。生成された resource は次で確認できます。

```bash
routerctl show derived-resources
```

削除済みまたは未対応の resource kind が YAML に残っている場合、routerd は黙って
無視せず config load を失敗させます。

## 管理対象の掃除

YAML からリソースが消えた場合、所有元の controller は自分が所有する構成物だけを
削除または無効化します。
対応する `HealthCheck` がなくなった `routerd-healthcheck@*.service` は
disable して削除します。
NAT44 rule が 0 件になった場合は、管理対象の `routerd_nat` table または
pf anchor を空にします。
`state: absent` の `generated service artifacts` は、render 済み unit を削除し、unit が存在するか
まだ enabled/active の場合だけ停止します。

firewall の rendering では、管理対象の nftables table は維持したまま、1 回の
`nft -f` batch で再読み込みします。
firewall zone の interface set や client-policy の MAC set のような named set は、
再定義の前に routerd が管理対象 set だけを destroy します。
これにより削除済みの要素が残らず、通常の apply で filter table 全体を
destroy/recreate することもありません。

## 削除

routerd は所有を確認できる artefact (routerd が以前作成、または明示的に adopt したもの) のみ削除します。
第三者構成や手動変更は触りません。

過去の設定への完全 rollback は現状の対象外です。
削除を含む変更の場合は、必ず `routerd plan` と `routerd apply --dry-run` で削除リストを確認してから適用してください。

## 関連項目

- [状態と所有権](../concepts/state-and-ownership.md)
- [Apply と render](../concepts/apply-and-render.md)
- [トラブルシューティング](../how-to/troubleshooting.md)
