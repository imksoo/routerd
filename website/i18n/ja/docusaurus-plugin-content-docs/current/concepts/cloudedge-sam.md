---
title: CloudEdge SAM とは
---

# CloudEdge SAM とは

CloudEdge SAM（Selective Address Mobility / 選択的アドレス移動）は、**特定の
`/32` IPv4 アドレスだけを、オンプレミスと複数のパブリッククラウド（AWS / Azure
/ OCI）の間で「移動できる住所」として扱う** routerd の機能です。

一般的なルーターやクラウドのロードバランサーにはない概念なので、まず「何が新し
いのか」「何を解決するのか」から説明します。これを理解しないと、設定項目を見て
も何をしているのか分からないためです。

## 何が問題なのか

クラウドをまたいでサービスを冗長化したいとき、従来は次のどちらかを強いられま
した。

1. **L2 延伸（VXLAN/EVPN などで LAN を引き伸ばす）** — パブリッククラウドは
   オペレーターが制御できるブロードキャストドメインを公開しません。クラウドの
   ファブリックは独自のルーティングとアドレス所有モデルを持っており、Ethernet
   セグメントをそのまま延ばすことはできません。
2. **DNS フェイルオーバーやグローバルロードバランサー** — クライアントから見え
   る IP アドレスが切り替わります。TCP コネクションは切れ、DNS TTL のキャッシュ
   が残り、IP アドレスを直接握っているクライアントは追従できません。

CloudEdge SAM は **第3の道** を取ります。「LAN 全体」ではなく「動かしたい
`/32` アドレスだけ」を、routerd 同士のオーバーレイ越しに移動させます。クライア
ントから見える送信元・宛先 IP アドレスは保存され、アドレスを保持するノード（=
holder）が AWS から Azure へ移っても、**同じ IP アドレスがそのまま生き続けます**。

これは「クラウドをまたいで動く仮想 IP」と言い換えられます。VRRP の仮想 IP が
同一 L2 セグメント内でしか動けないのに対し、CloudEdge SAM の `/32` は **クラウ
ドの境界を越えて** 動きます。

## 全体像（メンタルモデル）

CloudEdge SAM を理解する鍵は、**2つの平面を分けて考える** ことです。

| 平面 | 担当 | 真実の源 |
| --- | --- | --- |
| **到達性（オーバーレイ）** | どのノードがそのアドレスを持っているか、パケットをどこへ運ぶか | **BGP の best-path**（[ADR 0012](../adr/0012-bgp-address-mobility.md)） |
| **クラウド受け口（ingress）** | クラウドのファブリックが外部からのパケットを正しい VM に入れるか | **プロバイダ secondary IP / ルートテーブル**（背景で同期） |

ポイントは、**到達性の真実は BGP RIB が握り、クラウド API 操作はそれを後追いで
合わせるだけ** という役割分担です。昔の routerd はリース台帳やエポックといった
独自の制御平面でこれをやっていましたが、今は素の BGP unicast `/32` に寄せていま
す（詳細は [ADR 0012](../adr/0012-bgp-address-mobility.md)）。

```
                オンプレ /24 の中の「動かしたい /32」だけを選ぶ
                              │
            ┌─────────────────┼─────────────────┐
            ▼                 ▼                 ▼
        ┌────────┐        ┌────────┐        ┌────────┐
        │ AWS    │        │ Azure  │        │ OCI    │   ← routerd ノード群
        │ routerd│◄──────►│ routerd│◄──────►│ routerd│   （WireGuard/IPIP オーバーレイ + BGP）
        └────────┘        └────────┘        └────────┘
            ▲ holder          standby           standby
            │
        この /32 の BGP best-path を出しているノードが「現在の持ち主（holder）」
```

## 登場する新しい概念

routerd 特有の用語が出てきます。最初に押さえるべきものだけ挙げます（網羅的な
内部実装は [CloudEdge SAM 内部実装](../reference/cloudedge-sam-internals.md) を参照）。

- **MobilityPool** — 「どの `/32` を、どのノード群で、どう動かすか」を宣言する
  唯一のオペレーター入力リソース。BGP peer リストのように、各ノードは他ノードの
  identity（nodeRef / site / role / placement）を知っていればよく、相手の NIC ID
  やサブネット ID を書く必要はありません。
