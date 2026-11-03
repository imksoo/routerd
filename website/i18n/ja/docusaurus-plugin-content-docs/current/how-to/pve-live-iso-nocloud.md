---
title: Live ISO の PVE NoCloud bootstrap
---

# Live ISO の PVE NoCloud bootstrap

routerd の Live ISO は Ubuntu の `debootstrap` rootfs から作られており、完全な
`cloud-init` パッケージは入れていません。Proxmox VE のラボノード向けには、
routerd 起動前に必要な最小部分だけをサポートします。`cidata` / `CIDATA`
の config drive にある `user-data` から `hostname`、`routerd.config_url`、
`routerd.config_sha256` を読みます。

これにより、複数 VM が同じ ISO から起動しても、SSH や PVE 検証ログ上で別々の
ホストとして識別でき、完全な routerd config を HTTP や object storage から取得できます。

## user-data

トップレベルの `hostname` と、任意の routerd config pointer を持つ PVE snippet を作ります。

```yaml
#cloud-config
hostname: pve-rt07
routerd:
  config_url: http://10.0.0.10/routerd/pve-rt07/router.yaml
  config_sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
ssh_authorized_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... admin@example
```

VM の cloud-init user-data として接続します。

```sh
qm set 169 --ide2 local:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm reboot 169
```

起動時、Live ISO の setup service は次の順序で処理します。

1. NoCloud user-data の `hostname` を反映する。
2. SSH host key を再生成し、VM ごとの host identity を分離する。
3. `ssh_authorized_keys` を `/root/.ssh/authorized_keys` に配置し、`ssh.service` を有効化する。
4. `ROUTERD_CONFIG` config disk を最優先で試す。
5. config disk がない場合、`routerd.config_url` を `curl` で取得する。
6. `routerd.config_sha256` がある場合は検証する。
7. 取得した `router.yaml` を配置するか、対応する config bundle を展開する。
8. 外部 config がない場合は最後に検証済みの cache、さらに組み込み sample config に fallback する。
9. bootstrap 用の systemd-networkd DHCP profile を削除し、`routerd.service` を起動する。

対応する bundle URL は `.tar.zst`、`.tzst`、`.tar.gz`、`.tgz`、`.tar` です。
bundle には archive root に `router.yaml` が必要です。任意の `secrets/` と
`metadata.json` は `/etc/routerd/` 以下に配置されます。

fetch と checksum 検証が成功すると、配置済みの `router.yaml` は
`/var/lib/routerd/validated-config/router.yaml` に cache されます。次回 boot で
`routerd.config_url` を取得できない場合、Live ISO はこの検証済み cache を復元します。

ISO の既定 DHCP profile は、first boot が `routerd.config_url` に到達するためだけのものです。
config restore 後、setup service は routerd 起動前にこの profile を削除します。以後は
routerd の `DHCPv4Client`、`IPv4StaticAddress`、route resource がネットワークの管理元になります。

## 範囲

これは完全な cloud-init 実装ではありません。Live ISO が NoCloud から使うのは、
早期の hostname identity、root SSH authorized keys、routerd config bootstrap
だけです。user-data の network、user、package 設定や cloud-init module は実行しません。

より大きな bootstrap が必要な場合は、routerd の設定メディアを使うか、Ubuntu Server
をディスクへインストールして通常の cloud-init で管理してください。

cache 回帰試験の `security.capability` 検査には明示的な root 実行が必要です。
一般ユーザーでの通常実行は `user.*` のみを確認します。リポジトリのルートから実行してください。

```sh
cache_test_dir=$(mktemp -d /tmp/routerd-live-cache.XXXXXX)
go test -c -o "$cache_test_dir/cache.test" ./tests/liveiso
(cd tests/liveiso && sudo "$cache_test_dir/cache.test" -test.run '^TestLiveRootfsCacheStoresPortableNumericOwnership$' -test.v -test.timeout 30s)
rm -r -- "$cache_test_dir"
```

実際の cache writer/reader が `security.capability` を保持した旨の出力を確認します。
fixture は実行しないデータファイルで、ホストのネットワークは変更しません。

## 独立クライアントによる起動確認

専用の使い捨て VM を `make live-iso` の正規イメージから起動します。
`tests/fixtures/linux-live-iso-boot.json` を `router.yaml` として読み取り専用の
`ROUTERD_CONFIG` ドライブへ入れてください。1.5 形式で準備したこの設定は
LAN=`ens18`、WAN=`ens19`、管理=`ens20` の3ポートを使います。起動前に MAC と
ポート名の対応を確認し、異なる構成では3つの名前を書き換えて実 CLI の validate を通します。
各ポートは隔離 lab segment に接続し、制御経路には guest agent または serial console を使います。

対向は別の Linux VM とします。LAN/WAN/管理の segment を VLAN 3131/3132/3133 として
渡した trunk で `scripts/linux-router-reboot-fixture.sh --disposable-vm setup-peer TRUNK_IF`
を実行してください。IPv4 peer mode が独立した上流 DNS/HTTP、管理経路の正対照、
BusyBox DHCP client を起動します。ISO router より先に対向を準備します。

guest agent が応答してから120秒以内に setup service 成功、routerd active、
control status Healthy と controller error なしを確認します。canonical config と入力の一致、
bootstrap DHCP 撤去、image commit と実行ファイルの一致も確認し、稼働中の binary 差し替えや
製品状態の手修正で合格にしません。対向の `probe` と `isolation` で DHCP 取得、
ローカル・上流 DNS A（UDP/TCP）、対向が確認した NAT 送信元、管理→LAN の通信と、
WAN→LAN・LAN→管理・WAN→router DNS の遮断を検査します。同じ DHCP process と
アドレスの自然更新、観測中の routerd PID・再起動回数の安定、native deny counter も必要です。

配布物も検査します。executor・plugin manifest の正規配置、一時ディレクトリの1777、
package の setuid file の owner/mode、ping の `security.capability` を確認します。
rootfs cache を使う場合は実際に復元し、前後の数値 owner・mode・capability bytes を比較します。
format marker の存在だけでは属性保存を証明できません。

対象は clean boot と上記 IPv4 経路です。IPv6、実 ISP、永続 disk install、電源断、
全機能・全 OS の合格には広げません。失敗時は原因特定に必要な範囲の出力だけを一時保存し、
製品不具合をローカル再現してから次の lab 試験を設計します。成功時は空設定で撤去し、
peer cleanup、専用 VM・disk・ISO/config media・rootfs cache・build staging の削除まで行います。
残すのは手順と限定した判定結果です。


default route も検査する場合は WAN peer namespace の loopback に `198.18.18.1/32`
を付け、LAN から `http://198.18.18.1:8080/routed-default` にアクセスして NAT 送信元
`198.18.16.2` が返ることを確認します。WAN の直結範囲内への通信だけでは default route を証明できません。

2026-11-03 の限定検証では ISO 製品 source `0ee9777e`、修正した boot fixture `5e5e9a76`、
Ubuntu 24.04 / Linux 6.8.0-147 の2 VM で上記の clean boot と IPv4 経路が通りました。
DHCP の自然更新、default route 経由の転送、配布物 hash 一致、package の native 属性、
空設定による撤去も確認しました。ISO SHA-256 は
`fba24c3b714c8779e608de047d952ad45735fb0a302ade9252e6cc1d62b6a569` です。
初回の raw device 名の書き換え漏れは fixture 準備の失敗で、修正 config drive から
同じ ISO を cold boot して確認しました。成功後は VM・disk・一時 media を削除しました。
