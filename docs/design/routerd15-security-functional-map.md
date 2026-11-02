# routerd 1.5 Linux セキュリティ機能対照

## 結論

routerd 1.5 は、旧設定形式を読み込まず、新しい設定で同じ防御目的を明示します。
この表は PVE VM219 に保全した `router.yaml.legacy-e3b49db` を機能の根拠として使います。
旧設定、旧状態データベース、移行処理は導入手順にも実装にも使いません。

各行は、目的、1.5 の構成要素、独立 VLAN における受入試験を対応付けます。
「未検証」は機能がない意味ではなく、その構成での実通信証跡がまだない状態です。

| 旧構成の防御目的 | 1.5 の新規構成 | Linux 実通信の状態 | 残る受入試験 |
| --- | --- | --- | --- |
| WAN、LAN、管理網の分離と既定拒否 | `FirewallZone` の `untrust`、`trust`、`mgmt` と `FirewallPolicy` | 3 VLAN の PVE 試験で、LAN→管理網の明示許可と WAN→LAN の既定拒否を実測済み | ルーター自身への拒否、UDP、CIDR 選択、管理 CIDR |
| LAN から管理網への必要な到達だけを許可 | `FirewallRule/allow-lan-to-management` | HTTP は到達し、規則撤回後は遮断、再投入後は復旧 | プロトコル・CIDR 限定、既存通信の扱い |
| 旧 LAN セグメントから通常 LAN への限定許可 | `FirewallRule/allow-legacy-172-to-lan` | 未検証 | 送信元・宛先 CIDR の一致時だけの到達と、非一致時の拒否 |
| WAN から LAN、ルーター自身への不要な到達を拒否 | `FirewallZone/wan` と既定 `FirewallPolicy` | 独立 WAN クライアントから LAN HTTP への TCP は遮断され、`wan_to_lan` の拒否カウンターを確認 | ルーター自身への拒否、UDP、設定撤回後の既存通信 |
| 公開 DNS/NTP 宛の LAN 通信をローカルサービスへ誘導 | `IPAddressSet/public-dns`、`IPAddressSet/public-ntp`、`LocalServiceRedirect/lan-local-services` | 未検証 | DNS と NTP の転送先、規則撤回、既存通信と新規通信 |
| 管理インターフェースを指定 CIDR に限定し、設定変更でのロックアウトを防止 | `ManagementAccess/mgmt-lockout-guard` と管理用 `FirewallZone` | 未検証 | 許可 CIDR からの管理到達、非許可 CIDR の拒否、設定撤回時の保護 |
| BGP の受入・広告範囲を限定 | `BGPRouter` の `importPolicy`、`exportPolicy`、`redistribute` | VLAN 3132 の BGP 経路導入・撤回・復旧は実測済み。接頭辞方針は未検証 | 許可接頭辞、拒否接頭辞、撤回、再広告とカーネル経路 |
| 私設網宛の不用意な外部転送を防止 | `IPv4Route/private-10-blackhole` と `EgressRoutePolicy` の除外 CIDR | 未検証 | ブラックホール経路、除外先、通常宛先の転送、撤回 |
| 防火壁の拒否と通信記録を監査可能にする | `FirewallEventLog/default`、`TrafficFlowLog/default`、`LogSink` | 未検証 | 許可・拒否の記録、対象識別、設定撤回後の停止 |

## 試験の共通条件

受入試験は PVE の未使用 VLAN と独立 VM を使います。候補ルーター、LAN クライアント、
WAN または管理網の端点を別 VM または別 VLAN に置きます。各試験で、正常通信、
規則または上流の撤回、期待する遮断、再投入後の復旧を順に観測します。

候補試験後は VM219、VM220、VM224 の実行物、設定、状態、起動状態を元へ戻します。
この復元は試験成功の代替ではなく、隔離試験の終了条件です。