- **capture（捕捉）** — クラウド VM の NIC に対象 `/32` を secondary IP として
  割り当て、その VM がそのアドレス宛てパケットを受け取れるようにする操作。これが
  「クラウド受け口」を作ります。
- **holder（ホルダー / 持ち主）** — いま実際にその `/32` を capture して BGP で
  best-path 広告しているノード。placement group ごとに1つ。
- **placement group と priority** — 「この `/32` 群は、この group の中で、
  priority の高いノードを active にする」というアクティブ/スタンバイの宣言。
  priority は **数字が小さいほど高優先**。
- **holder-beacon** — active な holder だけが自分の owner `/32` に付ける BGP
  community（`64512:121`）。他ノードは「この community が付いた best-path を出して
  いるノードだけが本当の holder」と判断します。スタンバイの弱い広告や起動直後の
  広告を holder と誤認しないための、**権威ある目印** です。

## 切り替えの振る舞い（ここが本機能の価値）

CloudEdge SAM が解こうとしている難問は「**切り替え操作は最小にしたいが、本当に
落ちたときは確実に引き継ぎたい**」という相反する要求です。routerd は priority の
関係で振る舞いを変えます。

- **同じ priority の2台（例 a=10, b=10）** → **no-preempt**。一度どちらかが
  holder になったら、もう一方が復帰しても奪い返しません。無意味な切り替えで
  データプレーンを揺らさないためです。
- **異なる priority（例 a=10 が高優先, b=20 が低優先）** → **自動復帰**。高優先
  ノードが落ちて低優先が引き継いだあと、高優先が戻ってきたら自動的に持ち主が
  高優先へ戻ります。ただし `/32` を1つずつ移し、**瞬断ゼロ**で引き継ぎます。
- **稼働系が落ちたら（priority に関係なく）** → **確実に failover**。holder の
  VM が停止/OS 障害で落ちたら、スタンバイが secondary IP を奪取（seize）して BGP
  広告を引き継ぎ、データプレーンを自動回復します。

この3つを両立させるために、routerd は次の機構を組み合わせています（詳細は
[内部実装](../reference/cloudedge-sam-internals.md)）。

1. **startup fence（起動フェンス）** — 復帰直後のノードは、観測が収束するまで
   active 主張を保留する。古い自己状態で持ち主を奪い返す事故を防ぐ。
2. **holder retention（ホルダー保持）** — 実際に capture を握っている間は active
   を維持する。決定的タイブレークや一時的な観測ゆらぎで持ち主を手放さない。
3. **holder-beacon** — 上記の「本当の持ち主」を BGP best-path 上で権威的に判定
   する。cold-start の相互譲り合いデッドロックも解消する。

## CloudEdge SAM ではないもの（誤解を避ける）

- **L2 延伸ではありません。** Ethernet ブロードキャストドメインをクラウドに伸ば
  すわけではなく、選んだ `/32` だけをオーバーレイで運びます。
- **NAT やロードバランサーではありません。** 送信元・宛先 IP は保存されます。
  ファイアウォールや NAT は routerd の別レイヤーで、mobility リソースの項目では
  ありません。
- **クラウドネイティブ ingress を魔法のように解決しません。** オーバーレイ経由の
  到達性は BGP 収束だけで回復しますが、VPC/VNet/VCN を通る外部 ingress は
  プロバイダの secondary IP / ルートテーブルが追いつくまで待つ必要があります。

## 次に読むもの

- [Selective Address Mobility（設定モデル）](../reference/selective-address-mobility.md)
  — `MobilityPool` の宣言方法、self/remote メンバー、capture ポリシー。
- [CloudEdge SAM 内部実装](../reference/cloudedge-sam-internals.md)
  — BGP community 体系、placement、no-preempt、holder-beacon、failover の詳細。
- [ADR 0012: BGP /32 Address Mobility](../adr/0012-bgp-address-mobility.md)
  — なぜ BGP を真実の源にしたかという設計判断。
- [CloudEdge mobility デモ](../how-to/cloudedge-mobility-demo.md)
  — オンプレ/AWS/Azure/OCI の4サイトを実際に動かすハンズオン。
