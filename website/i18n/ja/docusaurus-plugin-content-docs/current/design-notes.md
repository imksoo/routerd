# 設計メモ

この文書は、現在のコードと実機検証から残すべき設計判断をまとめます。
古い試行錯誤の詳細ではなく、今後の判断に使う要点だけを残します。

## 1. デーモン契約

状態を持つ処理は、専用デーモンに分けます。
各デーモンは次を持ちます。

- Unix ドメインソケットの HTTP+JSON API
- `/v1/status`
- `/v1/healthz`
- `/v1/events`
- `/v1/commands/reload`
- `/v1/commands/renew`
- `/v1/commands/stop`
- state または lease ファイル
- events.jsonl

この契約は `routerd-dhcpv6-client`、`routerd-dhcpv4-client`、`routerd-pppoe-client`、`routerd-healthcheck` で使います。

## 2. DHCPv6-PD

DHCPv6-PD は `routerd-dhcpv6-client` が担当します。
旧 OS クライアントへ設定を生成する経路は本線ではありません。

通常の NGN/HGW 環境では、取得、lease 保存、起動時復元、T1 Renew で十分です。
過去の壊れた経路を前提にした過激な再送は採用しません。

## 3. LAN への反映

DHCPv6-PD が Bound でない場合、LAN へ古い IPv6 を出しません。
これは RA、DHCPv6、AAAA、LAN アドレスすべてに関係します。

## 4. DS-Lite

AFTR option が DHCPv6 情報要求で返らない環境があります。
そのため、`DSLiteTunnel` は AFTR FQDN または AFTR IPv6 の静的指定を正規経路として扱います。

AFTR FQDN は公開 DNS で解決できない場合があります。
`DNSResolverUpstream.zones` による条件付き転送を使います。

## 5. イベント連携

routerd は bus を持ちます。
controller chain はイベントを受け、必要なリソースだけを調整します。

Phase 2-B で、次の Kind を追加しました。

- `WANEgressPolicy`
- `EventRule`
- `DerivedEvent`
- `HealthCheck`

EventRule はイベント列を入力にして別のイベント列を作ります。
DerivedEvent は状態から asserted / retracted の仮想イベントを作ります。

## 6. Tier S

WireGuard、IPsec、VRF、VXLAN は Tier S の土台です。
WireGuard と VXLAN over WireGuard は複数 OS をまたいで検証済みです。
IPsec は cloud VPN 接続向けの設定生成を先に進めます。

抽象的な `VPNTunnel` は作りません。
WireGuard、IPsec、将来の Tailscale、SoftEther は別 Kind として追加します。

## 7. まだ残す課題

- 状態を持つファイアウォールの本番実適用
- DoH 代理
- BGP/OSPF
- 高可用化
- OpenTelemetry collector を含む本番監視構成
- IX2215 置き換えの長時間検証
