# 安裝與更新

下載Debian套件並驗證檢查碼。arm64請將 `linux-amd64` 改為 `linux-arm64`。相依的OS套件由apt安裝，並保留既有設定。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.deb
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.deb.sha256
sha256sum -c routerd-linux-amd64.deb.sha256
sudo apt install ./routerd-linux-amd64.deb
```

更新時以相同步驟安裝新的套件。替換檔案前，舊版套件會停止routerd及其輔助服務。新版套件在存在 `/usr/local/etc/routerd/router.yaml` 時啟動routerd，再由routerd啟動所需的輔助服務。需要固定版本時，請使用 `routerd_YYYYMMDD.HHMM_amd64.deb` 等帶版本號的檔案。移除時執行 `sudo apt remove routerd`。FreeBSD請參閱[FreeBSD入門](tutorials/freebsd-getting-started.md)。

[Releases](https://github.com/imksoo/routerd/releases/latest)

使用前修改介面名稱、位址與對端設定。先在本機驗證，再於目標路由器檢查計畫。--replace只用於完整設定。套用可能影響管理連線，請保留主控台。

```sh
routerd validate --config router.yaml
# On an existing router:
routerctl validate -f router.yaml --replace
routerctl plan -f router.yaml --replace
routerctl apply -f router.yaml --replace
```

既有路由器應先檢查差異，再用routerctl套用。新的Linux主機則放置確認後的設定並啟動routerd.service。

```sh
sudo install -d -m 0755 /usr/local/etc/routerd
sudo install -m 0600 router.yaml /usr/local/etc/routerd/router.yaml
sudo systemctl enable --now routerd.service
sudo routerctl get status
sudo routerctl get events --limit 20
```

Live ISO的工作根目錄與狀態DB是暫存空間。YAML與秘密資訊須放在已設定的持久媒體。不要更換執行中VM的ISO；先關機並確認停止，再更換媒體及開機。

[Live ISO](https://github.com/imksoo/routerd/blob/main/docs/how-to/pve-live-iso-nocloud.md) · [USB](operations/usb-persistence.md) · [DB](operations/state-database.md)
