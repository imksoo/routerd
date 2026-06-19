---
title: Live ISO の PVE NoCloud hostname
---

# Live ISO の PVE NoCloud hostname

routerd の Live ISO は Ubuntu の `debootstrap` rootfs から作られており、完全な
`cloud-init` パッケージは入れていません。Proxmox VE のラボノード向けには、
routerd 起動前に必要な最小部分だけをサポートします。`cidata` / `CIDATA`
の config drive にある `user-data` から `hostname` を読み、`hostnamectl`
で反映します。

これにより、複数 VM が同じ ISO から起動しても、SSH や PVE 検証ログ上で別々の
ホストとして識別できます。

## user-data

トップレベルに `hostname` を持つ PVE snippet を作ります。

```yaml
#cloud-config
hostname: pve-rt07
```

VM の cloud-init user-data として接続します。

```sh
qm set 169 --ide2 local:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm reboot 169
```

起動時、Live ISO の setup service は block device を短時間待ち、NoCloud media
label の `CIDATA` と `cidata` を探します。`user-data` を読み、hostname を検証し、
`/etc/hostname` に書き込み、`hostnamectl set-hostname` を呼びます。

## 範囲

これは完全な cloud-init 実装ではありません。Live ISO が NoCloud から使うのは、
早期の hostname identity だけです。user-data の network、user、package、SSH key
設定や cloud-init module は実行しません。

より大きな bootstrap が必要な場合は、routerd の設定メディアを使うか、Ubuntu Server
をディスクへインストールして通常の cloud-init で管理してください。
