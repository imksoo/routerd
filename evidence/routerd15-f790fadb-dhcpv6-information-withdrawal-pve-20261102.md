# DHCPv6 情報 DNS の撤回・復旧実通信試験

- 実行リビジョン: `f790fadb519f05d87c7fa770e36b38445243987a`
- Linux 配布物 SHA-256: `a2106365562888241428f22f955a6b26e64793e9e24734a06bb2255eccdbbc5e`
- 対象: VM219 の routerd 1.5 候補、VM220 の独立 Kea DHCPv6 ピアと DNS 応答器、VM224 の独立クライアント
- 分離経路: VLAN 3131 が LAN、VLAN 3132 が WAN、VLAN 3133 が管理

## 観測

1. Kea が DNS サーバー `2001:db8:17::53` と検索ドメイン `info.test` を配布すると、候補は DHCPv6 の広告、要求、応答、プレフィックス委譲を完了しました。`DHCPv6Information/wan-info` は同じ DNS サーバーを Ready 状態で記録しました。
2. VM224 から候補の委譲 LAN アドレス `2001:db8:1500:d0::1` へ送った `router.info.test AAAA` 問い合わせは `NOERROR` となり、`2001:db8:17::53` を応答しました。
3. Kea の DHCPv6 情報から DNS 配布を取り除くと、`DHCPv6Information/wan-info` の DNS サーバーは空になりました。候補の DNS リゾルバー実行時設定は `noUsableSources: true` と空の上流へ更新されました。
4. 同じ VM224 問い合わせは `NXDOMAIN` となり、撤回済みの上流へは転送されませんでした。
5. Kea の DNS 配布を復元すると、候補の DNS リゾルバー設定は `udp://[2001:db8:17::53]:53` を再度持ち、同じ VM224 問い合わせは `NOERROR` と `2001:db8:17::53` へ復旧しました。

## 範囲

この証跡は Linux の DHCPv6 情報から DNS 上流へ反映する一機能の実通信試験です。IPv6、NAT、DNS、PPPoE、経路制御、冗長化、および全機能の置換可否を単独で示すものではありません。
