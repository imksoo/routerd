# インストールと更新

Debianパッケージを取得し、チェックサムを検証します。arm64では `linux-amd64` を `linux-arm64` に変えます。依存するOSパッケージはaptが導入し、既存の設定は維持します。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.deb.sha256
sha256sum -c routerd-linux-amd64.deb.sha256
sudo apt install ./routerd-linux-amd64.deb
```

更新も、新しいパッケージを同じ手順で入れます。ファイルを置き換える前に、旧版のパッケージがrouterdと補助サービスを止めます。新版のパッケージは `/usr/local/etc/routerd/router.yaml` があればrouterdを起動し、routerdが必要な補助サービスを起動します。版を固定する場合は、`routerd_YYYYMMDD.HHMM_amd64.deb` のような版番号付きのファイルを使います。削除は `sudo apt remove routerd` です。FreeBSDは[FreeBSD入門](tutorials/freebsd-getting-started.md)を参照してください。

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
