# インストールと更新

公式Linux amd64アーカイブを取得し、チェックサムを検証します。別のOS・アーキテクチャではReleasesから対応するものを選びます。インストーラーは既存の設定を維持します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

[Releases](https://github.com/imksoo/routerd/releases/latest)

例のインターフェース名・アドレス・対向設定を変更して使います。まずローカルで検証し、対象ルーターで計画を確認してください。--replaceは全体設定にだけ使います。反映時には管理接続も変わり得るため、コンソールを確保します。

```sh
routerd validate --config router.yaml
# On an existing router:
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
```

既存ルーターでは差分を確認してrouterctlで反映します。新規Linuxホストでは確認済みの設定を配置してrouterd.serviceを起動します。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo routerctl get status
sudo routerctl get events --limit 20
```

Live ISOの作業ルートと状態DBは一時領域です。YAMLと秘密情報は設定済みの永続媒体に保存します。稼働中VMのISOを差し替えず、シャットダウンと停止を確認してから媒体を交換し、起動してください。

[Live ISO](how-to/pve-live-iso-nocloud.md) · [USB](operations/usb-persistence.md) · [DB](operations/state-database.md)
