# Linux DHCPv6情報DNSの分離VLAN実通信試験

## 結果

現行Linuxパッケージで、独立Keaから取得したDHCPv6 DNSオプションを `DHCPv6Information` の状態へ反映し、候補DNSリゾルバーがそのアドレスへ転送した応答を独立LANクライアントで確認しました。

## 実施条件

- 実行リビジョン: `883dacbd3df3f0725e07f2985ccfcf559089b3f4`
- パッケージ SHA-256: `cd763b1a1356053dd6e7400358119a0edfbf23c674a16a4fd07ff580d7de83ad`
- VM219はVLAN 3131のLAN、VLAN 3132のWANで候補実行物を新規状態領域に起動しました。
- VM220はVLAN 3132でKea DHCPv6サーバーと `2001:db8:17::53` の独立DNSサーバーを起動しました。Keaには `2001:db8:1500::/60` の委任とDNSオプションを設定しました。
- VM224はVLAN 3131の独立クライアントです。

## 観測

- Keaは候補へ `2001:db8:1500::/60` を割り当てました。候補は `2001:db8:1500::1/64` をLANへ設定しました。
- 候補のDHCPv6子プロセスは `InfoReplyReceived` を記録し、DNSサーバー `2001:db8:17::53` と検索ドメイン `info.test` を観測しました。
- 候補が生成したDNSリゾルバー構成には、`DHCPv6Information/wan-info.dnsServers` に由来する `udp://[2001:db8:17::53]:53` とWAN送信インターフェースが入っていました。
- VM224から候補LANアドレス `2001:db8:1500::1` へ問い合わせた `router.info.test` のAAAA応答は `2001:db8:17::53` でした。

## 試験台の訂正と未確認範囲

KeaはAppArmor下でPIDファイル作成を拒否したため、既存の分離Kea試験と同様に `aa-exec -p unconfined` で起動しました。DNSサーバーのアドレスをループバックへ置くとVLAN上の近隣探索に応答できなかったため、WAN側VLANインターフェースへ移しました。どちらも独立上流試験台の設定であり、候補の機能結果には含めません。

DHCPv6情報オプションの更新・撤回・再取得、複数DNSサーバー、SNTP・AFTR、WAN経路とIPv6転送、通常の `systemd` サービス経路、FreeBSD、および製品全体の合格は対象外です。
