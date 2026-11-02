# NAT44 動的 DNAT ピンホールの現行版 PVE 実行記録

対象リビジョンは \`b5ecacd50c8f017b1b30a0f3cd9e22eaa433ee52\` です。PVE の隔離 VM219 で、現行候補バイナリと実際の systemd サービスを起動しました。候補の \`routerd\` は SHA-256 \`6a5d1eb7c0149c97d30d6e8def8bb927551409461a5f00adcb0ca17ab887e500\` です。

独立した LAN クライアントと WAN 側ピアは、VM219 内の別ネットワーク名前空間で稼働させました。これは現行版の隔離 VM 実通信であり、VM220・VM224 と VLAN 3131・3132 を使用した三 VM 試験ではありません。

## 構成

- LAN クライアント: \`198.18.70.100/24\`。
- ルーター LAN/WAN: \`198.18.70.1/24\`、\`198.18.71.1/24\`。
- WAN 側ピア: \`198.18.71.10\`。
- NAT44: LAN 送信元の WAN 側マスカレード。
- 動的 DNAT: LAN クライアントが UDP/32100 で WAN 側ピアへ送信した場合だけ、ピアの UDP/32099 応答を元の LAN 側ポートへ 5 秒間転送。
- ファイアウォール: LAN→WAN の対象 UDP 通信を明示許可し、同じ通信範囲だけを \`FirewallFlowPinhole\` で逆方向へ許可。未要求の WAN→LAN 通信は既定拒否。

## 観測結果

1. \`routerd\` は NAT44 の動的 DNAT 対応表、LAN/WAN のゾーン、LAN→WAN の許可規則を nftables へ適用しました。
2. WAN 側からの未要求 UDP/32099 は、LAN クライアントへ届きませんでした。
3. LAN クライアントの UDP/32100 要求後、NAT44 の動的対応表には元の LAN アドレスとポートが記録されました。WAN 側の UDP/32099 応答は、動的 DNAT により LAN クライアントへ届きました。
4. 5 秒の期限満了後、同じ WAN 側応答は届きませんでした。
5. \`IPAddressSet/client\` を別アドレスへ変更すると、元クライアントの新規要求は許可されませんでした。
6. 空の 1.5 設定を \`routerctl apply --replace\` で適用すると、NAT44 表と動的対応表が撤回され、WAN 側からの応答は届きませんでした。

実行スクリプトは \`PASS: NAT44FlowDNATPinhole learns, DNATs, expires, source updates, and withdraws on the installed service\` を出力しました。

## 復元

VM219 は試験前スナップショット \`rd15-current-nat-pre\` へ戻しました。基準 ISO、基準バイナリのハッシュ、\`routerd\` と \`routerd-dnsmasq\` の稼働を確認した後、スナップショットと試験用 ISO を削除しました。

## 範囲外

- VM220・VM224 を使う VLAN 分離構成、静的ポート転送、NAT44 セッション同期、IPv6 NAT、物理回線は対象外です。
- この記録は NAT44 動的 DNAT ピンホールと対応するファイアウォール許可・撤回だけを示します。Linux 全機能または製品全体の合格を示すものではありません。
