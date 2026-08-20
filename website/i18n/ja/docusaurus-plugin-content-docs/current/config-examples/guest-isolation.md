---
title: ゲスト / IoT の分離を設計する
sidebar_position: 60
---

# ゲスト / IoT の分離を設計する

![信頼済み LAN とゲスト VLAN を別の Layer 2 ネットワークにし、ルーターで通信を扱う構成](/img/diagrams/config-example-guest-isolation.png)

ゲスト端末や IoT 端末を「信頼済み端末と話せないネットワーク」に置くには、
ルーターの設定だけでは足りません。最初に、何を分ける必要があるかを整理します。

:::danger 同じ LAN に置いただけでは分離できない

同じ Ethernet スイッチ、同じ VLAN、同じ Wi-Fi SSID にいる端末は、
ルーターを通らずに直接フレームを送れることがあります。
Layer 2 は、スイッチや AP が端末どうしのフレームを直接運ぶ部分です。
routerd がルーターを通る IP 通信を制限しても、共有された Layer 2 の通信を
完全には止められません。MAC アドレスだけの区別は、MAC アドレスの偽装にも弱いです。

本当に分けるには、管理できるスイッチや Wi-Fi AP で **別の VLAN / SSID** を作り、
信頼済み用とゲスト用を別の Layer 2 ネットワークにします。分けられない場合は、
その端末を「隔離済み」と呼ばないでください。

:::

## 安全な構成の考え方

```text
信頼済み端末 ── VLAN 10 / trusted SSID ──┐
                                         ├── [ routerd VM ] ── WAN
ゲスト / IoT ── VLAN 20 / guest SSID ────┘
```

- スイッチと AP が VLAN / SSID の境界を守ります。
- ルーターは、別れたネットワークの間を通る IP 通信を扱います。
- 管理用ネットワークも、可能なら別の VLAN にします。
- VM で試すときも、trusted と guest を別の仮想スイッチにします。

このページにある `ClientPolicy` は、routerd に「この端末をゲストとして扱いたい」
という意図を書く例です。VLAN / SSID の代わりにはなりません。

## ClientPolicy の例

完全な YAML は `examples/guest-mode.yaml` にあります。次は、指定した MAC
アドレスをゲスト候補として扱う形です。

```yaml
- apiVersion: firewall.routerd.net/v1alpha1
  kind: ClientPolicy
  metadata:
    name: guest-devices
  spec:
    mode: include
    macs:
      - 18:ec:e7:33:12:6c
    isolation:
      lanInternet: allow
      lanLAN: deny
      lanMgmt: deny
      mDNSBroadcast: deny
```

- `mode: include` は、ここに列挙した端末だけを対象にします。
- `lanInternet: allow` は、インターネット側へ出したいという意図です。
- `lanLAN: deny` と `lanMgmt: deny` は、LAN と管理網に届かせたくないという意図です。
- `mDNSBroadcast: deny` は、mDNS のブロードキャストを出したくないという意図です。

:::caution ファイアウォール機能の現在地

`ClientPolicy` とファイアウォール用リソースは、現在も土台を整えている段階です。
この YAML が validate できても、公開ネットワークに対する完成した安全保証には
なりません。隔離した Ubuntu Server VM 以外で、唯一の防御として使わないでください。

:::

## daemon の前に確認する

例をコピーし、MAC アドレス、NIC 名、VLAN / 仮想スイッチの構成を自分のラボに
合わせてから確認します。まだサービスを起動していないので、`routerd` を使います。

```sh
cp examples/guest-mode.yaml ./guest-mode.yaml
LAB_DIR="$(mktemp -d)"
sudo routerd validate --config ./guest-mode.yaml
sudo routerd apply --config ./guest-mode.yaml --once --dry-run --skip-service-manager \
  --state-file "$LAB_DIR/state.db" \
  --ledger-file "$LAB_DIR/ledger.db" \
  --status-file "$LAB_DIR/status.json"
sudo sed -n "1,160p" "$LAB_DIR/status.json"
```

ここで確認するのは YAML と routerd が扱う IP 側の意図です。
VLAN の番号や SSID の分離が本当にスイッチ / AP に設定されているかは、
その機器の管理画面や設定でも別に確認します。

## サービス起動後に確認する

VM コンソールを開いたままサービスを起動した**後で**、`routerctl` を使います。

```sh
sudo install -m 0600 ./guest-mode.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo systemctl is-active routerd.service
sudo routerctl get status
sudo routerctl describe ClientPolicy/guest-devices
```

本当の分離を試すときは、ゲスト VLAN / SSID の端末から次を確認します。

- インターネットへの通信が必要なら、その通信だけができる。
- trusted VLAN / SSID の端末へ届かない。
- 管理用ネットワークとルーターの管理画面へ届かない。
- 同じ物理スイッチでも、VLAN をまたぐ直接通信ができない。

期待と違う結果なら、routerd の YAML だけでなく、スイッチのポート設定、AP の
SSID と VLAN の対応、クライアント間通信を許す設定を見直します。

## 次に読むもの

- [NAT44 とファイアウォールの準備](../tutorials/basic-firewall.md)
- [LAN 側サービス](../tutorials/lan-side-services.md)
- [対応プラットフォーム](../platforms.md)
