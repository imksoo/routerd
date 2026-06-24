---
title: Azure と PVE の同一サブネット SAM スモークテスト
---

# Azure と PVE の同一サブネット SAM スモークテスト

![Azure プロバイダーセカンダリ IP 捕捉、オンプレミス proxy-ARP 捕捉、SAM /32 配送経路、転送の確認、routerctl doctor による検証の流れ](/img/diagrams/how-to-hybrid-azure-pve-same-subnet.png)

このガイドは、Azure の routerd ノードとオンプレミスの Proxmox VE routerd ノードで、Selective Address Mobility (SAM) により選択した `/32` アドレスを交換する、検証済みの運用形をまとめたものです。リソースの意味論は[選択的アドレス移動性のリファレンス](../reference/selective-address-mobility)を参照してください。

## Azure 側

- Azure NIC のセカンダリ IP は Azure 側に残します。このプロバイダー側のオブジェクトが、オンプレミスの `/32` 宛てのパケットを捕捉します。
- Ubuntu ゲスト OS には、捕捉した `/32` を持たせないでください。cloud-init や netplan がセカンダリ NIC の IP を自動付与することがあります。その設定は抑止するか削除します。routerd は no-local 捕捉ではリコンサイル時にそのアドレスをローカルインターフェースから外し、アドレスが存在しない状態を維持します。これは Claim が `configureOSAddress: false` の場合だけでなく、プロバイダー捕捉のセカンダリ IP を Azure に残したまま BGP delivery でリモートオーナーへ配送する場合も含みます。この BGP ケースでは、Azure NIC が ingress を所有し、Linux はローカル `/32` アドレスではなく proxy neighbor、forwarding sysctl/rule、インポートされた `/32` route で転送します。
- Azure NIC と Linux の両方で IP 転送を有効化します（`net.ipv4.ip_forward=1`）。

## オンプレミス PVE 側

- 同一サブネットのローカルホストが見える LAN やブリッジのインターフェースで、`proxy-arp` 捕捉を使います。
- Linux の転送を有効化します。SAM では routerd が通常の sysctl パスで `ip_forward` と `proxy_arp` を有効化します。
- 捕捉インターフェースと WireGuard トンネルの間で、捕捉した `/32` の転送をファイアウォールで許可します。SAM はファイアウォールルールや NAT ルールを追加しません。
- クラウドのゲストイメージでは、プロバイダーのファブリックがパケットを落としていると判断する前に、ホスト側ファイアウォールの既定値も確認してください。ルーターは WireGuard の UDP 待ち受けポートを受け付け、捕捉インターフェースと `wg-hybrid` の間の転送を許可する必要があります。`routerctl doctor hybrid` は、iptables の終端 drop/reject パターンと、SAM MSS clamp ルールの不足を警告します。

## トンネルとルーティング

- WireGuard はオンプレミスから Azure のパブリック IP へ接続する形にします。
- オンプレミス側のピアには `persistentKeepalive` を設定し、NAT やクラウドエッジの状態を維持します。
- 最初のスモークテストは UDR なしで実施します。後で UDR フォールバックを追加する場合は、Azure が捕捉した `/32` を配送元のルーターへ戻してしまう、同一サブネットのループに注意してください。
- SAM の配送は各 Claim をトンネルインターフェースへの `/32` 経路に落とし込みます。デフォルト経路は変更しません。

## 検証

実行:

```sh
routerctl doctor hybrid
```

`provider-secondary-ip` の no-local 捕捉では、捕捉した `/32` がローカルの `ip addr` に存在しないこと、配送経路がトンネルを向いていること、`ip_forward=1` であることを確認します。これには `configureOSAddress: false` の Claim と、BGP delivery でリモートオーナーへ配送する場合の両方が含まれます。`proxy-arp` では、`proxy_arp=1`、プロキシネイバーの存在、トンネルへの配送経路、`ip_forward=1` を確認します。

低 MTU のオーバーレイでは、`doctor hybrid` が SAM MSS clamp を報告し、`nft list table inet routerd_mss` に、選択した `/32` 経路の捕捉からトンネルへ、およびトンネルから捕捉への両方のルールが含まれていることを確認します。
