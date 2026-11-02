# DNS 初回構成受け渡しの PVE 再試験

対象リビジョンは `50286a653f3324ae5b6248b4f037ee4bd2e2b384`、Linux 配布物 SHA-256 は
`a6ffb540724599cf6f701437f7f1509b37731a26de4d5b9c6e2188e56b3a8051` です。

PVE VM219 は新規状態領域から起動しました。VM220 は VLAN 3132 の独立 Kea、DNS、AFTR、HTTP端点、VM224 は VLAN 3131 の独立 IPv4 クライアントです。

手動で `routerd-dns-resolver` を再起動せず、VM219 に `aftr-local.sock` が生成され、`DNSResolver/aftr-local` は `Applied` になりました。候補ローカル DNS の `aftr.test AAAA` は `2001:db8:16::1` を返しました。

Kea が委任した `2001:db8:1500::/60` を候補は `Bound` とし、`2001:db8:1500::100` を外側送信元とする `rd15-dslite` を自動生成しました。VM224 の新規 HTTP は独立端点から `routerd15 DNS first-start recovery` を受信しました。

VM219、VM220、VM224 は試験前スナップショットへ戻し、ISO、CD-ROM 接続、一時資産、スナップショットを削除しました。これは DNS 初回構成受け渡しの範囲だけを示し、全 DNS または DS-Lite 機能の合格を意味しません。
