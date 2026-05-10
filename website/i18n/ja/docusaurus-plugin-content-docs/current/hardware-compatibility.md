---
title: ハードウェア互換性
---

# ハードウェア互換性

routerd は、必要なカーネル機能とユーザーランド機能を持つ対応 OS 上で動きます。
実用上の論点は、ルーターとして十分なネットワークインターフェース、CPU、
メモリー、ストレージ耐久性があるかどうかです。

## 推奨クラス

| 種別 | 適性 | メモ |
| --- | --- | --- |
| Intel NUC | ラボルーター向き | 信頼性は高い傾向です。ただし Ethernet が 1 ポートのモデルが多いため、USB Ethernet や VLAN trunk は慎重に使います。 |
| Intel N100 mini PC | 家庭用ルーター向き | 消費電力に対する性能が高いです。Intel i226/i225 NIC と十分な冷却を持つモデルを選びます。 |
| Raspberry Pi 5 | edge や demo 向き | 高品質な電源と対応 USB/NVMe ストレージが重要です。スループットはアダプターに依存します。 |

## CPU とメモリー

家庭または小規模オフィスでは、次を目安にします。

- 基本的な経路制御、DHCP、DNS、NAT、Web Console なら 2 コアで足ります。
- DoH/DoT/DoQ、OpenTelemetry、ログ保存を使うなら 4 コアが扱いやすいです。
- 1 GiB RAM が実用下限です。
- ライブ ISO とログバッファーを使う場合は 2 GiB 以上を推奨します。

## ネットワークインターフェース

物理インターフェースは 2 つ以上を推奨します。

- WAN または untrust
- LAN または trust

3 つ目の管理インターフェースがあると、ファイアウォール変更の試験が安全になります。
SSH と Web Console を WAN/LAN policy から分離できます。

単一 NIC の VLAN ルーターも可能です。
ただし、初期設定時に管理経路を失うリスクが上がります。
反映前に必ず plan を確認してください。

## ストレージ

通常インストールでは SSD または NVMe を推奨します。
ディスクレス mini PC では、USB 永続化付きライブ ISO を使えます。

- 設定を USB デバイスへ保存します。
- ログは `/run/routerd/logs` の tmpfs に一時保存します。
- 1 日 1 回、圧縮ログと状態スナップショットを USB へ書き出せます。

これにより、低価格な flash media への書き込みを減らせます。

## NIC メモ

| NIC 種別 | 推奨 |
| --- | --- |
| Intel i210/i211 | 保守的で信頼性の高い選択です。 |
| Intel i225/i226 | 2.5GbE では良い選択です。firmware と OS driver を新しく保ちます。 |
| Realtek 2.5GbE | 動くことは多いですが、本番利用前に負荷試験をしてください。 |
| USB Ethernet | デモや NUC で便利です。本番ルーターでは無名アダプターを避けます。 |

## プラットフォームメモ

Ubuntu Server が primary target です。
NixOS と FreeBSD は platform 固有 renderer と service integration で対応します。
Linux 以外で特定機能に依存する場合は、[プラットフォーム](./platforms) を確認してください。

## 検証チェックリスト

1. 対象 OS またはライブ ISO を起動します。
2. すべての NIC 名が安定していることを確認します。
3. `routerd validate` と `routerd plan` を実行します。
4. 可能なら管理経路を分離してから反映します。
5. DHCP、DNS、NAT、firewall、route policy を確認します。
6. スループット試験を実行します。
7. CPU 温度と packet drop を確認します。
8. 再起動後、手動コマンドなしで収束することを確認します。
