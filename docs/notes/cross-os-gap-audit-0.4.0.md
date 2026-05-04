# Cross-OS Gap Audit for 0.4.0

作成日: 2026-05-04

対象は Phase 2.9 完了後の `main` です。
Ubuntu Server は現在の第一対象です。
このノートでは、Ubuntu で動いている経路を FreeBSD と NixOS へ広げるための差分を整理します。

## 結論

NixOS は Linux カーネル上の機能をかなり再利用できます。
最大の差分は、実行時に `/etc/systemd` や `/etc/systemd/network` を直接書く処理を、NixOS の宣言的な設定へ移す点です。

FreeBSD は土台がありますが、ルーター機能の中核で差分が大きく残っています。
特に nftables、conntrack、iproute2 を前提にした NAT、firewall、通信観測を pf、pfctl、ifconfig、route へ置き換える必要があります。

0.4.0 では、NixOS の宣言的なサービス生成を先に埋める方が投資効果が高いです。
次に FreeBSD の pf 生成器と pf state 観測を実装します。

## 確認したコマンド

| 確認 | 結果 | メモ |
| --- | --- | --- |
| `GOOS=linux GOARCH=amd64 go build ./cmd/...` | 成功 | 主要デーモンと `routerctl` がビルドできます。 |
| `GOOS=freebsd GOARCH=amd64 go build ./cmd/...` | 成功 | 主要デーモンと `routerctl` は FreeBSD 向けにコンパイルできます。 |
| `GOOS=linux GOARCH=amd64 go test ./pkg/render ./pkg/controller/chain ./cmd/routerd ./cmd/routerctl` | 成功 | Ubuntu 側の代表的な生成器と制御経路は通っています。 |
| `GOOS=freebsd GOARCH=amd64 go test -c ./pkg/...` | 成功 | 代表的なパッケージは FreeBSD 向けテストバイナリを生成できます。 |
| `GOOS=freebsd GOARCH=amd64 go test ./pkg/...` | 実行不可 | Linux 上では FreeBSD 用テストバイナリを実行できません。実機または FreeBSD CI が必要です。 |

対象にしたコマンドは次です。

- `cmd/routerd`
- `cmd/routerctl`
- `cmd/routerd-dhcpv4-client`
- `cmd/routerd-dhcpv6-client`
- `cmd/routerd-dns-resolver`
- `cmd/routerd-healthcheck`
- `cmd/routerd-pppoe-client`
- `cmd/routerd-firewall-logger`
- `pkg/render`
- `pkg/platform`
- `pkg/logstore`
- `pkg/controlapi`
- `pkg/controller/chain`
- `pkg/controller/firewall`
- `pkg/controller/nat44`
- `pkg/controller/conntrackobserver`

## プラットフォーム土台

`pkg/platform` は OS ごとの標準パスと機能フラグを持っています。
新しい OS 差分はここに寄せる方針で問題ありません。

| 項目 | Ubuntu | NixOS | FreeBSD | 差分 |
| --- | --- | --- | --- | --- |
| 実行時ディレクトリ | `/run/routerd` | Linux と同じ | `/var/run/routerd` | FreeBSD だけパス差分があります。 |
| 状態ディレクトリ | `/var/lib/routerd` | Linux と同じ | `/var/db/routerd` | FreeBSD のサービス生成で使う必要があります。 |
| サービス管理 | systemd | systemd | rc.d | `SystemdUnit` は FreeBSD では使えません。 |
| firewall | nftables | nftables | pf | FreeBSD 用 pf 生成器が未実装です。 |
| 経路制御 | iproute2 | iproute2 | route、ifconfig | FreeBSD の経路適用が不足しています。 |
| 通信観測 | conntrack | conntrack | pf state | FreeBSD の pf state 取り込みが未実装です。 |

## デーモン

主要デーモンは FreeBSD 向けにコンパイルできます。
ただし、起動管理と一部のソケット機能には OS 差分があります。

| デーモン | Ubuntu | NixOS | FreeBSD | 次の作業 |
| --- | --- | --- | --- | --- |
| `routerd` | systemd で稼働 | NixOS module の土台あり | rc.d の土台あり | NixOS と FreeBSD の生成内容を 0.3 系の引数に追従させます。 |
| `routerd-dhcpv6-client` | systemd で稼働 | router02 で宣言的稼働を確認済み | daemon(8) で稼働確認済み | rc.d と NixOS module の標準化が必要です。 |
| `routerd-dhcpv4-client` | systemd 経路あり | 生成が不足 | rc.d 生成が不足 | NixOS と FreeBSD の起動生成を追加します。 |
| `routerd-dns-resolver` | systemd 経路あり | 生成が不足 | rc.d 生成が不足 | 複数 listen とログ DB のパスを OS ごとに反映します。 |
| `routerd-healthcheck` | systemd 経路あり | 生成が不足 | rc.d 生成が不足 | FreeBSD では ICMP 権限の扱いを分けます。 |
| `routerd-pppoe-client` | pppd 経路あり | 生成が不足 | mpd5/ppp の土台あり | 実運用サービス化は未完了です。 |
| `routerd-firewall-logger` | stdin とファイル入力の土台あり | Linux と同じ | pflog 入力が不足 | NFLOG と pflog の入力実装が必要です。 |

