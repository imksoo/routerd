---
title: FreeBSD から始める
---

# FreeBSD から始める

![リリースアーカイブの導入から rc.d、rc.conf.d、pf、dnsmasq、mpd5 の生成と適用検証へ進む FreeBSD 入門の流れ](/img/diagrams/tutorial-freebsd-getting-started.png)

FreeBSD は、Ubuntu と同じ routerd リソースモデルを使います。ただし Ubuntu と
同じ初回ラボの対応範囲ではなく、選択されたネイティブ連携を持つ第 2 対象です。
最初はコンソールを使える隔離テストホストで試してください。生成されるホスト成果物は
FreeBSD の機構に合わせます。
routerd は、`rc.conf.d`、`rc.d` スクリプト、`pf.conf`、`dhclient.conf`、
dnsmasq 設定、`mpd5.conf`、そして DS-Lite 用の動的な `ifconfig gif` 操作を扱います。

このチュートリアルは、FreeBSD 14 系を前提とします。
リリースインストーラーの配置先は `/usr/local` 配下です。
参照設定として `examples/freebsd-edge.yaml` を使います。

## 1. リリースアーカイブから導入する

[GitHub Releases](https://github.com/imksoo/routerd/releases) から FreeBSD 用の
アーカイブを取得し、ルーター上で同梱のインストーラーを実行します。

```sh
fetch https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-freebsd-amd64.tar.gz
fetch https://github.com/imksoo/routerd/releases/download/v20260707.1514/routerd-freebsd-amd64.tar.gz.sha256
cat routerd-freebsd-amd64.tar.gz.sha256
sha256 routerd-freebsd-amd64.tar.gz
tar -xzf routerd-freebsd-amd64.tar.gz
sudo ./install.sh
```

`install.sh` は、通常必要になる FreeBSD パッケージを導入します。
対象は `ca_root_nss`、`curl`、`dnsmasq`、`wireguard-tools`、`mpd5`、
`bind-tools`、`tcpdump`、`jq`、`chrony`、`strongswan` です。
Tailscale もあわせて入れる場合は `sudo ./install.sh --with-tailscale` を使います。
FreeBSD の基本システムには、`ifconfig`、`route`、`sysctl`、`service`、`sysrc`、
`pfctl`、`pflog0`、`netstat`、`sockstat`、`ping`、`traceroute` があります。
依存パッケージの一覧は `./install.sh --list-deps` で確認できます。

## 2. ルーター設定を配置する

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 examples/freebsd-edge.yaml /usr/local/etc/routerd/router.yaml
```

適用する前に、インターフェース名、アドレス、秘密値を編集してください。
初回は、管理用の SSH を別のインターフェースに置くか、ハイパーバイザーのコンソールを用意しておいてください。

## 3. 検証し、生成ファイルを確認する

まず設定を検証します。

```sh
sudo routerd validate --config /usr/local/etc/routerd/router.yaml
```

次に、FreeBSD 用の成果物を一時ディレクトリへ生成します。

```sh
render_dir=$(mktemp -d)
sudo routerd render freebsd \
  --config /usr/local/etc/routerd/router.yaml \
  --out-dir "$render_dir"
```

主な出力は次のとおりです。

- `rc.conf.d-routerd`
- `dhclient.conf`
- `mpd5.conf`
- `pf.conf`
- `dnsmasq.conf`
- `install-packages.sh`
- `rc.d-*`

実ホストへ反映する前に、内容を確認します。

```sh
sudo less "$render_dir/rc.conf.d-routerd"
sudo less "$render_dir/pf.conf"
sudo less "$render_dir/dnsmasq.conf"
```

## 4. FreeBSD 側の役割を理解する

routerd は、リソースを次の FreeBSD の機構へ対応付けます。

| 機構 | 役割 |
| --- | --- |
| `rc.conf.d-routerd` | インターフェース別名、転送、複製インターフェース、静的経路、`pf`、`pflog`、`mpd5` の有効化 |
| `rc.d-*` スクリプト | dnsmasq、firewall logger、healthcheck、Tailscale、DHCP クライアントなどの管理対象デーモン |
| `pf.conf` | ゾーンフィルター、管理対象サービス用の許可、NAT、ファイアウォールログ |
| `pflog0` | `routerd-firewall-logger` が読むファイアウォールログ |
| `dnsmasq.conf` | DHCPv4、DHCPv6、DHCP 中継、RA |
| `dhclient.conf` | 引き継いだ上流インターフェースの DHCPv4 クライアント動作 |
| `mpd5.conf` | PPPoE の bundle、link、認証、MTU/MRU、既定経路 |
| `ifconfig gif` | 静的な `rc.conf` だけでは足りない DS-Lite トンネルの動的な適用 |

## 5. dry-run の後、コンソールから適用する

使い捨ての state ファイルで、ネットワークを変更しない確認を行います。

```sh
workdir=$(mktemp -d)
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once --dry-run --skip-service-manager \
  --state-file "$workdir/state.db" \
  --ledger-file "$workdir/ledger.db" \
  --status-file "$workdir/status.json"
```

生成ファイルと dry-run が想定どおりなら、コンソールまたは独立した管理経路から
適用します。次のコマンドはホストを変更します。

```sh
sudo routerd apply --config /usr/local/etc/routerd/router.yaml --once
```

routerd は、`pf.conf` を読み込む前に `pfctl -nf` で検証します。
dnsmasq も、再起動の前に `dnsmasq --test` で検証します。
one-shot apply が正常なら rc.d サービスを起動します。

```sh
sudo sysrc routerd_enable=YES
sudo service routerd start
```

## 6. 状態とログを確認する

routerd の状態を確認します。

```sh
sudo routerctl get status
sudo routerctl get events --limit 20
```

システムログを追います。

```sh
tail -f /var/log/routerd.log
```

pf の状態を確認します。

```sh
sudo pfctl -ss -v
```

`pflog0` でファイアウォールログを確認します。

```sh
sudo tcpdump -n -e -ttt -i pflog0
```

`FirewallEventLog` を有効にすると、routerd は `pflog0` の内容を取り込みます。
取り込んだログは、`routerctl` と Web 管理画面から確認できます。

## 関連項目

- [対応プラットフォーム](../platforms.md)
- [WAN 側サービス](./wan-side-services.md)
- [基本ファイアウォール](./basic-firewall.md)
