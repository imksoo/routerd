# 安裝與更新

下載官方Linux amd64封存檔並驗證檢查碼。其他OS或架構請在Releases選擇對應檔案。安裝程式保留既有設定。

```sh
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz
curl -LO https://github.com/imksoo/routerd/releases/latest/download/routerd-linux-amd64.tar.gz.sha256
sha256sum -c routerd-linux-amd64.tar.gz.sha256
tar -xzf routerd-linux-amd64.tar.gz
sudo ./install.sh
```

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