## NixOS の差分

NixOS は Linux カーネルを使うため、nftables、conntrack、iproute2、WireGuard、VXLAN は再利用できます。
一方で、OS の状態は NixOS 設定から作るべきです。
実行時に drop-in を直接書く処理は長期的には合いません。

| 項目 | 現状 | 差分 |
| --- | --- | --- |
| パッケージ | `Package` は `nix` manager を実行時に扱えません。 | NixOS では `environment.systemPackages` へ生成する経路を主にします。 |
| systemd サービス | `contrib/nix/module.nix` は `routerd` 本体中心です。 | DNS resolver、DHCP client、HealthCheck、PPPoE、event relay を生成対象にします。 |
| NetworkAdoption | Ubuntu 向け drop-in を書きます。 | `systemd.network.networks` や generated module へ変換します。 |
| SysctlProfile | NixOS renderer に `boot.kernel.sysctl` の土台があります。 | router 用既定値と個別指定を生成内容へ統合します。 |
| DNS resolver | バイナリは動かせます。 | NixOS service とログ DB のディレクトリ作成が不足しています。 |
| dnsmasq | Linux と同じバイナリを使えます。 | DHCP サーバー用途の NixOS service 生成が不足しています。 |
| firewall/NAT | Linux 実装を使えます。 | nftables 設定の適用主体を routerd 実行時にするか、NixOS 生成に寄せるかを決めます。 |
| ログ DB | SQLite 自体は OS 非依存です。 | `StateDirectory` 相当を NixOS module で宣言する必要があります。 |

NixOS の第一歩は、既存の `Package`、`SystemdUnit`、`NetworkAdoption` を NixOS module へ変換する生成器です。
実行時に `nix profile` を変更するより、生成された NixOS 設定を `nixos-rebuild test` で確認する方が安全です。

## FreeBSD の差分

FreeBSD は Linux 用の nftables と conntrack を使えません。
ルーターの中核は pf と pf state へ移す必要があります。

| 項目 | 現状 | 差分 |
| --- | --- | --- |
| パッケージ | `Package` は `pkg` manager を実行時に扱えません。 | `pkg info` と `pkg install -y` の実装が必要です。 |
| サービス | `contrib/freebsd/routerd` は本体用です。 | DNS resolver、DHCP client、HealthCheck、PPPoE、firewall logger の rc.d が必要です。 |
| SysctlProfile | 実行時 `sysctl` は使えます。 | `/etc/sysctl.conf` などの永続化が未整理です。 |
| firewall | `platform.Features.HasPF` はあります。 | `FirewallZone`、`FirewallPolicy`、`FirewallRule` の pf 生成器がありません。 |
| NAT44 | nftables 生成のみです。 | `nat on ... from ... to any -> (...)` の pf 生成器が必要です。 |
| conntrack 観測 | Linux conntrack 前提です。 | `pfctl -ss` を解析する観測器が必要です。 |
| firewall log | Linux NFLOG 前提の設計です。 | `pflog0` または `tcpdump -n -e -ttt -i pflog0` 入力の取り込みが必要です。 |
| DS-Lite | Linux は `ip -6 tunnel` 前提です。 | FreeBSD では gif または ifconfig tunnel を使う設計が必要です。 |
| DNS resolver | Go 実装は動かせます。 | `viaInterface` は Linux の `SO_BINDTODEVICE` に依存しています。FreeBSD では未強制です。 |
| DHCP server | dnsmasq は使えます。 | rc.d と設定ファイル所有を整理する必要があります。 |

FreeBSD は 0.4.0 で pf 生成と pf state 観測を先に入れると、通信の可視化と安全性を同時に進められます。
DS-Lite と PPPoE の FreeBSD 実適用は、その後に分ける方が安全です。

## ログストレージと観測

Phase 2.9 のログ DB は SQLite なので OS 依存は小さいです。
差分は入力元とサービス起動です。

| ログ | Ubuntu | NixOS | FreeBSD | 差分 |
| --- | --- | --- | --- | --- |
| DNS query | `routerd-dns-resolver` が直接書き込みます。 | 同じ実装を使えます。 | 同じ実装を使えます。 | サービス生成と状態ディレクトリだけが差分です。 |
| traffic flow | conntrack 観測から書き込みます。 | conntrack-tools があれば同じです。 | pf state 観測が必要です。 | FreeBSD 用 parser が必要です。 |
| firewall log | NFLOG 入力が前提です。 | Linux と同じ方向です。 | pflog 入力が必要です。 | 入力デーモンを分ける必要があります。 |
| retention | OS 非依存です。 | 同じです。 | 同じです。 | パスだけ `platform` に従います。 |

