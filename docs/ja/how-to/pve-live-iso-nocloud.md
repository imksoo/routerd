---
title: Live ISO の PVE NoCloud bootstrap
---

# Live ISO の PVE NoCloud bootstrap

routerd の Live ISO は Ubuntu の `debootstrap` rootfs から作られており、完全な
`cloud-init` パッケージは入れていません。Proxmox VE のラボノード向けには、
routerd 起動前に必要な最小部分だけをサポートします。`cidata` / `CIDATA`
の config drive にある `user-data` から `hostname`、`routerd.config_url`、
`routerd.config_sha256` を読みます。

これにより、複数 VM が同じ ISO から起動しても、SSH や PVE 検証ログ上で別々の
ホストとして識別でき、完全な routerd config を HTTP や object storage から取得できます。

## user-data

トップレベルの `hostname` と、任意の routerd config pointer を持つ PVE snippet を作ります。

```yaml
#cloud-config
hostname: pve-rt07
routerd:
  config_url: http://10.0.0.10/routerd/pve-rt07/router.yaml
  config_sha256: 0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
ssh_authorized_keys:
  - ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA... admin@example
```

VM の cloud-init user-data として接続します。

```sh
qm set 169 --ide2 local:iso/routerd-live.iso,media=cdrom
qm set 169 --cicustom user=local:snippets/routerd-pve-rt07.yaml
qm set 169 --boot order=ide2
qm reboot 169
```

起動時、Live ISO の setup service は次の順序で処理します。

1. NoCloud user-data の `hostname` を反映する。
2. SSH host key を再生成し、VM ごとの host identity を分離する。
3. `ssh_authorized_keys` を `/root/.ssh/authorized_keys` に配置し、`ssh.service` を有効化する。
4. `ROUTERD_CONFIG` config disk を最優先で試す。
5. config disk がない場合、`routerd.config_url` を `curl` で取得する。
6. `routerd.config_sha256` がある場合は検証する。
7. 取得した `router.yaml` を配置するか、対応する config bundle を展開する。
8. 外部 config がない場合は最後に検証済みの cache、さらに組み込み sample config に fallback する。
9. bootstrap 用の systemd-networkd DHCP profile を削除し、`routerd.service` を起動する。

対応する bundle URL は `.tar.zst`、`.tzst`、`.tar.gz`、`.tgz`、`.tar` です。
bundle には archive root に `router.yaml` が必要です。任意の `secrets/` と
`metadata.json` は `/usr/local/etc/routerd/` 以下に配置されます。

fetch と checksum 検証が成功すると、配置済みの `router.yaml` は
`/var/lib/routerd/validated-config/router.yaml` に cache されます。次回 boot で
`routerd.config_url` を取得できない場合、Live ISO はこの検証済み cache を復元します。

ISO の既定 DHCP profile は、first boot が `routerd.config_url` に到達するためだけのものです。
config restore 後、setup service は routerd 起動前にこの profile を削除します。以後は
routerd の `DHCPv4Client`、`IPv4StaticAddress`、route resource がネットワークの管理元になります。

## 範囲

これは完全な cloud-init 実装ではありません。Live ISO が NoCloud から使うのは、
早期の hostname identity、root SSH authorized keys、routerd config bootstrap
だけです。user-data の network、user、package 設定や cloud-init module は実行しません。

より大きな bootstrap が必要な場合は、routerd の設定メディアを使うか、Ubuntu Server
をディスクへインストールして通常の cloud-init で管理してください。
