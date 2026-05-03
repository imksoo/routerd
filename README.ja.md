# routerd

[プロジェクトサイトとドキュメント: routerd.net](https://routerd.net/)

routerd は、ルーターの意図を YAML の型付きリソースとして書くための
宣言的な制御ソフトウェアです。routerd はその意図を、ローカルデーモン、
生成設定、カーネルネットワーク状態、観測状態へ反映します。
現在はプレリリースです。pve05 から pve07 上の Ubuntu、NixOS、FreeBSD VM を中心に、
IX2215 置き換えに必要な機能を小さな部品から作り直しています。

現在の本線は OS の DHCPv6 生成器ではなく、管理対象デーモンです。

- `routerd-dhcpv6-client`: DHCPv6-PD と Information-request
- `routerd-dhcpv4-client`: WAN DHCPv4 リース
- `routerd-pppoe-client`: PPPoE セッション
- `routerd-healthcheck`: 別プロセスの疎通確認

routerd 本体はデーモンイベントを bus と SQLite イベント保存先へ流します。
コントローラーチェーンは、LAN アドレス、DNS、RA、DS-Lite へ反映します。
IPv4 経路、NAT44、WAN 出口選択も同じ流れで扱います。

## 現在できること

- Interface / Link / Bridge / VXLAN / VRF / WireGuard / cloud VPN 向け IPsec の土台
- WAN 取得: DHCPv6-PD、DHCPv6 Information、DHCPv4 リース、PPPoE、DS-Lite
- LAN サービス: dnsmasq 統合による DHCPv4、DHCPv6、RA、DNS host record、
  ローカルドメイン、DDNS、DNSSEC フラグ、条件付き転送、DoH/DoT/DoQ と
  平文 UDP DNS へのローカル DNS 代理
- 委譲プレフィックスからの LAN IPv6 アドレス派生
- `WANEgressPolicy`、`HealthCheck`、`EventRule`、`DerivedEvent` による
  WAN 出口選択
- IPv4 既定経路、nftables による NAT44、conntrack 集計観測
- Unix ソケット HTTP+JSON の routerd / デーモン制御 API
- NixOS 生成設定。router02 では `routerd-dhcpv6-client@wan-pd` を
  `routerd-generated.nix` の宣言的ユニットとして運用済み

状態を持つファイアウォールは棚上げ中です。
現在の nftables 実装は NAT44 と、ラボで必要な狭い範囲が中心です。
汎用ファイアウォール規則言語ではありません。

## 命名

Phase 1.6 で RFC 表記に合わせた整理を行いました。
旧名の互換別名はありません。

使う名前:

- `DHCPv4Address`, `DHCPv4Lease`, `DHCPv4Server`, `DHCPv4Scope`,
  `DHCPv4Reservation`, `DHCPv4Relay`
- `DHCPv6Address`, `DHCPv6PrefixDelegation`, `DHCPv6Information`,
  `DHCPv6Server`, `DHCPv6Scope`
- `routerd-dhcpv4-client`, `routerd-dhcpv6-client`
- `/run/routerd/dhcpv4-client/...`, `/run/routerd/dhcpv6-client/...`
- `/var/lib/routerd/dhcpv4-client/...`, `/var/lib/routerd/dhcpv6-client/...`

Netplan の `dhcp4` / `dhcp6` や Debian/FreeBSD package 名など、外部仕様で
決まっている名前はそのまま扱います。

## ビルド

Go 1.24 以降を前提にします。

```sh
make test
make build
make check-schema
```

主な生成物:

- `bin/linux/routerd`
- `bin/linux/routerctl`
- `bin/linux/routerd-dhcpv4-client`
- `bin/linux/routerd-dhcpv6-client`
- `bin/linux/routerd-healthcheck`

よく使う確認:

```sh
go test ./...
routerd validate --config examples/router-lab.yaml
routerd plan --config examples/router-lab.yaml
routerd apply --config examples/router-lab.yaml --once --dry-run --status-file /tmp/routerd-status.json
routerctl status
```

## 配置

ソースインストールの既定:

- 設定: `/usr/local/etc/routerd/router.yaml`
- バイナリ: `/usr/local/sbin/routerd`, `/usr/local/sbin/routerctl`,
  `/usr/local/sbin/routerd-*`
- プラグイン: `/usr/local/libexec/routerd/plugins`
- Linux 実行時: `/run/routerd`
- Linux 状態: `/var/lib/routerd`
- FreeBSD 実行時と状態: `/var/run/routerd`, `/var/db/routerd`

デーモンの例:

```sh
/usr/local/sbin/routerd-dhcpv6-client \
  --resource wan-pd \
  --interface ens18 \
  --socket /run/routerd/dhcpv6-client/wan-pd.sock \
  --lease-file /var/lib/routerd/dhcpv6-client/wan-pd/lease.json \
  --event-file /var/lib/routerd/dhcpv6-client/wan-pd/events.jsonl
```

デーモン契約:

- `GET /v1/status`
- `GET /v1/healthz`
- `GET /v1/events?since=<cursor>&wait=<duration>`
- `POST /v1/commands/<command>`

## ラボの現在値

2026-05-03、Phase 1.7 後:

| ホスト | OS | デーモン | PD プレフィックス | 状態 |
|---|---|---|---|---|
| router01 | FreeBSD | `routerd-dhcpv6-client` | `2409:10:3d60:1250::/60` | Bound |
| router02 | NixOS | 宣言的 `routerd-dhcpv6-client@wan-pd` | `2409:10:3d60:1230::/60` | Bound |
| router03 | Ubuntu | `routerd-dhcpv6-client` | `2409:10:3d60:1240::/60` | Bound |
| router04 | FreeBSD | `routerd-dhcpv6-client` | `2409:10:3d60:1260::/60` | Bound |
| router05 | Ubuntu | `routerd-dhcpv6-client` + コントローラーチェーン | `2409:10:3d60:1220::/60` | Bound |

router05 では routerd が `ds-routerd-test@ens18` を実作成しました。
HGW 経由の条件付き DNS で `gw.transix.jp` を解決しました。
IPv4 既定経路と NAT44 を反映し、`curl -4` の外部疎通を確認済みです。

## 注意

主対象は Ubuntu Server です。
NixOS と FreeBSD は動作確認済みの範囲が広がっています。
ただし、すべての生成器が同等という意味ではありません。
健全なラボは pve05 から pve07 です。
pve01 から pve04 の vmbr0 VLAN 1901 経路は壊れた検証経路として扱います。
設計判断の根拠にしません。

詳細は `docs/design.md` が正です。