## 依存パッケージ

依存パッケージの宣言は Ubuntu と NixOS ではかなり揃っています。
FreeBSD は Linux の package 名を流用できないため、専用の一覧にする必要があります。

| 分類 | Ubuntu | NixOS | FreeBSD |
| --- | --- | --- | --- |
| DHCP/DNS | `dnsmasq-base`, `dnsutils` | `dnsmasq`, `bind` | `dnsmasq`, `bind-tools` |
| firewall/NAT | `nftables`, `conntrack` | `nftables`, `conntrack-tools` | base の `pfctl`, `tcpdump` |
| 経路 | `iproute2` | `iproute2` | base の `route`, `ifconfig` |
| VPN | `wireguard-tools`, `strongswan-swanctl` | `wireguard-tools`, `strongswan` | `wireguard-tools`, `strongswan` |
| PPPoE | `ppp` | `ppp` | `mpd5` または base ppp |
| 診断 | `tcpdump`, `traceroute`, `net-tools` | `tcpdump`, `traceroute`, `nettools` | base の `tcpdump`, `traceroute`, `netstat`, `sockstat` |

Step 2 では `Package` Kind を次の扱いに分けます。

- Ubuntu は `apt` で実行時に確認と導入を行います。
- NixOS は実行時導入ではなく、生成される NixOS 設定に反映します。
- FreeBSD は `pkg info` と `pkg install -y` を実装します。

## 0.4.0 の実装順序案

### 3.0a NixOS の最小対応

最初に NixOS を進めます。
Linux 実装を再利用できるため、効果が早く出ます。

- `Package` を NixOS 生成へ接続します。
- `SystemdUnit` を NixOS の `systemd.services` へ変換します。
- `NetworkAdoption` を NixOS の systemd-networkd 設定へ変換します。
- `routerd-dns-resolver`、DHCP client、HealthCheck、event relay のサービス生成を追加します。
- `examples/nixos-edge.yaml` を 0.3 系の構成へ更新します。

### 3.0b FreeBSD のパッケージと rc.d

次に FreeBSD の OS 操作の土台を埋めます。

- `Package` に `pkg` manager を追加します。
- `SysctlProfile` の FreeBSD 永続化を追加します。
- `SystemdUnit` に相当する rc.d service 生成 Kind を追加するか、`ServiceUnit` へ一般化します。
- DNS resolver、DHCP client、HealthCheck、event relay の rc.d テンプレートを追加します。

### 3.0c FreeBSD pf 生成器

FreeBSD のルーター機能の中心です。

- `FirewallZone`、`FirewallPolicy`、`FirewallRule` から pf ルールを生成します。
- `IPv4SourceNAT` と NAT44 を pf の nat/rdr へ変換します。
- ICMPv6 と established 相当の許可を pf で表現します。
- dry-run で `pfctl -nf` を使えるようにします。

### 3.0d FreeBSD の通信観測

Web Console と `routerctl show traffic` の移植に必要です。

- `pfctl -ss` の出力を traffic flow へ変換します。
- pf state の方向、NAT 前後、プロトコル、バイト数を OTel semantic conventions に近い列へ入れます。
- `routerd-firewall-logger` に pflog 入力を追加します。

### 3.0e FreeBSD のトンネルと WAN

最後に FreeBSD の WAN 実適用を広げます。

- DS-Lite を gif または ifconfig tunnel で生成します。
- PPPoE の mpd5 経路を実機で確認します。
- DNS resolver の `viaInterface` を FreeBSD でどう強制するかを決めます。

## 未決事項

`SystemdUnit` は名前が Linux に寄りすぎています。
FreeBSD と NixOS を同じ設定で扱うなら、将来は `ServiceUnit` へ一般化する方が自然です。
ただし、0.4.0 では既存 Kind を壊さず、OS ごとの生成器を増やす方が移行リスクは低いです。

NixOS の `Package` は実行時導入ではなく宣言的な生成に寄せるべきです。
これは Ubuntu の `apt` 実装とは意味が違います。
そのため status では `Applied` ではなく `Rendered` や `Declarative` の phase を使う案があります。

FreeBSD の `viaInterface` は Linux の `SO_BINDTODEVICE` と同じ実装がありません。
経路表、jail、またはソケット bind のどれで表現するかは追加調査が必要です。

## Step 2 の入口

Step 2 は NixOS から始めるのが妥当です。
理由は次の通りです。

- Linux カーネル機能を流用できます。
- 実装対象が生成器とサービス定義に寄っています。
- router02 で DHCPv6-PD の実績があります。
- FreeBSD の pf 移植より破壊範囲が小さいです。

実装は `pkg/platform` と既存 renderer に寄せます。
`runtime.GOOS` の直接参照は増やしません。
