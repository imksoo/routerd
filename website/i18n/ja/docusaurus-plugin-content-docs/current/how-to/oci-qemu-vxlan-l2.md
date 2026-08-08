---
title: PVEのLANをOCI上のQEMUゲストへVXLAN延伸する
---

# PVEのLANをOCI上のQEMUゲストへVXLAN延伸する

この構成は、PVE bridgeのEthernet broadcast domainを、OCI Compute上の
QEMUゲスト専用ネットワークまで延伸します。OCI VCNは外側のL3通信だけを
運び、延伸対象のbridgeには接続しません。

```text
PVE vmbr0 -- lan0 [routerd VM] wg0 -- VXLAN -- wg0 [routerd QEMU guest] lan0
                    |                                  |
                  br-l2                              br-l2
                                                       |
                                          OCI host br-legacy -- Windows taps

OCI VCN VNIC -- OCI hostのroute/NAT -- routerd guestのunderlay NIC
```

OCI側routerd applianceもQEMUゲストです。VM.Standard.E5.Flexでは
`/dev/kvm`を実測確認できるまでTCGゲストとして扱います。underlay/管理用NICと、
IPを設定しないLAN側NICの2本を割り当てます。Windows guestのtapとrouterd guestの
LAN側tapを、OCI host内部だけの`br-legacy`へ接続します。VCN VNICをこのbridgeへ
接続してはいけません。

routerdでは両端に同じVNIを持つ`VXLANTunnel`を作成し、LAN側NICとVXLAN deviceを
同じ`Bridge`へ参加させます。`VXLANTunnel`はpeerごとにall-zero MACのFDB entryを
作るため、broadcastとunknown unicastをunicast underlayへ複製します。
`VXLANSegment`の既定L2 filterは使用しないため、ARP、DHCPv4、IPv6 RS/RA/NS/NA、
DHCPv6が透過します。

本番`vmbr0`へ接続する前に、次の順序で確認します。

1. disposable Linux hostで
   `sudo tests/netns/vxlan-l2-control-plane-transparency.sh`を実行する。
2. PVEとOCIのisolated bridge間で同じprotocolとMTUをpacket captureする。
3. NW管理者から単一のguest IP/MACの予約を得る。
4. routerd applianceが`br-l2`でDHCP、DNS、RA serviceを起動していないことを確認する。
5. PVE firewall、MAC filter、物理switchがremote guest MACを許容し、MAC flap、
   duplicate IP、broadcast stormがないことを確認する。

完全透過は双方向です。guestがDHCP OfferやRAを送信することも技術的には可能なので、
Windows guestは信頼済みかつ初期状態ではofflineとし、必要なら別途directionalな
bridge firewallをpositive/negative testしてから有効化します。
