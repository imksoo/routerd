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
2. `ROUTERD_CONFIG` config disk を最優先で試す。
3. config disk がない場合、`routerd.config_url` を `curl` で取得する。
4. `routerd.config_sha256` がある場合は検証する。
5. 取得した `router.yaml` を配置するか、対応する config bundle を展開する。
6. 外部 config がない場合は組み込み sample config に fallback する。

対応する bundle URL は `.tar.zst`、`.tzst`、`.tar.gz`、`.tgz`、`.tar` です。
bundle には archive root に `router.yaml` が必要です。任意の `secrets/` と
`metadata.json` は `/usr/local/etc/routerd/` 以下に配置されます。

## 範囲

これは完全な cloud-init 実装ではありません。Live ISO が NoCloud から使うのは、
早期の hostname identity と routerd config bootstrap だけです。user-data の
network、user、package、SSH key 設定や cloud-init module は実行しません。

より大きな bootstrap が必要な場合は、routerd の設定メディアを使うか、Ubuntu Server
をディスクへインストールして通常の cloud-init で管理してください。
